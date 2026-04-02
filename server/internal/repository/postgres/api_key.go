package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

// APIKeyRepo implements repository.APIKeyRepository using sqlc.
type APIKeyRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

// NewAPIKeyRepo creates a new APIKeyRepo.
func NewAPIKeyRepo(db DBTX) *APIKeyRepo {
	return &APIKeyRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new APIKeyRepo that operates within the given transaction.
func (r *APIKeyRepo) WithTx(tx pgx.Tx) repository.APIKeyRepository {
	return &APIKeyRepo{q: sqlcgen.New(tx), db: tx}
}

// Create generates a new API key, hashes it, and stores it. Returns the key
// with the raw value set — this is the only time the raw key is available.
func (r *APIKeyRepo) Create(ctx context.Context, params domain.CreateAPIKeyParams) (*domain.APIKey, string, error) {
	id := generateID("AK")

	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}

	prefix := "sk_"
	rawKey := prefix + hex.EncodeToString(keyBytes)
	keyHash := crypto.HashToken(rawKey)
	keyHint := rawKey[len(rawKey)-4:]

	createdBy := params.CreatedBy

	permissions := params.Permissions
	if permissions == nil {
		permissions = []string{}
	}

	var expiresAt *time.Time
	if params.ExpiresIn != "" {
		d, err := parseDuration(params.ExpiresIn)
		if err != nil {
			return nil, "", fmt.Errorf("parse expires_in: %w", err)
		}
		t := time.Now().Add(d)
		expiresAt = &t
	}

	row, err := r.q.CreateAPIKey(ctx, sqlcgen.CreateAPIKeyParams{
		ID:           id,
		Name:         params.Name,
		Description:  params.Description,
		KeyHash:      keyHash,
		KeyPrefix:    prefix,
		KeyHint:      keyHint,
		Scope:        string(params.Scope),
		Column8:      stringToText(params.WorkspaceID),
		Column9:      stringToText(params.AccountID),
		WorkspaceIds: params.WorkspaceIDs,
		CreatedBy:    createdBy,
		OnBehalfOf:   "",
		Type:         "persistent",
		Environment:  "live",
		Permissions:  permissions,
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		return nil, "", fmt.Errorf("insert api key: %w", err)
	}

	key := apiKeyToDomain(row)
	return key, rawKey, nil
}

// Get retrieves an API key by ID.
func (r *APIKeyRepo) Get(ctx context.Context, id string) (*domain.APIKey, error) {
	row, err := r.q.GetAPIKey(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return apiKeyToDomain(row), nil
}

// GetByHash retrieves an API key by its hash (for validation).
func (r *APIKeyRepo) GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	row, err := r.q.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return apiKeyToDomain(row), nil
}

// List retrieves API keys with optional filtering.
func (r *APIKeyRepo) List(ctx context.Context, params domain.ListAPIKeysParams) (*domain.CursorPage[domain.APIKey], error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	rows := make([]any, 0, limit+1)

	if params.IncludeRevoked {
		queryRows, err := r.q.ListAPIKeysIncludeRevoked(ctx, sqlcgen.ListAPIKeysIncludeRevokedParams{
			Column1: params.WorkspaceID,
			Column2: params.AccountID,
			Column3: string(params.Scope),
			ID:      params.Cursor,
			Limit:   int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list api keys: %w", err)
		}
		for _, row := range queryRows {
			rows = append(rows, row)
		}
	} else {
		queryRows, err := r.q.ListAPIKeys(ctx, sqlcgen.ListAPIKeysParams{
			Column1: params.WorkspaceID,
			Column2: params.AccountID,
			Column3: string(params.Scope),
			ID:      params.Cursor,
			Limit:   int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list api keys: %w", err)
		}
		for _, row := range queryRows {
			rows = append(rows, row)
		}
	}

	keys := make([]domain.APIKey, 0, len(rows))
	for _, row := range rows {
		k := apiKeyToDomain(row)
		// Never expose key_hash in list responses
		k.KeyHash = ""
		keys = append(keys, *k)
	}

	page := &domain.CursorPage[domain.APIKey]{}
	if len(keys) > limit {
		page.HasMore = true
		page.NextCursor = keys[limit].ID
		page.Items = keys[:limit]
	} else {
		page.Items = keys
	}
	if page.Items == nil {
		page.Items = []domain.APIKey{}
	}
	return page, nil
}

// Revoke marks an API key as revoked.
func (r *APIKeyRepo) Revoke(ctx context.Context, id string) error {
	return r.q.RevokeAPIKey(ctx, id)
}

// Update modifies name, description, and/or permissions of an API key.
func (r *APIKeyRepo) Update(ctx context.Context, id string, params domain.UpdateAPIKeyParams) (*domain.APIKey, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	if params.Name != nil {
		name = *params.Name
	}
	desc := existing.Description
	if params.Description != nil {
		desc = *params.Description
	}
	perms := existing.Permissions
	if params.Permissions != nil {
		perms = *params.Permissions
	}
	workspaceIDs := existing.WorkspaceIDs
	if params.WorkspaceIDs != nil {
		workspaceIDs = *params.WorkspaceIDs
	}

	row, err := r.q.UpdateAPIKey(ctx, sqlcgen.UpdateAPIKeyParams{
		ID:           id,
		Name:         name,
		Description:  desc,
		Permissions:  perms,
		WorkspaceIds: workspaceIDs,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update api key: %w", err)
	}

	return apiKeyToDomain(row), nil
}

// SetRotated marks an old key as rotated, pointing to the new key.
func (r *APIKeyRepo) SetRotated(ctx context.Context, oldKeyID, newKeyID string, gracePeriodEndsAt time.Time) error {
	return r.q.SetAPIKeyRotated(ctx, sqlcgen.SetAPIKeyRotatedParams{
		ID:                oldKeyID,
		RotatedToID:       newKeyID,
		GracePeriodEndsAt: &gracePeriodEndsAt,
	})
}

// UpdateUsage increments request_count and sets last_used_at.
func (r *APIKeyRepo) UpdateUsage(ctx context.Context, id string) error {
	return r.q.UpdateAPIKeyUsage(ctx, id)
}

// parseDuration parses a human-friendly duration string (supports "d" for days).
func parseDuration(s string) (time.Duration, error) {
	// Support "Nd" for N days
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}
