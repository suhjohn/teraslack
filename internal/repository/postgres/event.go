package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
)

// EventRepo implements repository.EventRepository using Postgres.
type EventRepo struct {
	pool *pgxpool.Pool
}

// NewEventRepo creates a new EventRepo.
func NewEventRepo(pool *pgxpool.Pool) *EventRepo {
	return &EventRepo{pool: pool}
}

func (r *EventRepo) CreateEvent(ctx context.Context, event *domain.Event) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO events (id, type, team_id, payload)
		VALUES ($1, $2, $3, $4)`,
		event.ID, event.Type, event.TeamID, event.Payload)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (r *EventRepo) CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	id := generateID("ES")

	var sub domain.EventSubscription
	err := r.pool.QueryRow(ctx, `
		INSERT INTO event_subscriptions (id, team_id, url, event_types, secret)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, team_id, url, event_types, secret, enabled, created_at, updated_at`,
		id, params.TeamID, params.URL, params.EventTypes, params.Secret,
	).Scan(
		&sub.ID, &sub.TeamID, &sub.URL, &sub.EventTypes, &sub.Secret,
		&sub.Enabled, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert subscription: %w", err)
	}
	return &sub, nil
}

func (r *EventRepo) GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error) {
	var sub domain.EventSubscription
	err := r.pool.QueryRow(ctx, `
		SELECT id, team_id, url, event_types, secret, enabled, created_at, updated_at
		FROM event_subscriptions WHERE id = $1`, id,
	).Scan(
		&sub.ID, &sub.TeamID, &sub.URL, &sub.EventTypes, &sub.Secret,
		&sub.Enabled, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return &sub, nil
}

func (r *EventRepo) UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error) {
	existing, err := r.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}

	url := existing.URL
	if params.URL != nil {
		url = *params.URL
	}
	eventTypes := existing.EventTypes
	if params.EventTypes != nil {
		eventTypes = params.EventTypes
	}
	enabled := existing.Enabled
	if params.Enabled != nil {
		enabled = *params.Enabled
	}

	var sub domain.EventSubscription
	err = r.pool.QueryRow(ctx, `
		UPDATE event_subscriptions SET url = $2, event_types = $3, enabled = $4
		WHERE id = $1
		RETURNING id, team_id, url, event_types, secret, enabled, created_at, updated_at`,
		id, url, eventTypes, enabled,
	).Scan(
		&sub.ID, &sub.TeamID, &sub.URL, &sub.EventTypes, &sub.Secret,
		&sub.Enabled, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update subscription: %w", err)
	}
	return &sub, nil
}

func (r *EventRepo) DeleteSubscription(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM event_subscriptions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EventRepo) ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, team_id, url, event_types, secret, enabled, created_at, updated_at
		FROM event_subscriptions WHERE team_id = $1 AND enabled = TRUE
		ORDER BY created_at ASC`, params.TeamID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []domain.EventSubscription
	for rows.Next() {
		var sub domain.EventSubscription
		if err := rows.Scan(
			&sub.ID, &sub.TeamID, &sub.URL, &sub.EventTypes, &sub.Secret,
			&sub.Enabled, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	if subs == nil {
		subs = []domain.EventSubscription{}
	}
	return subs, nil
}

func (r *EventRepo) ListSubscriptionsByTeamAndEvent(ctx context.Context, teamID, eventType string) ([]domain.EventSubscription, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, team_id, url, event_types, secret, enabled, created_at, updated_at
		FROM event_subscriptions
		WHERE team_id = $1 AND enabled = TRUE AND $2 = ANY(event_types)
		ORDER BY created_at ASC`, teamID, eventType)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions by event: %w", err)
	}
	defer rows.Close()

	var subs []domain.EventSubscription
	for rows.Next() {
		var sub domain.EventSubscription
		if err := rows.Scan(
			&sub.ID, &sub.TeamID, &sub.URL, &sub.EventTypes, &sub.Secret,
			&sub.Enabled, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	if subs == nil {
		subs = []domain.EventSubscription{}
	}
	return subs, nil
}
