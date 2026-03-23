package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type conversationPostingPolicyDocument struct {
	AllowedAccountTypes   []domain.AccountType   `json:"allowed_account_types,omitempty"`
	AllowedDelegatedRoles []domain.DelegatedRole `json:"allowed_delegated_roles,omitempty"`
	AllowedUserIDs        []string               `json:"allowed_user_ids,omitempty"`
	AllowedUsergroupIDs   []string               `json:"allowed_usergroup_ids,omitempty"`
}

type ConversationAccessRepo struct {
	db DBTX
}

func NewConversationAccessRepo(db DBTX) *ConversationAccessRepo {
	return &ConversationAccessRepo{db: db}
}

func (r *ConversationAccessRepo) WithTx(tx pgx.Tx) repository.ConversationAccessRepository {
	return &ConversationAccessRepo{db: tx}
}

func (r *ConversationAccessRepo) ListManagers(ctx context.Context, conversationID string) ([]domain.ConversationManagerAssignment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT conversation_id, user_id, assigned_by, created_at
		FROM conversation_manager_assignments
		WHERE conversation_id = $1
		ORDER BY user_id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list conversation managers: %w", err)
	}
	defer rows.Close()

	assignments := []domain.ConversationManagerAssignment{}
	for rows.Next() {
		var assignment domain.ConversationManagerAssignment
		if err := rows.Scan(&assignment.ConversationID, &assignment.UserID, &assignment.AssignedBy, &assignment.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation manager: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation managers: %w", err)
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

	if _, err := tx.Exec(ctx, `DELETE FROM conversation_manager_assignments WHERE conversation_id = $1`, conversationID); err != nil {
		return fmt.Errorf("delete existing conversation managers: %w", err)
	}
	for _, userID := range userIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_manager_assignments (conversation_id, user_id, assigned_by)
			VALUES ($1, $2, $3)
		`, conversationID, userID, assignedBy); err != nil {
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
	var exists bool
	if err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM conversation_manager_assignments
			WHERE conversation_id = $1 AND user_id = $2
		)
	`, conversationID, userID).Scan(&exists); err != nil {
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
	err := r.db.QueryRow(ctx, `
		SELECT conversation_id, policy_type, policy_json, updated_by, updated_at
		FROM conversation_posting_policies
		WHERE conversation_id = $1
	`, conversationID).Scan(&conversationID, &policyType, &policyJSON, &updatedBy, &updatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation posting policy: %w", err)
	}

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

	var updatedAt time.Time
	if err := r.db.QueryRow(ctx, `
		INSERT INTO conversation_posting_policies (conversation_id, policy_type, policy_json, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (conversation_id)
		DO UPDATE SET
			policy_type = EXCLUDED.policy_type,
			policy_json = EXCLUDED.policy_json,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
		RETURNING updated_at
	`, policy.ConversationID, string(policy.PolicyType), policyJSON, policy.UpdatedBy).Scan(&updatedAt); err != nil {
		return nil, fmt.Errorf("upsert conversation posting policy: %w", err)
	}

	policy.UpdatedAt = updatedAt
	return &policy, nil
}
