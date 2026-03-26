package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type conversationPostingPolicyDocument struct {
	AllowedAccountTypes   []domain.AccountType   `json:"allowed_account_types,omitempty"`
	AllowedDelegatedRoles []domain.DelegatedRole `json:"allowed_delegated_roles,omitempty"`
	AllowedUserIDs        []string               `json:"allowed_user_ids,omitempty"`
	AllowedUsergroupIDs   []string               `json:"allowed_usergroup_ids,omitempty"`
}

type ConversationAccessRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewConversationAccessRepo(db DBTX) *ConversationAccessRepo {
	return &ConversationAccessRepo{q: sqlcgen.New(db), db: db}
}

func (r *ConversationAccessRepo) WithTx(tx pgx.Tx) repository.ConversationAccessRepository {
	return &ConversationAccessRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *ConversationAccessRepo) ListManagers(ctx context.Context, conversationID string) ([]domain.ConversationManagerAssignment, error) {
	rows, err := r.q.ListConversationManagers(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list conversation managers: %w", err)
	}

	assignments := []domain.ConversationManagerAssignment{}
	for _, row := range rows {
		assignments = append(assignments, domain.ConversationManagerAssignment{
			ConversationID: row.ConversationID,
			UserID:         row.UserID,
			AssignedBy:     row.AssignedBy,
			CreatedAt:      tsToTime(row.CreatedAt),
		})
	}
	return assignments, nil
}

func (r *ConversationAccessRepo) ReplaceManagers(ctx context.Context, conversationID string, userIDs []string, assignedBy string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteConversationManagers(ctx, conversationID); err != nil {
		return fmt.Errorf("delete existing conversation managers: %w", err)
	}
	for _, userID := range userIDs {
		if err := qtx.InsertConversationManager(ctx, sqlcgen.InsertConversationManagerParams{
			ConversationID: conversationID,
			UserID:         userID,
			AssignedBy:     assignedBy,
		}); err != nil {
			return fmt.Errorf("insert conversation manager: %w", err)
		}
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit tx: %w", err)
		}
	}
	return nil
}

func (r *ConversationAccessRepo) IsManager(ctx context.Context, conversationID, userID string) (bool, error) {
	exists, err := r.q.IsConversationManager(ctx, sqlcgen.IsConversationManagerParams{
		ConversationID: conversationID,
		UserID:         userID,
	})
	if err != nil {
		return false, fmt.Errorf("check conversation manager: %w", err)
	}
	return exists, nil
}

func (r *ConversationAccessRepo) GetPostingPolicy(ctx context.Context, conversationID string) (*domain.ConversationPostingPolicy, error) {
	var (
		policyType string
		policyJSON []byte
		updatedBy  string
		updatedAt  time.Time
	)
	row, err := r.q.GetConversationPostingPolicy(ctx, conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation posting policy: %w", err)
	}
	conversationID = row.ConversationID
	policyType = row.PolicyType
	policyJSON = row.PolicyJson
	updatedBy = row.UpdatedBy
	updatedAt = tsToTime(row.UpdatedAt)

	var doc conversationPostingPolicyDocument
	if len(policyJSON) > 0 {
		if err := json.Unmarshal(policyJSON, &doc); err != nil {
			return nil, fmt.Errorf("decode conversation posting policy: %w", err)
		}
	}
	return &domain.ConversationPostingPolicy{
		ConversationID:        conversationID,
		PolicyType:            domain.ConversationPostingPolicyType(policyType),
		AllowedAccountTypes:   doc.AllowedAccountTypes,
		AllowedDelegatedRoles: doc.AllowedDelegatedRoles,
		AllowedUserIDs:        doc.AllowedUserIDs,
		AllowedUsergroupIDs:   doc.AllowedUsergroupIDs,
		UpdatedBy:             updatedBy,
		UpdatedAt:             updatedAt,
	}, nil
}

func (r *ConversationAccessRepo) UpsertPostingPolicy(ctx context.Context, policy domain.ConversationPostingPolicy) (*domain.ConversationPostingPolicy, error) {
	doc := conversationPostingPolicyDocument{
		AllowedAccountTypes:   policy.AllowedAccountTypes,
		AllowedDelegatedRoles: policy.AllowedDelegatedRoles,
		AllowedUserIDs:        policy.AllowedUserIDs,
		AllowedUsergroupIDs:   policy.AllowedUsergroupIDs,
	}
	policyJSON, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode conversation posting policy: %w", err)
	}

	updatedAt, err := r.q.UpsertConversationPostingPolicy(ctx, sqlcgen.UpsertConversationPostingPolicyParams{
		ConversationID: policy.ConversationID,
		PolicyType:     string(policy.PolicyType),
		PolicyJson:     policyJSON,
		UpdatedBy:      policy.UpdatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert conversation posting policy: %w", err)
	}

	policy.UpdatedAt = tsToTime(updatedAt)
	return &policy, nil
}
