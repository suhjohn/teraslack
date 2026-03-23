package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration for the application.
type Config struct {
	Port                    int
	DatabaseURL             string
	BaseURL                 string
	AuthStateSecret         string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string

	// S3 configuration
	S3Bucket    string
	S3Region    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3KeyPrefix string

	// Turbopuffer configuration
	TurbopufferAPIKey string
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
		dbURL = "postgres://teraslack:teraslack@localhost:5432/teraslack?sslmode=disable"
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", port)
	}

	return &Config{
		Port:                    port,
		DatabaseURL:             dbURL,
		BaseURL:                 baseURL,
		AuthStateSecret:         os.Getenv("AUTH_STATE_SECRET"),
		GitHubOAuthClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubOAuthClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		GoogleOAuthClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		S3Bucket:                os.Getenv("S3_BUCKET"),
		S3Region:                getEnv("S3_REGION", "us-east-1"),
		S3Endpoint:              os.Getenv("S3_ENDPOINT"),
		S3AccessKey:             os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:             os.Getenv("S3_SECRET_KEY"),
		S3KeyPrefix:             os.Getenv("S3_KEY_PREFIX"),
		TurbopufferAPIKey:       os.Getenv("TURBOPUFFER_API_KEY"),
		TurbopufferRegion:       getEnv("TURBOPUFFER_REGION", "aws-us-west-2"),
		EncryptionKey:           os.Getenv("ENCRYPTION_KEY"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
