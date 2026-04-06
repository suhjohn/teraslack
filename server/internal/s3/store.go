package s3store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/johnsuh/teraslack/server/internal/config"
)

var (
	ErrNotFound    = errors.New("object not found")
	ErrCASMismatch = errors.New("object compare-and-set failed")
)

type ReadResult struct {
	Body   []byte
	ETag   string
	Exists bool
}

type Store interface {
	Read(ctx context.Context, key string) (ReadResult, error)
	WriteCAS(ctx context.Context, key string, body []byte, expectedETag string) (string, error)
}

type ObjectStore struct {
	bucket string
	client *s3.Client
}

func New(ctx context.Context, cfg config.Config) (*ObjectStore, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required")
	}
	if cfg.S3Region == "" {
		return nil, fmt.Errorf("S3_REGION is required")
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.S3Region),
	}
	if cfg.S3AccessKey != "" || cfg.S3SecretKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if cfg.S3Endpoint != "" {
			options.BaseEndpoint = &cfg.S3Endpoint
			options.UsePathStyle = true
		}
	})

	return &ObjectStore{
		bucket: cfg.S3Bucket,
		client: client,
	}, nil
}

func (s *ObjectStore) Read(ctx context.Context, key string) (ReadResult, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		if isNotFound(err) {
			return ReadResult{}, ErrNotFound
		}
		return ReadResult{}, err
	}
	defer output.Body.Close()

	body, err := io.ReadAll(output.Body)
	if err != nil {
		return ReadResult{}, err
	}

	return ReadResult{
		Body:   body,
		ETag:   deref(output.ETag),
		Exists: true,
	}, nil
}

func (s *ObjectStore) WriteCAS(ctx context.Context, key string, body []byte, expectedETag string) (string, error) {
	input := &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        bytes.NewReader(body),
		ContentType: stringPtr("application/json"),
	}
	if expectedETag == "" {
		input.IfNoneMatch = stringPtr("*")
	} else {
		input.IfMatch = &expectedETag
	}

	output, err := s.client.PutObject(ctx, input)
	if err != nil {
		if isPreconditionFailed(err) {
			return "", ErrCASMismatch
		}
		return "", err
	}
	return deref(output.ETag), nil
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "NoSuchKey" || code == "NotFound"
	}
	return false
}

func isPreconditionFailed(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "PreconditionFailed"
	}
	return false
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtr(value string) *string {
	return &value
}
