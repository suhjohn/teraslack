package queue

import (
	"context"
	"errors"
	"time"
)

const (
	QueueIndex     = "index"
	QueueProjector = "projector"
	QueueWebhook   = "webhook"
)

var ErrNotConfigured = errors.New("queue transport is not configured")

type Producer struct {
	enqueue func(context.Context, EnqueueRequest) error
}

type Consumer struct {
	consumerID    string
	leaseDuration time.Duration
	claim         func(context.Context, ClaimRequest) (ClaimResponse, error)
	heartbeat     func(context.Context, HeartbeatRequest) error
	ack           func(context.Context, AckRequest) error
	retry         func(context.Context, RetryRequest) error
}

func (m *Manager) Producer() *Producer {
	return &Producer{
		enqueue: m.Enqueue,
	}
}

func (m *Manager) Consumer(consumerID string) *Consumer {
	return &Consumer{
		consumerID:    consumerID,
		leaseDuration: defaultLeaseDuration,
		claim:         m.Claim,
		heartbeat:     m.Heartbeat,
		ack:           m.Ack,
		retry:         m.Retry,
	}
}

func (p *Producer) Enqueue(ctx context.Context, items ...Item) error {
	if p == nil || p.enqueue == nil {
		return ErrNotConfigured
	}
	return p.enqueue(ctx, EnqueueRequest{Items: items})
}

func (c *Consumer) WithLeaseDuration(leaseDuration time.Duration) *Consumer {
	clone := *c
	if leaseDuration > 0 {
		clone.leaseDuration = leaseDuration
	}
	return &clone
}

func (c *Consumer) Claim(ctx context.Context, limit int) ([]ClaimedJob, error) {
	if c == nil || c.claim == nil {
		return nil, ErrNotConfigured
	}
	response, err := c.claim(ctx, ClaimRequest{
		ConsumerID:           c.consumerID,
		Limit:                limit,
		LeaseDurationSeconds: int(c.leaseDuration / time.Second),
	})
	if err != nil {
		return nil, err
	}
	return response.Jobs, nil
}

func (c *Consumer) Heartbeat(ctx context.Context, jobIDs ...string) error {
	if c == nil || c.heartbeat == nil {
		return ErrNotConfigured
	}
	return c.heartbeat(ctx, HeartbeatRequest{
		ConsumerID: c.consumerID,
		JobIDs:     jobIDs,
	})
}

func (c *Consumer) Ack(ctx context.Context, jobIDs ...string) error {
	if c == nil || c.ack == nil {
		return ErrNotConfigured
	}
	return c.ack(ctx, AckRequest{
		ConsumerID: c.consumerID,
		JobIDs:     jobIDs,
	})
}

func (c *Consumer) Retry(ctx context.Context, delay time.Duration, cause error, jobIDs ...string) error {
	if c == nil || c.retry == nil {
		return ErrNotConfigured
	}
	var message *string
	if cause != nil {
		errorText := cause.Error()
		message = &errorText
	}
	return c.retry(ctx, RetryRequest{
		ConsumerID:   c.consumerID,
		JobIDs:       jobIDs,
		DelaySeconds: int(delay / time.Second),
		Error:        message,
	})
}

func ConsumeOnce(ctx context.Context, consumer *Consumer, limit int, heartbeatInterval time.Duration, retryDelay time.Duration, handler func(context.Context, ClaimedJob) error) error {
	jobs, err := consumer.Claim(ctx, limit)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	for _, job := range jobs {
		heartbeatCancel := StartHeartbeat(ctx, consumer, job.ID, heartbeatInterval, nil)
		processErr := handler(ctx, job)
		heartbeatCancel()

		if processErr == nil {
			if err := consumer.Ack(ctx, job.ID); err != nil {
				return err
			}
			continue
		}

		if err := consumer.Retry(ctx, retryDelay, processErr, job.ID); err != nil {
			return err
		}
	}
	return nil
}
