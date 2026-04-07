package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	s3store "github.com/johnsuh/teraslack/server/internal/s3"
)

const (
	defaultGroupCommitLinger = 20 * time.Millisecond
	maxPendingOps            = 512
)

type opResult struct {
	value any
	err   error
}

type opApplyResult struct {
	value   any
	mutated bool
}

type queueOp struct {
	ctx     context.Context
	apply   func(*State, time.Time) (opApplyResult, error)
	replyCh chan opResult
}

type Manager struct {
	key   string
	store s3store.Store
	ops   chan queueOp
}

func NewManager(store s3store.Store, key string) *Manager {
	manager := &Manager{
		key:   key,
		store: store,
		ops:   make(chan queueOp, 1024),
	}
	go manager.run()
	return manager
}

func (m *Manager) Enqueue(ctx context.Context, request EnqueueRequest) error {
	if len(request.Items) == 0 {
		return nil
	}
	_, err := m.submit(ctx, func(state *State, now time.Time) (opApplyResult, error) {
		for _, item := range request.Items {
			availableAt := now
			if item.AvailableAt != nil {
				availableAt = item.AvailableAt.UTC()
			}
			payload := append(json.RawMessage(nil), item.Payload...)
			state.Jobs = append(state.Jobs, Job{
				ID:          uuid.NewString(),
				Kind:        item.Kind,
				Payload:     payload,
				EnqueuedAt:  now,
				AvailableAt: availableAt,
				Attempt:     0,
			})
		}
		return opApplyResult{mutated: true}, nil
	})
	return err
}

func (m *Manager) Claim(ctx context.Context, request ClaimRequest) (ClaimResponse, error) {
	value, err := m.submit(ctx, func(state *State, now time.Time) (opApplyResult, error) {
		if request.ConsumerID == "" {
			return opApplyResult{}, fmt.Errorf("consumer_id is required")
		}
		limit := request.Limit
		if limit <= 0 {
			limit = 1
		}
		if limit > maxClaimLimit {
			limit = maxClaimLimit
		}
		leaseDuration := time.Duration(request.LeaseDurationSeconds) * time.Second
		if leaseDuration <= 0 {
			leaseDuration = defaultLeaseDuration
		}

		claimed := make([]ClaimedJob, 0, limit)
		mutated := false
		for index := range state.Jobs {
			job := &state.Jobs[index]
			if !jobClaimable(job, now) {
				continue
			}
			job.Attempt++
			job.Claim = &Claim{
				ConsumerID:           request.ConsumerID,
				ClaimedAt:            now,
				LastHeartbeatAt:      now,
				LeaseDurationSeconds: int(leaseDuration / time.Second),
			}
			claimed = append(claimed, ClaimedJob{
				ID:          job.ID,
				Kind:        job.Kind,
				Payload:     append(json.RawMessage(nil), job.Payload...),
				EnqueuedAt:  job.EnqueuedAt,
				AvailableAt: job.AvailableAt,
				Attempt:     job.Attempt,
				ClaimedAt:   now,
			})
			mutated = true
			if len(claimed) >= limit {
				break
			}
		}

		return opApplyResult{
			value:   ClaimResponse{Jobs: claimed},
			mutated: mutated,
		}, nil
	})
	if err != nil {
		return ClaimResponse{}, err
	}
	response, ok := value.(ClaimResponse)
	if !ok {
		return ClaimResponse{}, fmt.Errorf("unexpected claim response type %T", value)
	}
	return response, nil
}

func (m *Manager) Heartbeat(ctx context.Context, request HeartbeatRequest) error {
	if len(request.JobIDs) == 0 {
		return nil
	}
	_, err := m.submit(ctx, func(state *State, now time.Time) (opApplyResult, error) {
		if request.ConsumerID == "" {
			return opApplyResult{}, fmt.Errorf("consumer_id is required")
		}
		mutated := false
		jobIDs := jobSet(request.JobIDs)
		for index := range state.Jobs {
			job := &state.Jobs[index]
			if !jobIDs[job.ID] || job.Claim == nil || job.Claim.ConsumerID != request.ConsumerID {
				continue
			}
			job.Claim.LastHeartbeatAt = now
			mutated = true
		}
		return opApplyResult{mutated: mutated}, nil
	})
	return err
}

func (m *Manager) Ack(ctx context.Context, request AckRequest) error {
	if len(request.JobIDs) == 0 {
		return nil
	}
	_, err := m.submit(ctx, func(state *State, now time.Time) (opApplyResult, error) {
		if request.ConsumerID == "" {
			return opApplyResult{}, fmt.Errorf("consumer_id is required")
		}
		jobIDs := jobSet(request.JobIDs)
		nextJobs := state.Jobs[:0]
		mutated := false
		for _, job := range state.Jobs {
			if jobIDs[job.ID] && job.Claim != nil && job.Claim.ConsumerID == request.ConsumerID {
				mutated = true
				continue
			}
			nextJobs = append(nextJobs, cloneJob(job))
		}
		state.Jobs = nextJobs
		return opApplyResult{mutated: mutated}, nil
	})
	return err
}

