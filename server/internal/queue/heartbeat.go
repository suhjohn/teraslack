package queue

import (
	"context"
	"time"
)

type Heartbeater interface {
	Heartbeat(ctx context.Context, jobIDs ...string) error
}

func StartHeartbeat(ctx context.Context, heartbeater Heartbeater, jobID string, interval time.Duration, onError func(error)) context.CancelFunc {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				err := heartbeater.Heartbeat(heartbeatCtx, jobID)
				if err != nil && onError != nil {
					onError(err)
				}
			}
		}
	}()
	return cancel
}
