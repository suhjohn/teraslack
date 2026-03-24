package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type EventRepo struct {
	q         *sqlcgen.Queries
	db        DBTX
	encryptor *crypto.Encryptor
}

func NewEventRepo(db DBTX, encryptor *crypto.Encryptor) *EventRepo {
	return &EventRepo{q: sqlcgen.New(db), db: db, encryptor: encryptor}
}

// WithTx returns a new EventRepo that operates within the given transaction.
func (r *EventRepo) WithTx(tx pgx.Tx) repository.EventRepository {
	return &EventRepo{q: sqlcgen.New(tx), db: tx, encryptor: r.encryptor}
}

func (r *EventRepo) CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	id := generateID("ES")

	// Encrypt the webhook secret before storing in DB.
	encryptedSecret, encErr := r.encryptor.Encrypt(params.Secret)
	if encErr != nil {
		return nil, fmt.Errorf("encrypt secret: %w", encErr)
	}

	row, err := r.q.CreateEventSubscription(ctx, sqlcgen.CreateEventSubscriptionParams{
		ID:              id,
		TeamID:          params.TeamID,
		Url:             params.URL,
		EventType:       params.Type,
		ResourceType:    params.ResourceType,
		ResourceID:      params.ResourceID,
		EncryptedSecret: encryptedSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("insert subscription: %w", err)
	}

	return createEventSubRowToDomain(row), nil
}

func (r *EventRepo) GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error) {
	row, err := r.q.GetEventSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return getEventSubRowToDomain(row), nil
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
	eventType := existing.Type
	if params.Type != nil {
		eventType = *params.Type
	}
	resourceType := existing.ResourceType
	if params.ResourceType != nil {
		resourceType = *params.ResourceType
	}
	resourceID := existing.ResourceID
	if params.ResourceID != nil {
		resourceID = *params.ResourceID
	}
	enabled := existing.Enabled
	if params.Enabled != nil {
		enabled = *params.Enabled
	}

	row, err := r.q.UpdateEventSubscription(ctx, sqlcgen.UpdateEventSubscriptionParams{
		ID:           id,
		Url:          url,
		EventType:    eventType,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Enabled:      enabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update subscription: %w", err)
	}

	return updateEventSubRowToDomain(row), nil
}

func (r *EventRepo) DeleteSubscription(ctx context.Context, id string) error {
	return r.q.DeleteEventSubscription(ctx, id)
}

func (r *EventRepo) ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	rows, err := r.q.ListEventSubscriptions(ctx, params.TeamID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}

	subs := make([]domain.EventSubscription, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, *listEventSubRowToDomain(row))
	}
	return subs, nil
}

func (r *EventRepo) ListSubscriptionsByEvent(ctx context.Context, teamID, eventType, resourceType, resourceID string) ([]domain.EventSubscription, error) {
	rows, err := r.q.ListEventSubscriptionsByTeamAndEvent(ctx, sqlcgen.ListEventSubscriptionsByTeamAndEventParams{
		TeamID:       teamID,
		EventType:    eventType,
		ResourceType: textValue(resourceType),
		ResourceID:   textValue(resourceID),
	})
	if err != nil {
		return nil, fmt.Errorf("list subscriptions by event: %w", err)
	}

	subs := make([]domain.EventSubscription, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, *listEventSubByTeamEventRowToDomain(row))
	}
	return subs, nil
}

func textValue(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}
