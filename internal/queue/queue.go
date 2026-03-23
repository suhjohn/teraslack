// Package queue implements a distributed job queue backed by a single JSON file
// on S3, following the Turbopuffer pattern of CAS-based coordination via ETags.
//
// Architecture:
//   - A single queue.json file on S3 holds all pending/claimed/completed jobs.
//   - Writers (producers) use group commit to batch pushes into a single CAS write.
//   - Workers (consumers) claim jobs via CAS, send heartbeats, and mark completion.
//   - CAS (compare-and-set) via S3 ETags ensures strong consistency without locks.
//   - Multiple workers can run safely — CAS prevents double-claims.
package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// ErrCASConflict is returned when a compare-and-set write fails because the
// object was modified since it was last read.
var ErrCASConflict = errors.New("queue: CAS conflict — object modified since last read")

// JobStatus represents the state of a job in the queue.
type JobStatus string

const (
	StatusPending   JobStatus = "pending"
	StatusClaimed   JobStatus = "claimed"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
)

// Job represents a single job in an S3-backed queue.
// Used for both index jobs (Turbopuffer) and webhook delivery jobs.
type Job struct {
	ID        string    `json:"id"`         // Unique job ID (e.g., "evt-123")
	EventID   int64     `json:"event_id"`   // Source service_event ID
	TeamID    string    `json:"team_id"`    // Team context
	EventType string    `json:"event_type"` // "user.created", "message.posted", etc.
	Status    JobStatus `json:"status"`     // Current job state

	// Index-specific fields
	ResourceType string          `json:"resource_type,omitempty"` // "user", "message", "conversation", "file"
	ResourceID   string          `json:"resource_id,omitempty"`   // Entity ID
	Content      string          `json:"content,omitempty"`       // Searchable text content for embedding
	Data         json.RawMessage `json:"data,omitempty"`          // Full entity snapshot for Turbopuffer metadata

	// Webhook-specific fields
	SubscriptionID string          `json:"subscription_id,omitempty"` // Target subscription
	URL            string          `json:"url,omitempty"`             // Webhook delivery URL
	Secret         string          `json:"secret,omitempty"`          // Encrypted HMAC signing secret
	Payload        json.RawMessage `json:"payload,omitempty"`         // Webhook request body (full service event envelope)

	// Retry tracking
	Attempts      int        `json:"attempts,omitempty"`        // Number of delivery attempts so far
	MaxAttempts   int        `json:"max_attempts,omitempty"`    // Max delivery attempts before permanent failure
	LastError     string     `json:"last_error,omitempty"`      // Last error message
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"` // Earliest time this job can be claimed (for backoff)

	// Worker coordination
	ClaimedBy   string     `json:"claimed_by,omitempty"`   // Worker ID that claimed this job
	Heartbeat   *time.Time `json:"heartbeat,omitempty"`    // Last heartbeat from claiming worker
	CompletedAt *time.Time `json:"completed_at,omitempty"` // When job was completed
	CreatedAt   time.Time  `json:"created_at"`             // When job was enqueued
}

// QueueState represents the full state of the queue file on S3.
type QueueState struct {
	// Cursor is the last service_event ID that was processed by the producer.
	// Used to resume tailing from the correct position after restart.
	Cursor int64 `json:"cursor"`
	Jobs   []Job `json:"jobs"`
}

// GCRetention is the default retention period for completed jobs before garbage collection.
const GCRetention = 7 * 24 * time.Hour

// S3Queue provides CAS-based read/write operations on a queue.json file in S3.
type S3Queue struct {
	client *s3.Client
	bucket string
	key    string
}

// S3Config holds configuration for the S3 queue.
type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	QueueKey  string // S3 object key, e.g. "queue/index-queue.json"
}

// NewS3Queue creates a new S3-backed queue client.
func NewS3Queue(ctx context.Context, cfg S3Config) (*S3Queue, error) {
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(cfg.Region))

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)
	key := cfg.QueueKey
	if key == "" {
		key = "queue/index-queue.json"
	}

	return &S3Queue{client: client, bucket: cfg.Bucket, key: key}, nil
}

// Snapshot holds the queue state and the ETag for CAS operations.
type Snapshot struct {
	State QueueState
	ETag  string // S3 ETag from the last read; empty if the object doesn't exist yet
}

// Read fetches the current queue state from S3.
// If the queue file doesn't exist, returns an empty state with an empty ETag.
func (q *S3Queue) Read(ctx context.Context) (*Snapshot, error) {
	out, err := q.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(q.bucket),
		Key:    aws.String(q.key),
	})
	if err != nil {
		// If the object doesn't exist, return empty state
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return &Snapshot{State: QueueState{Jobs: []Job{}}}, nil
		}
		return nil, fmt.Errorf("s3 get queue: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read queue body: %w", err)
	}

	var state QueueState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal queue: %w", err)
	}
	if state.Jobs == nil {
		state.Jobs = []Job{}
	}

	etag := ""
	if out.ETag != nil {
		etag = *out.ETag
	}

	return &Snapshot{State: state, ETag: etag}, nil
}

// Write performs a compare-and-set write of the queue state to S3.
// If the ETag doesn't match (object was modified since last read), returns ErrCASConflict.
func (q *S3Queue) Write(ctx context.Context, state QueueState, expectedETag string) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal queue: %w", err)
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(q.bucket),
		Key:         aws.String(q.key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	}

	// CAS: If we have an ETag from a previous read, use If-Match.
	// If the object is new (no ETag), use If-None-Match to prevent race on creation.
	if expectedETag != "" {
		input.IfMatch = aws.String(expectedETag)
	} else {
		input.IfNoneMatch = aws.String("*")
	}

	out, err := q.client.PutObject(ctx, input)
	if err != nil {
		// S3 returns 412 Precondition Failed when If-Match doesn't match.
		// Use smithy GenericAPIError to match the error code properly.
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "PreconditionFailed" {
			return "", ErrCASConflict
		}
		// Fallback: check HTTP status code directly
		var httpErr interface{ HTTPStatusCode() int }
		if errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == 412 {
			return "", ErrCASConflict
		}
		return "", fmt.Errorf("s3 put queue: %w", err)
	}

	newETag := ""
	if out.ETag != nil {
		newETag = *out.ETag
	}
	return newETag, nil
}
