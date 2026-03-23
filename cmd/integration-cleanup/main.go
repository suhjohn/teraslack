package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	tp "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"

	s3client "github.com/suhjohn/teraslack/internal/s3"
)

func main() {
	if err := run(); err != nil {
		log.Printf("integration cleanup failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var errs []error

	if err := cleanupS3(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := cleanupTurbopuffer(ctx); err != nil {
		errs = append(errs, err)
	}

	if len(errs) == 0 {
		return nil
	}

	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}
	return fmt.Errorf("%s", strings.Join(msgs, "; "))
}

func cleanupS3(ctx context.Context) error {
	bucket := strings.TrimSpace(os.Getenv("S3_BUCKET"))
	prefix := strings.Trim(strings.TrimSpace(os.Getenv("S3_KEY_PREFIX")), "/")
	if bucket == "" || prefix == "" {
		log.Printf("skipping s3 cleanup: bucket or prefix not set")
		return nil
	}

	client, err := s3client.NewClient(ctx, s3client.Config{
		Bucket:    bucket,
		Region:    envOr("S3_REGION", "us-east-1"),
		Endpoint:  strings.TrimSpace(os.Getenv("S3_ENDPOINT")),
		AccessKey: firstNonEmpty("S3_ACCESS_KEY", "AWS_ACCESS_KEY_ID"),
		SecretKey: firstNonEmpty("S3_SECRET_KEY", "AWS_SECRET_ACCESS_KEY"),
	})
	if err != nil {
		return fmt.Errorf("init s3 client: %w", err)
	}

	log.Printf("deleting s3 prefix %q from bucket %q", prefix+"/", bucket)
	if err := client.DeletePrefix(ctx, prefix+"/"); err != nil {
		return fmt.Errorf("delete s3 prefix %q: %w", prefix+"/", err)
	}
	return nil
}

func cleanupTurbopuffer(ctx context.Context) error {
	apiKey := strings.TrimSpace(os.Getenv("TURBOPUFFER_API_KEY"))
	prefix := strings.TrimSpace(os.Getenv("TURBOPUFFER_NS_PREFIX"))
	if apiKey == "" || prefix == "" {
		log.Printf("skipping turbopuffer cleanup: api key or namespace prefix not set")
		return nil
	}

	client := tp.NewClient(option.WithAPIKey(apiKey))
	region := envOr("TURBOPUFFER_REGION", "aws-us-west-2")
	client = tp.NewClient(
		option.WithAPIKey(apiKey),
		option.WithRegion(region),
	)
	pager := client.NamespacesAutoPaging(ctx, tp.NamespacesParams{
		Prefix: tp.String(prefix),
	})

	var deleted int
	for pager.Next() {
		ns := pager.Current()
		log.Printf("deleting turbopuffer namespace %q", ns.ID)
		namespace := client.Namespace(ns.ID)
		if _, err := namespace.DeleteAll(ctx, tp.NamespaceDeleteAllParams{}); err != nil {
			return fmt.Errorf("delete turbopuffer namespace %q: %w", ns.ID, err)
		}
		deleted++
	}
	if err := pager.Err(); err != nil {
		return fmt.Errorf("list turbopuffer namespaces with prefix %q: %w", prefix, err)
	}

	log.Printf("deleted %d turbopuffer namespaces with prefix %q in region %q", deleted, prefix, region)
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}
