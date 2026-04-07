package queue

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
)

const brokerRequestTimeout = 30 * time.Second

type BrokerClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewBrokerClient(baseURL string) *BrokerClient {
	return &BrokerClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: brokerRequestTimeout,
		},
	}
}

func (c *BrokerClient) Configured() bool {
	return c != nil && c.baseURL != ""
}

func (c *BrokerClient) Producer(queueName string) *Producer {
	return &Producer{
		enqueue: func(ctx context.Context, request EnqueueRequest) error {
			return c.post(ctx, queueName, "enqueue", request, nil)
		},
	}
}

func (c *BrokerClient) Consumer(queueName string, consumerID string) *Consumer {
	return &Consumer{
		consumerID:    consumerID,
		leaseDuration: defaultLeaseDuration,
		claim: func(ctx context.Context, request ClaimRequest) (ClaimResponse, error) {
			var response ClaimResponse
			err := c.post(ctx, queueName, "claim", request, &response)
			return response, err
		},
		heartbeat: func(ctx context.Context, request HeartbeatRequest) error {
			return c.post(ctx, queueName, "heartbeat", request, nil)
		},
		ack: func(ctx context.Context, request AckRequest) error {
			return c.post(ctx, queueName, "ack", request, nil)
		},
		retry: func(ctx context.Context, request RetryRequest) error {
			return c.post(ctx, queueName, "retry", request, nil)
		},
	}
}

func (c *BrokerClient) post(ctx context.Context, queueName string, action string, request any, response any) error {
	if !c.Configured() {
		return fmt.Errorf("queue broker is not configured")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/queues/%s/%s", c.baseURL, url.PathEscape(queueName), action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("queue broker %s %s failed: %s", queueName, action, message)
	}
	if response == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(response)
}
