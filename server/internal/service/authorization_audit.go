package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func recordAuthorizationAudit(ctx context.Context, repo repository.AuthorizationAuditRepository, tx pgx.Tx, workspaceID, action, resource, resourceID string, metadata any) error {
	if repo == nil || workspaceID == "" || action == "" || resource == "" || resourceID == "" {
		return nil
	}
	var metadataJSON json.RawMessage
	if metadata != nil {
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		metadataJSON = encoded
	}
	auditRepo := repo
	if tx != nil {
		auditRepo = repo.WithTx(tx)
	}
	actor := actorFromContext(ctx)
	metadataJSON, err := mergeActorMetadata(metadataJSON, actor)
	if err != nil {
		return err
	}
	_, err = auditRepo.Create(ctx, domain.CreateAuthorizationAuditLogParams{
		WorkspaceID:     workspaceID,
		ActorID:    actor.CompatibilityUserID(),
		APIKeyID:   ctxutil.GetAPIKeyID(ctx),
		OnBehalfOf: ctxutil.GetOnBehalfOf(ctx),
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Metadata:   metadataJSON,
	})
	return err
}
