package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	maxAttempts    = 3
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type queryEmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
	Model     string    `json:"model"`
}

type documentEmbeddingsResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Model      string      `json:"model"`
}

func New(baseURL string, apiKey string) *Client {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Client{
		baseURL: trimmedBaseURL,
		apiKey:  strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.baseURL != "" && c.apiKey != ""
}

func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return nil, fmt.Errorf("cannot embed an empty query")
	}
	var response queryEmbeddingResponse
	if err := c.post(ctx, "/embed/query", map[string]any{"text": normalized}, &response); err != nil {
		return nil, err
	}
	return response.Embedding, nil
}

func (c *Client) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	normalized := make([]string, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text != "" {
			normalized = append(normalized, text)
		}
	}
	if len(normalized) == 0 {
		return [][]float32{}, nil
	}
	var response documentEmbeddingsResponse
	if err := c.post(ctx, "/embed/documents", map[string]any{"texts": normalized}, &response); err != nil {
		return nil, err
	}
	if len(response.Embeddings) != len(normalized) {
		return nil, fmt.Errorf("embedding service returned %d embeddings for %d inputs", len(response.Embeddings), len(normalized))
	}
	return response.Embeddings, nil
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	if !c.Configured() {
		return fmt.Errorf("embedding service is not configured")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal embedding request: %w", err)
	}

	var lastErr error
	endpoint := c.baseURL + "/" + strings.TrimLeft(path, "/")
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err == nil {
			err = decodeResponse(resp, out)
		}
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt >= maxAttempts || !isRetriableError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt) * 250 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("embedding request failed")
	}
	return lastErr
}

func decodeResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return &requestError{
			statusCode: resp.StatusCode,
			message:    strings.TrimSpace(string(body)),
		}
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode embedding response: %w", err)
	}
	return nil
}

type requestError struct {
	statusCode int
	message    string
}

func (e *requestError) Error() string {
	return fmt.Sprintf("embedding request failed (%d): %s", e.statusCode, e.message)
}

func isRetriableError(err error) bool {
	var reqErr *requestError
	if errors.As(err, &reqErr) && reqErr.statusCode >= 500 {
		return true
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "terminated by signal") || strings.Contains(message, "internal error") || strings.Contains(message, "timeout") || strings.Contains(message, "timed out") || strings.Contains(message, "econnreset") || strings.Contains(message, "socket hang up") || strings.Contains(message, "fetch failed") {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
