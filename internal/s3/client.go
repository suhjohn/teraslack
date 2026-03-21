package s3

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client wraps the AWS S3 client for file operations.
type Client struct {
	client *s3.Client
	bucket string
}

// Config holds S3 configuration.
type Config struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
}

// NewClient creates a new S3 client.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithRegion(cfg.Region))

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
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
	return &Client{client: client, bucket: cfg.Bucket}, nil
}

// Upload uploads a file to S3 and returns the key.
func (c *Client) Upload(ctx context.Context, key string, body io.Reader, contentType string, size int64) error {
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("s3 upload: %w", err)
	}
	return nil
}

// Download returns a reader for the given S3 key.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, string, error) {
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("s3 download: %w", err)
	}
	contentType := ""
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	return out.Body, contentType, nil
}

// Delete removes a file from S3.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 delete: %w", err)
	}
	return nil
}

// GeneratePresignedURL generates a presigned URL for uploading.
func (c *Client) GeneratePresignedURL(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error) {
	presigner := s3.NewPresignClient(c.client)
	req, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("generate presigned url: %w", err)
	}
	return req.URL, nil
}

// GenerateDownloadURL generates a presigned URL for downloading.
func (c *Client) GenerateDownloadURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	presigner := s3.NewPresignClient(c.client)
	req, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("generate download url: %w", err)
	}
	return req.URL, nil
}

// Bucket returns the configured bucket name.
func (c *Client) Bucket() string {
	return c.bucket
}
