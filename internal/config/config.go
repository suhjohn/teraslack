package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration for the application.
type Config struct {
	Port        int
	DatabaseURL string
	BaseURL     string

	// Region is the unified region parameter. Individual service regions
	// (S3_REGION, TURBOPUFFER_REGION) override this if set.
	Region string

	// S3 configuration
	S3Bucket    string
	S3Region    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string

	// Queue configuration
	QueueS3Key string // S3 object key for the index queue file

	// Turbopuffer configuration
	TurbopufferRegion string

	// Encryption configuration
	// Hex-encoded 32-byte key for AES-256-GCM encryption of sensitive data.
	// Generate with: openssl rand -hex 32
	EncryptionKey string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	port := 8080
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT %q: %w", v, err)
		}
		port = p
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://slackbackend:slackbackend@localhost:5432/slackbackend?sslmode=disable"
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", port)
	}

	// Unified region: REGION env var, defaulting to us-east-1.
	// S3_REGION and TURBOPUFFER_REGION override it for their respective services.
	region := getEnv("REGION", "us-east-1")

	return &Config{
		Port:              port,
		DatabaseURL:       dbURL,
		BaseURL:           baseURL,
		Region:            region,
		S3Bucket:          os.Getenv("S3_BUCKET"),
		S3Region:          getEnv("S3_REGION", region),
		S3Endpoint:        os.Getenv("S3_ENDPOINT"),
		S3AccessKey:       os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:       os.Getenv("S3_SECRET_KEY"),
		QueueS3Key:        getEnv("QUEUE_S3_KEY", "queue/index-queue.json"),
		TurbopufferRegion: getEnv("TURBOPUFFER_REGION", region),
		EncryptionKey:     os.Getenv("ENCRYPTION_KEY"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
