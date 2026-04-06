package queue

import (
	"encoding/json"
	"time"
)

const (
	defaultLeaseDuration = 2 * time.Minute
	maxClaimLimit        = 100
)

type State struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

type Job struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	EnqueuedAt  time.Time       `json:"enqueued_at"`
	AvailableAt time.Time       `json:"available_at"`
	Attempt     int             `json:"attempt"`
	LastError   *string         `json:"last_error,omitempty"`
	Claim       *Claim          `json:"claim,omitempty"`
}

type Claim struct {
	ConsumerID           string    `json:"consumer_id"`
	ClaimedAt            time.Time `json:"claimed_at"`
	LastHeartbeatAt      time.Time `json:"last_heartbeat_at"`
	LeaseDurationSeconds int       `json:"lease_duration_seconds"`
}

type Item struct {
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	AvailableAt *time.Time      `json:"available_at,omitempty"`
}

type EnqueueItem = Item

type EnqueueRequest struct {
	Items []EnqueueItem `json:"items"`
}

type ClaimRequest struct {
	ConsumerID           string `json:"consumer_id"`
	Limit                int    `json:"limit"`
	LeaseDurationSeconds int    `json:"lease_duration_seconds,omitempty"`
}

type ClaimedJob struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	EnqueuedAt  time.Time       `json:"enqueued_at"`
	AvailableAt time.Time       `json:"available_at"`
	Attempt     int             `json:"attempt"`
	ClaimedAt   time.Time       `json:"claimed_at"`
}

type ClaimResponse struct {
	Jobs []ClaimedJob `json:"jobs"`
}

type HeartbeatRequest struct {
	ConsumerID string   `json:"consumer_id"`
	JobIDs     []string `json:"job_ids"`
}

type AckRequest struct {
	ConsumerID string   `json:"consumer_id"`
	JobIDs     []string `json:"job_ids"`
}

type RetryRequest struct {
	ConsumerID   string   `json:"consumer_id"`
	JobIDs       []string `json:"job_ids"`
	DelaySeconds int      `json:"delay_seconds,omitempty"`
	Error        *string  `json:"error,omitempty"`
}

type EmptyResponse struct{}
