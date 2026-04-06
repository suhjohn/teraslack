package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppRole                 string
	Port                    int
	DatabaseURL             string
	MigrationDatabaseURL    string
	BaseURL                 string
	FrontendURL             string
	CORSAllowedOrigins      []string
	AuthStateSecret         string
	EncryptionKey           string
	AWSKMSKeyID             string
	AWSKMSRegion            string
	AWSKMSEndpoint          string
	ResendAPIKey            string
	AuthEmailFrom           string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	S3Region                string
	S3Endpoint              string
	S3Bucket                string
	S3AccessKey             string
	S3SecretKey             string
	ProjectorQueueS3Key     string
	WebhookQueueS3Key       string
	WebhookProducerID       string
	ProjectorWorkerID       string
	WebhookWorkerID         string
}

func Load() (Config, error) {
	cfg := Config{
		AppRole:                 getenvDefault("APP_ROLE", "server"),
		Port:                    getenvIntDefault("PORT", 8080),
		DatabaseURL:             strings.TrimSpace(os.Getenv("DATABASE_URL")),
		MigrationDatabaseURL:    strings.TrimSpace(os.Getenv("MIGRATION_DATABASE_URL")),
		BaseURL:                 strings.TrimSpace(os.Getenv("BASE_URL")),
		FrontendURL:             strings.TrimSpace(os.Getenv("FRONTEND_URL")),
		CORSAllowedOrigins:      splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
		AuthStateSecret:         strings.TrimSpace(os.Getenv("AUTH_STATE_SECRET")),
		EncryptionKey:           strings.TrimSpace(os.Getenv("ENCRYPTION_KEY")),
		AWSKMSKeyID:             strings.TrimSpace(os.Getenv("AWS_KMS_KEY_ID")),
		AWSKMSRegion:            firstNonEmptyEnv("AWS_KMS_REGION", "AWS_REGION", "AWS_DEFAULT_REGION"),
		AWSKMSEndpoint:          strings.TrimSpace(os.Getenv("AWS_KMS_ENDPOINT")),
		ResendAPIKey:            strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		AuthEmailFrom:           strings.TrimSpace(os.Getenv("AUTH_EMAIL_FROM")),
		GitHubOAuthClientID:     strings.TrimSpace(os.Getenv("GITHUB_OAUTH_CLIENT_ID")),
		GitHubOAuthClientSecret: strings.TrimSpace(os.Getenv("GITHUB_OAUTH_CLIENT_SECRET")),
		GoogleOAuthClientID:     strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_ID")),
		GoogleOAuthClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")),
		S3Region:                strings.TrimSpace(os.Getenv("S3_REGION")),
		S3Endpoint:              strings.TrimSpace(os.Getenv("S3_ENDPOINT")),
		S3Bucket:                strings.TrimSpace(os.Getenv("S3_BUCKET")),
		S3AccessKey:             strings.TrimSpace(os.Getenv("S3_ACCESS_KEY")),
		S3SecretKey:             strings.TrimSpace(os.Getenv("S3_SECRET_KEY")),
		ProjectorQueueS3Key:     getenvDefault("PROJECTOR_QUEUE_S3_KEY", "queues/projector/queue.json"),
		WebhookQueueS3Key:       getenvDefault("WEBHOOK_QUEUE_S3_KEY", "queues/webhooks/queue.json"),
		WebhookProducerID:       getenvWorkerID("WEBHOOK_PRODUCER_ID", "webhook-producer"),
		ProjectorWorkerID:       getenvWorkerID("PROJECTOR_WORKER_ID", "external-event-projector"),
		WebhookWorkerID:         getenvWorkerID("WEBHOOK_WORKER_ID", "webhook-worker"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.MigrationDatabaseURL == "" {
		cfg.MigrationDatabaseURL = cfg.DatabaseURL
	}
	if cfg.FrontendURL != "" && len(cfg.CORSAllowedOrigins) == 0 {
		cfg.CORSAllowedOrigins = []string{cfg.FrontendURL}
	}
	return cfg, nil
}

func getenvDefault(key string, value string) string {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		return raw
	}
	return value
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func getenvIntDefault(key string, value int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return value
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return value
	}
	return parsed
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getenvWorkerID(envKey string, prefix string) string {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("WORKER_ID")); value != "" {
		return value
	}
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s@%s:%d", prefix, host, os.Getpid())
}
