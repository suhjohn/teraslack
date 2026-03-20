package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/crypto"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type EventRepo struct {
	q         *sqlcgen.Queries
	pool      *pgxpool.Pool
	encryptor *crypto.Encryptor
}

func NewEventRepo(pool *pgxpool.Pool, encryptor *crypto.Encryptor) *EventRepo {
	return &EventRepo{q: sqlcgen.New(pool), pool: pool, encryptor: encryptor}
}

func (r *EventRepo) CreateEvent(ctx context.Context, event *domain.Event) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	return r.q.CreateEventRecord(ctx, sqlcgen.CreateEventRecordParams{
		ID:      event.ID,
		Type:    event.Type,
		TeamID:  event.TeamID,
		Payload: payload,
	})
}

func (r *EventRepo) CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	id := generateID("ES")

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	// Encrypt the webhook secret before storing in DB.
	encryptedSecret, encErr := r.encryptor.Encrypt(params.Secret)
	if encErr != nil {
		return nil, fmt.Errorf("encrypt secret: %w", encErr)
	}

	row, err := qtx.CreateEventSubscription(ctx, sqlcgen.CreateEventSubscriptionParams{
		ID:              id,
		TeamID:          params.TeamID,
		Url:             params.URL,
		EventTypes:      params.EventTypes,
		Secret:          params.Secret,
		EncryptedSecret: encryptedSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("insert subscription: %w", err)
	}

	sub := createEventSubRowToDomain(row)

	// Redact sensitive fields before writing to event log.
	redacted := sub.Redacted()
	eventData, _ := json.Marshal(redacted)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		EventType:     domain.EventSubscriptionCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return sub, nil
}

func (r *EventRepo) GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error) {
	row, err := r.q.GetEventSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	sub := getEventSubRowToDomain(row)
	// Decrypt the secret for runtime use.
	if sub.EncryptedSecret != "" {
		decrypted, decErr := r.encryptor.Decrypt(sub.EncryptedSecret)
		if decErr == nil {
			sub.Secret = decrypted
		}
	}
	return sub, nil
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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateEventSubscription(ctx, sqlcgen.UpdateEventSubscriptionParams{
		ID:         id,
		Url:        url,
		EventTypes: eventTypes,
		Enabled:    enabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update subscription: %w", err)
	}

	updatedSub := updateEventSubRowToDomain(row)

	// Redact sensitive fields before writing to event log.
	redacted := updatedSub.Redacted()
	eventData, _ := json.Marshal(redacted)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		EventType:     domain.EventSubscriptionUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return updatedSub, nil
}

func (r *EventRepo) DeleteSubscription(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	// Fetch full entity before deletion for snapshot
	subRow, subErr := qtx.GetEventSubscription(ctx, id)
	if subErr != nil {
		return fmt.Errorf("get subscription before delete: %w", subErr)
	}
	subSnapshot := getEventSubRowToDomain(subRow)

	if err := qtx.DeleteEventSubscription(ctx, id); err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}

	// Redact sensitive fields before writing to event log.
	redacted := subSnapshot.Redacted()
	eventData, _ := json.Marshal(redacted)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		EventType:     domain.EventSubscriptionDeleted,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *EventRepo) ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	rows, err := r.q.ListEventSubscriptions(ctx, params.TeamID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}

	subs := make([]domain.EventSubscription, 0, len(rows))
	for _, row := range rows {
		sub := listEventSubRowToDomain(row)
		// Decrypt secrets for runtime use.
		if sub.EncryptedSecret != "" {
			decrypted, decErr := r.encryptor.Decrypt(sub.EncryptedSecret)
			if decErr == nil {
				sub.Secret = decrypted
			}
		}
		subs = append(subs, *sub)
	}
	return subs, nil
}

func (r *EventRepo) ListSubscriptionsByTeamAndEvent(ctx context.Context, teamID, eventType string) ([]domain.EventSubscription, error) {
	rows, err := r.q.ListEventSubscriptionsByTeamAndEvent(ctx, sqlcgen.ListEventSubscriptionsByTeamAndEventParams{
		TeamID:  teamID,
		Column2: eventType,
	})
	if err != nil {
		return nil, fmt.Errorf("list subscriptions by event: %w", err)
	}

	subs := make([]domain.EventSubscription, 0, len(rows))
	for _, row := range rows {
		sub := listEventSubByTeamEventRowToDomain(row)
		// Decrypt secrets for webhook dispatch.
		if sub.EncryptedSecret != "" {
			decrypted, decErr := r.encryptor.Decrypt(sub.EncryptedSecret)
			if decErr == nil {
				sub.Secret = decrypted
			}
		}
		subs = append(subs, *sub)
	}
	return subs, nil
}
