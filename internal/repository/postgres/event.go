package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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
		EventTypes:      params.EventTypes,
		Secret:          "",
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

	row, err := r.q.UpdateEventSubscription(ctx, sqlcgen.UpdateEventSubscriptionParams{
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
