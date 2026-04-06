package handler

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		entries: make(map[string][]time.Time),
	}
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now().UTC()
	cutoff := now.Add(-window)

	l.mu.Lock()
	defer l.mu.Unlock()

	values := l.entries[key]
	start := 0
	for start < len(values) && values[start].Before(cutoff) {
		start++
	}
	if start > 0 {
		values = append([]time.Time(nil), values[start:]...)
	}
	if len(values) >= limit {
		l.entries[key] = values
		return false
	}
	values = append(values, now)
	l.entries[key] = values
	return true
}
