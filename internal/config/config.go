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

	// S3 configuration
	S3Bucket    string
	S3Region    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string

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

	return &Config{
		Port:              port,
		DatabaseURL:       dbURL,
		BaseURL:           baseURL,
		S3Bucket:          os.Getenv("S3_BUCKET"),
		S3Region:          getEnv("S3_REGION", "us-east-1"),
		S3Endpoint:        os.Getenv("S3_ENDPOINT"),
		S3AccessKey:       os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:       os.Getenv("S3_SECRET_KEY"),
		TurbopufferRegion: getEnv("TURBOPUFFER_REGION", "us-east-1"),
		EncryptionKey:     os.Getenv("ENCRYPTION_KEY"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
