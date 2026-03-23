package teraslackmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type EventPage struct {
	Items      []domain.ExternalEvent `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
	HasMore    bool                   `json:"has_more"`
}

func NewClient(baseURL, apiKey string) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

func (c *Client) PostMessage(ctx context.Context, channelID, userID, text string, metadata map[string]any) (*domain.Message, error) {
	var resp domain.Message

	body := map[string]any{
		"channel_id": channelID,
		"user_id":    userID,
		"text":       text,
	}
	if metadata != nil {
		body["metadata"] = metadata
	}
	if err := c.doJSON(ctx, http.MethodPost, "/messages", nil, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListMessages(ctx context.Context, channelID string, limit int) ([]domain.Message, error) {
	query := url.Values{}
	query.Set("conversation_id", channelID)
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}

	var resp struct {
		Items []domain.Message `json:"items"`
	}

	if err := c.doJSON(ctx, http.MethodGet, "/messages", query, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *Client) ListEvents(ctx context.Context, after string, eventType, resourceType, resourceID string, limit int) ([]domain.ExternalEvent, error) {
	page, err := c.ListEventPage(ctx, after, eventType, resourceType, resourceID, limit)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (c *Client) ListEventPage(ctx context.Context, after string, eventType, resourceType, resourceID string, limit int) (*EventPage, error) {
	query := url.Values{}
	if after != "" {
		query.Set("after", after)
	}
	if eventType != "" {
		query.Set("type", eventType)
	}
	if resourceType != "" {
		query.Set("resource_type", resourceType)
	}
	if resourceID != "" {
		query.Set("resource_id", resourceID)
	}
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}

	var resp EventPage
	if err := c.doJSON(ctx, http.MethodGet, "/events", query, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Items == nil {
		resp.Items = []domain.ExternalEvent{}
	}
	return &resp, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return fmt.Errorf("unexpected status %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
