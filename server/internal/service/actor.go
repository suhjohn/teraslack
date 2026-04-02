package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
)

type canonicalActor struct {
	WorkspaceID   string
	AccountID     string
	MembershipID  string
	UserID        string
	PrincipalType domain.PrincipalType
	AccountType   domain.AccountType
	IsBot         bool
	APIKeyID      string
	OnBehalfOf    string
}

func actorFromContext(ctx context.Context) canonicalActor {
	return canonicalActor{
		WorkspaceID:   strings.TrimSpace(ctxutil.GetWorkspaceID(ctx)),
		AccountID:     strings.TrimSpace(ctxutil.GetAccountID(ctx)),
		MembershipID:  strings.TrimSpace(ctxutil.GetMembershipID(ctx)),
		UserID:        strings.TrimSpace(ctxutil.GetActingUserID(ctx)),
		PrincipalType: ctxutil.GetPrincipalType(ctx),
		AccountType:   ctxutil.GetAccountType(ctx),
		IsBot:         ctxutil.GetIsBot(ctx),
		APIKeyID:      strings.TrimSpace(ctxutil.GetAPIKeyID(ctx)),
		OnBehalfOf:    strings.TrimSpace(ctxutil.GetOnBehalfOf(ctx)),
	}
}

func (a canonicalActor) CompatibilityUserID() string {
	return a.UserID
}

func (a canonicalActor) IsAuthenticated() bool {
	return a.WorkspaceID != "" || a.AccountID != "" || a.MembershipID != "" || a.UserID != "" || a.APIKeyID != ""
}

func (a canonicalActor) metadataFields() map[string]any {
	fields := map[string]any{}
	if a.WorkspaceID != "" {
		fields["actor_workspace_id"] = a.WorkspaceID
	}
	if a.AccountID != "" {
		fields["actor_account_id"] = a.AccountID
	}
	if a.MembershipID != "" {
		fields["actor_membership_id"] = a.MembershipID
	}
	if a.UserID != "" {
		fields["actor_user_id"] = a.UserID
	}
	return fields
}

func (a canonicalActor) syntheticUser() *domain.User {
	if a.WorkspaceID == "" && a.UserID == "" && a.PrincipalType == "" && a.AccountType == "" && !a.IsBot {
		return nil
	}
	return &domain.User{
		ID:            a.UserID,
		WorkspaceID:   a.WorkspaceID,
		PrincipalType: a.PrincipalType,
		AccountType:   a.AccountType,
		IsBot:         a.IsBot,
	}
}

func requireCompatibilityActorID(ctx context.Context, requested, field string) (string, error) {
	if actorID := actorFromContext(ctx).CompatibilityUserID(); actorID != "" {
		return actorID, nil
	}
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested, nil
	}
	return "", fmt.Errorf("%s: %w", field, domain.ErrInvalidArgument)
}

func compatibilityActorID(ctx context.Context) string {
	return actorFromContext(ctx).CompatibilityUserID()
}

func CompatibilityActorID(ctx context.Context) string {
	return compatibilityActorID(ctx)
}

func mergeActorMetadata(existing json.RawMessage, actor canonicalActor) (json.RawMessage, error) {
	fields := actor.metadataFields()
	if len(fields) == 0 {
		if len(existing) == 0 {
			return nil, nil
		}
		return existing, nil
	}

	merged := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &merged); err != nil {
			return nil, err
		}
	}
	for key, value := range fields {
		merged[key] = value
	}
	return json.Marshal(merged)
}
