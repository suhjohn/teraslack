package teraslackmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

type userPage struct {
	Items      []domain.User `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
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

func (c *Client) AuthMe(ctx context.Context) (*domain.AuthContext, error) {
	var resp domain.AuthContext
	if err := c.doJSON(ctx, http.MethodGet, "/auth/me", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetUser(ctx context.Context, userID string) (*domain.User, error) {
	var resp domain.User
	if err := c.doJSON(ctx, http.MethodGet, "/users/"+url.PathEscape(strings.TrimSpace(userID)), nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListUsers(ctx context.Context, teamID, cursor string, limit int) (*userPage, error) {
	query := url.Values{}
	if teamID != "" {
		query.Set("team_id", teamID)
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}

	var resp userPage
	if err := c.doJSON(ctx, http.MethodGet, "/users", query, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Items == nil {
		resp.Items = []domain.User{}
	}
	return &resp, nil
}

func (c *Client) CreateUser(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	var resp domain.User
	if err := c.doJSON(ctx, http.MethodPost, "/users", nil, params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CreateAPIKey(ctx context.Context, params domain.CreateAPIKeyParams) (*domain.APIKey, string, error) {
	var resp struct {
		APIKey domain.APIKey `json:"api_key"`
		Secret string        `json:"secret"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api-keys", nil, params, &resp); err != nil {
		return nil, "", err
	}
	return &resp.APIKey, resp.Secret, nil
}

func (c *Client) CreateConversation(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	var resp domain.Conversation
	if err := c.doJSON(ctx, http.MethodPost, "/conversations", nil, params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) InviteUsers(ctx context.Context, channelID string, userIDs []string) (*domain.Conversation, error) {
	var resp domain.Conversation
	body := map[string]any{
		"user_ids": userIDs,
	}
	path := "/conversations/" + url.PathEscape(strings.TrimSpace(channelID)) + "/members"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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

func (c *Client) Request(ctx context.Context, method, path string, query map[string]any, body any) (any, error) {
	parsedQuery, cleanPath, err := normalizeRequestPath(path, query)
	if err != nil {
		return nil, err
	}
	var resp any
	if err := c.doJSON(ctx, method, cleanPath, parsedQuery, body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
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

	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func normalizeRequestPath(path string, query map[string]any) (url.Values, string, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return nil, "", fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse path: %w", err)
	}

	values := parsed.Query()
	for key, value := range query {
		appendQueryValues(values, key, value)
	}
	return values, parsed.Path, nil
}

func appendQueryValues(values url.Values, key string, value any) {
	key = strings.TrimSpace(key)
	if key == "" || value == nil {
		return
	}
	switch v := value.(type) {
	case string:
		values.Set(key, v)
	case bool:
		values.Set(key, strconv.FormatBool(v))
	case float64:
		values.Set(key, strconv.FormatFloat(v, 'f', -1, 64))
	case int:
		values.Set(key, strconv.Itoa(v))
	case int64:
		values.Set(key, strconv.FormatInt(v, 10))
	case json.Number:
		values.Set(key, v.String())
	case []any:
		for _, item := range v {
			appendQueryValues(values, key, item)
		}
	case []string:
		for _, item := range v {
			values.Add(key, item)
		}
	default:
		values.Set(key, fmt.Sprint(v))
	}
}
