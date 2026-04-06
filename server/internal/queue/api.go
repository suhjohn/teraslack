package queue

import (
	"context"
	"time"
)

type Producer struct {
	manager *Manager
}

type Consumer struct {
	manager       *Manager
	consumerID    string
	leaseDuration time.Duration
}

func (m *Manager) Producer() *Producer {
	return &Producer{manager: m}
}

func (m *Manager) Consumer(consumerID string) *Consumer {
	return &Consumer{
		manager:       m,
		consumerID:    consumerID,
		leaseDuration: defaultLeaseDuration,
	}
}

func (p *Producer) Enqueue(ctx context.Context, items ...Item) error {
	return p.manager.Enqueue(ctx, EnqueueRequest{Items: items})
}

func (c *Consumer) WithLeaseDuration(leaseDuration time.Duration) *Consumer {
	clone := *c
	if leaseDuration > 0 {
		clone.leaseDuration = leaseDuration
	}
	return &clone
}

func (c *Consumer) Claim(ctx context.Context, limit int) ([]ClaimedJob, error) {
	response, err := c.manager.Claim(ctx, ClaimRequest{
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
	return c.manager.Heartbeat(ctx, HeartbeatRequest{
		ConsumerID: c.consumerID,
		JobIDs:     jobIDs,
	})
}

func (c *Consumer) Ack(ctx context.Context, jobIDs ...string) error {
	return c.manager.Ack(ctx, AckRequest{
		ConsumerID: c.consumerID,
		JobIDs:     jobIDs,
	})
}

func (c *Consumer) Retry(ctx context.Context, delay time.Duration, cause error, jobIDs ...string) error {
	var message *string
	if cause != nil {
		errorText := cause.Error()
		message = &errorText
	}
	return c.manager.Retry(ctx, RetryRequest{
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
