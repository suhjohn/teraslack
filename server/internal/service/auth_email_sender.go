package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type ResendAuthEmailSenderConfig struct {
	APIKey  string
	From    string
	BaseURL string
}

type resendAuthEmailSender struct {
	httpClient *http.Client
	logger     *slog.Logger
	apiKey     string
	from       string
	baseURL    string
}

func NewResendAuthEmailSender(httpClient *http.Client, logger *slog.Logger, cfg ResendAuthEmailSenderConfig) AuthEmailSender {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.resend.com"
	}
	return &resendAuthEmailSender{
		httpClient: httpClient,
		logger:     logger,
		apiKey:     strings.TrimSpace(cfg.APIKey),
		from:       strings.TrimSpace(cfg.From),
		baseURL:    baseURL,
	}
}

func (s *resendAuthEmailSender) SendVerificationCode(ctx context.Context, email, code string, expiresAt time.Time) error {
	payload := map[string]any{
		"from":    s.from,
		"to":      []string{email},
		"subject": "Your Teraslack verification code",
		"text": fmt.Sprintf(
			"Your Teraslack verification code is %s. It expires at %s.",
			code,
			expiresAt.UTC().Format(time.RFC1123),
		),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal resend payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform resend request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(readSmallBody(resp.Body))
		if message == "" {
			message = resp.Status
		}
		if s.logger != nil {
			s.logger.Warn("resend email request failed", "status", resp.StatusCode, "body", message)
		}
		return fmt.Errorf("resend email request failed: %s", message)
	}

	return nil
}

func readSmallBody(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 4096))
	return string(data)
}