func (m *Manager) Retry(ctx context.Context, request RetryRequest) error {
	if len(request.JobIDs) == 0 {
		return nil
	}
	_, err := m.submit(ctx, func(state *State, now time.Time) (opApplyResult, error) {
		if request.ConsumerID == "" {
			return opApplyResult{}, fmt.Errorf("consumer_id is required")
		}
		delay := time.Duration(request.DelaySeconds) * time.Second
		if delay < 0 {
			delay = 0
		}
		jobIDs := jobSet(request.JobIDs)
		mutated := false
		for index := range state.Jobs {
			job := &state.Jobs[index]
			if !jobIDs[job.ID] || job.Claim == nil || job.Claim.ConsumerID != request.ConsumerID {
				continue
			}
			job.Claim = nil
			job.AvailableAt = now.Add(delay)
			if request.Error != nil {
				message := *request.Error
				job.LastError = &message
			}
			mutated = true
		}
		return opApplyResult{mutated: mutated}, nil
	})
	return err
}

func (m *Manager) submit(ctx context.Context, apply func(*State, time.Time) (opApplyResult, error)) (any, error) {
	replyCh := make(chan opResult, 1)
	operation := queueOp{
		ctx:     ctx,
		apply:   apply,
		replyCh: replyCh,
	}

	select {
	case m.ops <- operation:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case result := <-replyCh:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *Manager) run() {
	var (
		state  State
		etag   string
		loaded bool
	)

	for operation := range m.ops {
		pending := m.collectPending(operation)
		for {
			if !loaded {
				nextState, nextETag, err := m.load()
				if err != nil {
					m.replyWithError(pending, err)
					break
				}
				state = nextState
				etag = nextETag
				loaded = true
			}

			working := cloneState(state)
			now := time.Now().UTC()
			results := make([]opResult, len(pending))
			mutated := false

			for index, pendingOp := range pending {
				if pendingOp.ctx.Err() != nil {
					results[index] = opResult{err: pendingOp.ctx.Err()}
					continue
				}
				applyResult, err := pendingOp.apply(&working, now)
				if err != nil {
					results[index] = opResult{err: err}
					continue
				}
				results[index] = opResult{value: applyResult.value}
				if applyResult.mutated {
					mutated = true
				}
			}

			if !mutated {
				loaded = false
				m.reply(pending, results)
				break
			}

			nextETag, err := m.save(working, etag)
			if errors.Is(err, s3store.ErrCASMismatch) {
				loaded = false
				continue
			}
			if err != nil {
				m.replyWithError(pending, err)
				break
			}

			state = working
			etag = nextETag
			m.reply(pending, results)
			break
		}
	}
}

func (m *Manager) collectPending(first queueOp) []queueOp {
	pending := []queueOp{first}
	timer := time.NewTimer(defaultGroupCommitLinger)
	defer timer.Stop()

	for len(pending) < maxPendingOps {
		select {
		case next := <-m.ops:
			pending = append(pending, next)
		case <-timer.C:
			return pending
		}
	}
	return pending
}

func (m *Manager) load() (State, string, error) {
	result, err := m.store.Read(context.Background(), m.key)
	if err != nil {
		if errors.Is(err, s3store.ErrNotFound) {
			return State{Version: 1, Jobs: []Job{}}, "", nil
		}
		return State{}, "", err
	}

	if len(result.Body) == 0 {
		return State{Version: 1, Jobs: []Job{}}, result.ETag, nil
	}

	var state State
	if err := json.Unmarshal(result.Body, &state); err != nil {
		return State{}, "", fmt.Errorf("decode queue state %q: %w", m.key, err)
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Jobs == nil {
		state.Jobs = []Job{}
	}
	return state, result.ETag, nil
}

func (m *Manager) save(state State, etag string) (string, error) {
	body, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return m.store.WriteCAS(context.Background(), m.key, body, etag)
}

func (m *Manager) reply(pending []queueOp, results []opResult) {
	for index, pendingOp := range pending {
		select {
		case pendingOp.replyCh <- results[index]:
		default:
		}
	}
}

func (m *Manager) replyWithError(pending []queueOp, err error) {
	for _, pendingOp := range pending {
		select {
		case pendingOp.replyCh <- opResult{err: err}:
		default:
		}
	}
}

func jobClaimable(job *Job, now time.Time) bool {
	if now.Before(job.AvailableAt) {
		return false
	}
	if job.Claim == nil {
		return true
	}
	expiresAt := job.Claim.LastHeartbeatAt.Add(time.Duration(job.Claim.LeaseDurationSeconds) * time.Second)
	return !now.Before(expiresAt)
}

func cloneState(state State) State {
	clone := State{
		Version: state.Version,
		Jobs:    make([]Job, 0, len(state.Jobs)),
	}
	for _, job := range state.Jobs {
		clone.Jobs = append(clone.Jobs, cloneJob(job))
	}
	return clone
}

func cloneJob(job Job) Job {
	clone := job
	if job.Payload != nil {
		clone.Payload = append(json.RawMessage(nil), job.Payload...)
	}
	if job.LastError != nil {
		message := *job.LastError
		clone.LastError = &message
	}
	if job.Claim != nil {
		claim := *job.Claim
		clone.Claim = &claim
	}
	return clone
}

func jobSet(jobIDs []string) map[string]bool {
	result := make(map[string]bool, len(jobIDs))
	for _, jobID := range jobIDs {
		result[jobID] = true
	}
	return result
}
