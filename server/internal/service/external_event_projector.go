package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

const externalEventProjectorName = "external_events"

type ExternalEventProjector struct {
	db           repository.TxBeginner
	internal     repository.InternalEventStoreRepository
	external     repository.ExternalEventRepository
	checkpoints  repository.ProjectorCheckpointRepository
	logger       *slog.Logger
	pollInterval time.Duration
	ownedShards  []int
}

func NewExternalEventProjector(
	db repository.TxBeginner,
	internal repository.InternalEventStoreRepository,
	external repository.ExternalEventRepository,
	checkpoints repository.ProjectorCheckpointRepository,
	logger *slog.Logger,
) *ExternalEventProjector {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &ExternalEventProjector{
		db:           db,
		internal:     internal,
		external:     external,
		checkpoints:  checkpoints,
		logger:       logger,
		pollInterval: 200 * time.Millisecond,
		ownedShards:  allInternalEventShards(),
	}
}

func (p *ExternalEventProjector) SetOwnedShards(shards []int) {
	if len(shards) == 0 {
		p.ownedShards = allInternalEventShards()
		return
	}
	p.ownedShards = append([]int(nil), shards...)
}

func (p *ExternalEventProjector) Start(ctx context.Context) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		if err := p.ProcessPending(ctx); err != nil {
			if ctx.Err() == nil {
				p.logger.Error("external event projector failed and stopped", "error", err)
			}
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *ExternalEventProjector) ProcessPending(ctx context.Context) error {
	for _, shardID := range p.ownedShards {
		if err := p.processPendingShard(ctx, shardID); err != nil {
			return err
		}
	}
	return nil
}

func (p *ExternalEventProjector) processPendingShard(ctx context.Context, shardID int) error {
	checkpointName := externalEventProjectorCheckpointName(shardID)
	lastID, err := p.checkpoints.Get(ctx, checkpointName)
	if err != nil {
		return err
	}
	for {
		events, err := p.internal.GetAllSinceByShard(ctx, shardID, lastID, 100)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}

		tx, err := p.db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin projector tx: %w", err)
		}
		defer tx.Rollback(ctx)

		externalRepo := p.external.WithTx(tx)
		checkpoints := p.checkpoints.WithTx(tx)
		batchLastID := lastID

		for _, internalEvent := range events {
			projected, projectErr := projectExternalEvents(internalEvent)
			if projectErr != nil {
				if err := externalRepo.RecordProjectionFailure(ctx, internalEvent.ID, projectErr.Error()); err != nil {
					return err
				}
				if err := checkpoints.Set(ctx, checkpointName, batchLastID); err != nil {
					return err
				}
				if err := tx.Commit(ctx); err != nil {
					return fmt.Errorf("commit projector failure state: %w", err)
				}
				return fmt.Errorf("project internal event %d (%s) on shard %d: %w", internalEvent.ID, internalEvent.EventType, shardID, projectErr)
			}

			for _, externalEvent := range projected {
				if _, err := externalRepo.Insert(ctx, externalEvent); err != nil {
					if recErr := externalRepo.RecordProjectionFailure(ctx, internalEvent.ID, err.Error()); recErr != nil {
						return recErr
					}
					if err := checkpoints.Set(ctx, checkpointName, batchLastID); err != nil {
						return err
					}
					if err := tx.Commit(ctx); err != nil {
						return fmt.Errorf("commit projector failure state: %w", err)
					}
					return fmt.Errorf("persist external event for internal event %d on shard %d: %w", internalEvent.ID, shardID, err)
				}
			}

			batchLastID = internalEvent.ID
		}

		if err := checkpoints.Set(ctx, checkpointName, batchLastID); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit projector tx: %w", err)
		}
		lastID = batchLastID
	}
}

func (p *ExternalEventProjector) Rebuild(ctx context.Context) error {
	if err := p.external.Rebuild(ctx, nil); err != nil {
		return err
	}
	for _, shardID := range p.ownedShards {
		if err := p.checkpoints.Set(ctx, externalEventProjectorCheckpointName(shardID), 0); err != nil {
			return err
		}
	}
	return p.ProcessPending(ctx)
}

func (p *ExternalEventProjector) RebuildFeeds(ctx context.Context) error {
	return p.external.RebuildFeeds(ctx)
}

func externalEventProjectorCheckpointName(shardID int) string {
	return fmt.Sprintf("%s:shard:%d", externalEventProjectorName, shardID)
}

func allInternalEventShards() []int {
	shards := make([]int, domain.InternalEventShardCount)
	for i := range shards {
		shards[i] = i
	}
	return shards
}

func projectExternalEvents(internalEvent domain.InternalEvent) ([]domain.ExternalEvent, error) {
	switch internalEvent.EventType {
	case domain.EventWorkspaceCreated:
		return singleExternalEvent(internalEvent, domain.EventTypeWorkspaceCreated, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, safeWorkspacePayload)
	case domain.EventWorkspaceUpdated:
		return singleExternalEvent(internalEvent, domain.EventTypeWorkspaceUpdated, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, safeWorkspacePayload)
	case domain.EventUserCreated:
		return singleExternalEvent(internalEvent, domain.EventTypeUserCreated, domain.ResourceTypeUser, internalEvent.AggregateID, safeUserPayload)
	case domain.EventUserUpdated:
		return projectUserUpdated(internalEvent)
	case domain.EventUserDeleted:
		return singleExternalEvent(internalEvent, domain.EventTypeUserDeleted, domain.ResourceTypeUser, internalEvent.AggregateID, userDeletePayload)
	case domain.EventUserRolesUpdated:
		return singleExternalEvent(internalEvent, domain.EventTypeUserRolesUpdated, domain.ResourceTypeUser, internalEvent.AggregateID, safeUserRolesPayload)
	case domain.EventConversationCreated:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationCreated, domain.ResourceTypeConversation, internalEvent.AggregateID, safeConversationPayload)
	case domain.EventConversationUpdated, domain.EventConversationTopicSet, domain.EventConversationPurposeSet:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationUpdated, domain.ResourceTypeConversation, internalEvent.AggregateID, safeConversationPayload)
	case domain.EventConversationArchived:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationArchived, domain.ResourceTypeConversation, internalEvent.AggregateID, safeConversationPayload)
	case domain.EventConversationUnarchived:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationUnarchived, domain.ResourceTypeConversation, internalEvent.AggregateID, safeConversationPayload)
	case domain.EventConversationManagerAdded:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationManagerAdded, domain.ResourceTypeConversation, internalEvent.AggregateID, safeConversationManagerPayload)
	case domain.EventConversationManagerRemoved:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationManagerRemoved, domain.ResourceTypeConversation, internalEvent.AggregateID, safeConversationManagerPayload)
	case domain.EventConversationPostingPolicyUpdated:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationPostingPolicyUpdated, domain.ResourceTypeConversation, internalEvent.AggregateID, safeConversationPostingPolicyPayload)
	case domain.EventMemberJoined:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationMemberAdded, domain.ResourceTypeConversation, internalEvent.AggregateID, memberChangedPayload)
	case domain.EventMemberLeft:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationMemberRemoved, domain.ResourceTypeConversation, internalEvent.AggregateID, memberChangedPayload)
	case domain.EventMessagePosted:
		return projectMessageSnapshot(internalEvent, domain.EventTypeConversationMessageCreated)
	case domain.EventMessageUpdated:
		return projectMessageSnapshot(internalEvent, domain.EventTypeConversationMessageUpdated)
	case domain.EventMessageDeleted:
		return singleExternalEvent(internalEvent, domain.EventTypeConversationMessageDeleted, domain.ResourceTypeConversation, conversationResourceIDFromDeletedMessage(internalEvent), messageDeletePayload)
	case domain.EventReactionAdded:
		return projectReactionChange(internalEvent, domain.EventTypeConversationReactionAdded)
	case domain.EventReactionRemoved:
		return projectReactionChange(internalEvent, domain.EventTypeConversationReactionRemoved)
	case domain.EventPinAdded:
		return projectPinChange(internalEvent, domain.EventTypeConversationPinAdded)
	case domain.EventPinRemoved:
		return projectPinChange(internalEvent, domain.EventTypeConversationPinRemoved)
	case domain.EventBookmarkCreated:
		return projectBookmarkSnapshot(internalEvent, domain.EventTypeConversationBookmarkCreated)
	case domain.EventBookmarkUpdated:
		return projectBookmarkSnapshot(internalEvent, domain.EventTypeConversationBookmarkUpdated)
	case domain.EventBookmarkDeleted:
		return projectBookmarkDeleted(internalEvent)
	case domain.EventFileCreated:
		return singleExternalEvent(internalEvent, domain.EventTypeFileCreated, domain.ResourceTypeFile, internalEvent.AggregateID, safeFilePayload)
	case domain.EventFileUpdated:
		return singleExternalEvent(internalEvent, domain.EventTypeFileUpdated, domain.ResourceTypeFile, internalEvent.AggregateID, safeFilePayload)
	case domain.EventFileDeleted:
		return singleExternalEvent(internalEvent, domain.EventTypeFileDeleted, domain.ResourceTypeFile, internalEvent.AggregateID, fileDeletePayload)
	case domain.EventFileShared:
		return projectFileShared(internalEvent)
	case domain.EventSubscriptionCreated:
		return singleExternalEvent(internalEvent, domain.EventTypeEventSubscriptionCreated, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, safeEventSubscriptionPayload)
	case domain.EventSubscriptionUpdated:
		return singleExternalEvent(internalEvent, domain.EventTypeEventSubscriptionUpdated, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, safeEventSubscriptionPayload)
	case domain.EventSubscriptionDeleted:
		return singleExternalEvent(internalEvent, domain.EventTypeEventSubscriptionDeleted, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, eventSubscriptionDeletePayload)
	case domain.EventExternalPrincipalAccessGranted:
		return singleExternalEvent(internalEvent, domain.EventTypeExternalPrincipalAccessGranted, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, safeExternalPrincipalAccessPayload)
	case domain.EventExternalPrincipalAccessUpdated:
		return singleExternalEvent(internalEvent, domain.EventTypeExternalPrincipalAccessUpdated, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, safeExternalPrincipalAccessPayload)
	case domain.EventExternalPrincipalAccessRevoked:
		return singleExternalEvent(internalEvent, domain.EventTypeExternalPrincipalAccessRevoked, domain.ResourceTypeWorkspace, internalEvent.WorkspaceID, safeExternalPrincipalAccessPayload)
	default:
		return nil, nil
	}
}

func singleExternalEvent(
	internalEvent domain.InternalEvent,
	eventType, resourceType, resourceID string,
	payloadFn func(domain.InternalEvent) (json.RawMessage, error),
) ([]domain.ExternalEvent, error) {
	payload, err := payloadFn(internalEvent)
	if err != nil {
		return nil, err
	}
	return []domain.ExternalEvent{{
		WorkspaceID:                 internalEvent.WorkspaceID,
		Type:                   eventType,
		ResourceType:           resourceType,
		ResourceID:             resourceID,
		OccurredAt:             internalEvent.CreatedAt,
		Payload:                payload,
		SourceInternalEventID:  int64Ptr(internalEvent.ID),
		SourceInternalEventIDs: []int64{internalEvent.ID},
		DedupeKey:              fmt.Sprintf("internal:%d:0", internalEvent.ID),
	}}, nil
}

func projectUserUpdated(internalEvent domain.InternalEvent) ([]domain.ExternalEvent, error) {
	var user domain.User
	if err := json.Unmarshal(internalEvent.Payload, &user); err != nil {
		return nil, fmt.Errorf("decode user payload: %w", err)
	}
	if user.Deleted {
		return singleExternalEvent(internalEvent, domain.EventTypeUserDeleted, domain.ResourceTypeUser, user.ID, userDeletePayload)
	}
	return singleExternalEvent(internalEvent, domain.EventTypeUserUpdated, domain.ResourceTypeUser, user.ID, safeUserPayload)
}

func projectMessageSnapshot(internalEvent domain.InternalEvent, eventType string) ([]domain.ExternalEvent, error) {
	var msg domain.Message
	if err := json.Unmarshal(internalEvent.Payload, &msg); err != nil {
		return nil, fmt.Errorf("decode message payload: %w", err)
	}
	payload, err := safeMessagePayload(internalEvent)
	if err != nil {
		return nil, err
	}
	return []domain.ExternalEvent{{
		WorkspaceID:                 internalEvent.WorkspaceID,
		Type:                   eventType,
		ResourceType:           domain.ResourceTypeConversation,
		ResourceID:             msg.ChannelID,
		OccurredAt:             internalEvent.CreatedAt,
		Payload:                payload,
		SourceInternalEventID:  int64Ptr(internalEvent.ID),
		SourceInternalEventIDs: []int64{internalEvent.ID},
		DedupeKey:              fmt.Sprintf("internal:%d:0", internalEvent.ID),
	}}, nil
}

func projectReactionChange(internalEvent domain.InternalEvent, eventType string) ([]domain.ExternalEvent, error) {
	var payload struct {
		Reaction domain.AddReactionParams `json:"reaction"`
	}
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode reaction payload: %w", err)
	}
	body, err := marshalJSON(map[string]any{
		"channel_id": payload.Reaction.ChannelID,
		"message_ts": payload.Reaction.MessageTS,
		"user_id":    payload.Reaction.UserID,
		"emoji":      payload.Reaction.Emoji,
	})
	if err != nil {
		return nil, err
	}
	return []domain.ExternalEvent{{
		WorkspaceID:                 internalEvent.WorkspaceID,
		Type:                   eventType,
		ResourceType:           domain.ResourceTypeConversation,
		ResourceID:             payload.Reaction.ChannelID,
		OccurredAt:             internalEvent.CreatedAt,
		Payload:                body,
		SourceInternalEventID:  int64Ptr(internalEvent.ID),
		SourceInternalEventIDs: []int64{internalEvent.ID},
		DedupeKey:              fmt.Sprintf("internal:%d:0", internalEvent.ID),
	}}, nil
}

func projectPinChange(internalEvent domain.InternalEvent, eventType string) ([]domain.ExternalEvent, error) {
	body, channelID, err := pinPayloadAndChannel(internalEvent)
	if err != nil {
		return nil, err
	}
	return []domain.ExternalEvent{{
		WorkspaceID:                 internalEvent.WorkspaceID,
		Type:                   eventType,
		ResourceType:           domain.ResourceTypeConversation,
		ResourceID:             channelID,
		OccurredAt:             internalEvent.CreatedAt,
		Payload:                body,
		SourceInternalEventID:  int64Ptr(internalEvent.ID),
		SourceInternalEventIDs: []int64{internalEvent.ID},
		DedupeKey:              fmt.Sprintf("internal:%d:0", internalEvent.ID),
	}}, nil
}

func projectBookmarkSnapshot(internalEvent domain.InternalEvent, eventType string) ([]domain.ExternalEvent, error) {
	var bookmark domain.Bookmark
	if err := json.Unmarshal(internalEvent.Payload, &bookmark); err != nil {
		return nil, fmt.Errorf("decode bookmark payload: %w", err)
	}
	payload, err := marshalJSON(map[string]any{
		"id":         bookmark.ID,
		"channel_id": bookmark.ChannelID,
		"title":      bookmark.Title,
		"type":       bookmark.Type,
		"link":       bookmark.Link,
		"emoji":      bookmark.Emoji,
		"created_by": bookmark.CreatedBy,
		"updated_by": bookmark.UpdatedBy,
		"created_at": bookmark.CreatedAt,
		"updated_at": bookmark.UpdatedAt,
	})
	if err != nil {
		return nil, err
	}
	return []domain.ExternalEvent{{
		WorkspaceID:                 internalEvent.WorkspaceID,
		Type:                   eventType,
		ResourceType:           domain.ResourceTypeConversation,
		ResourceID:             bookmark.ChannelID,
		OccurredAt:             internalEvent.CreatedAt,
		Payload:                payload,
		SourceInternalEventID:  int64Ptr(internalEvent.ID),
		SourceInternalEventIDs: []int64{internalEvent.ID},
		DedupeKey:              fmt.Sprintf("internal:%d:0", internalEvent.ID),
	}}, nil
}

func projectBookmarkDeleted(internalEvent domain.InternalEvent) ([]domain.ExternalEvent, error) {
	var payload struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode bookmark deleted payload: %w", err)
	}
	body, err := marshalJSON(map[string]any{"id": payload.ID})
	if err != nil {
		return nil, err
	}
	return []domain.ExternalEvent{{
		WorkspaceID:                 internalEvent.WorkspaceID,
		Type:                   domain.EventTypeConversationBookmarkDeleted,
		ResourceType:           domain.ResourceTypeConversation,
		ResourceID:             payload.ChannelID,
		OccurredAt:             internalEvent.CreatedAt,
		Payload:                body,
		SourceInternalEventID:  int64Ptr(internalEvent.ID),
		SourceInternalEventIDs: []int64{internalEvent.ID},
		DedupeKey:              fmt.Sprintf("internal:%d:0", internalEvent.ID),
	}}, nil
}

func projectFileShared(internalEvent domain.InternalEvent) ([]domain.ExternalEvent, error) {
	var payload struct {
		FileID    string `json:"file_id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode file shared payload: %w", err)
	}
	body, err := marshalJSON(map[string]any{
		"file_id":    payload.FileID,
		"channel_id": payload.ChannelID,
	})
	if err != nil {
		return nil, err
	}
	return []domain.ExternalEvent{{
		WorkspaceID:                 internalEvent.WorkspaceID,
		Type:                   domain.EventTypeFileShared,
		ResourceType:           domain.ResourceTypeFile,
		ResourceID:             payload.FileID,
		OccurredAt:             internalEvent.CreatedAt,
		Payload:                body,
		SourceInternalEventID:  int64Ptr(internalEvent.ID),
		SourceInternalEventIDs: []int64{internalEvent.ID},
		DedupeKey:              fmt.Sprintf("internal:%d:0", internalEvent.ID),
	}}, nil
}

func safeWorkspacePayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var workspace domain.Workspace
	if err := json.Unmarshal(internalEvent.Payload, &workspace); err != nil {
		return nil, fmt.Errorf("decode workspace payload: %w", err)
	}
	return marshalJSON(map[string]any{
		"id":               workspace.ID,
		"name":             workspace.Name,
		"domain":           workspace.Domain,
		"email_domain":     workspace.EmailDomain,
		"description":      workspace.Description,
		"icon":             workspace.Icon,
		"discoverability":  workspace.Discoverability,
		"default_channels": workspace.DefaultChannels,
		"created_at":       workspace.CreatedAt,
		"updated_at":       workspace.UpdatedAt,
	})
}

func safeUserPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var user domain.User
	if err := json.Unmarshal(internalEvent.Payload, &user); err != nil {
		return nil, fmt.Errorf("decode user payload: %w", err)
	}
	return marshalJSON(user)
}

func userDeletePayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var user domain.User
	if err := json.Unmarshal(internalEvent.Payload, &user); err != nil {
		return nil, fmt.Errorf("decode user delete payload: %w", err)
	}
	return marshalJSON(map[string]any{"id": user.ID})
}

func safeUserRolesPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode user roles payload: %w", err)
	}
	return marshalJSON(payload)
}

func safeConversationPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var conversation domain.Conversation
	if err := json.Unmarshal(internalEvent.Payload, &conversation); err != nil {
		return nil, fmt.Errorf("decode conversation payload: %w", err)
	}
	return marshalJSON(conversation)
}

func safeConversationManagerPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var payload map[string]string
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode conversation manager payload: %w", err)
	}
	return marshalJSON(payload)
}

func safeConversationPostingPolicyPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var payload domain.ConversationPostingPolicy
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode conversation posting policy payload: %w", err)
	}
	return marshalJSON(payload)
}

func memberChangedPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var payload struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode member change payload: %w", err)
	}
	return marshalJSON(map[string]any{
		"conversation_id": internalEvent.AggregateID,
		"user_id":         payload.UserID,
		"actor_id":        internalEvent.ActorID,
	})
}

func safeMessagePayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var msg domain.Message
	if err := json.Unmarshal(internalEvent.Payload, &msg); err != nil {
		return nil, fmt.Errorf("decode message payload: %w", err)
	}
	return marshalJSON(msg)
}

func messageDeletePayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var msg domain.Message
	if err := json.Unmarshal(internalEvent.Payload, &msg); err != nil {
		return nil, fmt.Errorf("decode message delete payload: %w", err)
	}
	return marshalJSON(map[string]any{
		"channel_id": msg.ChannelID,
		"ts":         msg.TS,
	})
}

func safeFilePayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var file domain.File
	if err := json.Unmarshal(internalEvent.Payload, &file); err != nil {
		return nil, fmt.Errorf("decode file payload: %w", err)
	}
	return marshalJSON(map[string]any{
		"id":           file.ID,
		"workspace_id":      file.WorkspaceID,
		"name":         file.Name,
		"title":        file.Title,
		"mimetype":     file.Mimetype,
		"filetype":     file.Filetype,
		"size":         file.Size,
		"user_id":      file.UserID,
		"permalink":    file.Permalink,
		"is_external":  file.IsExternal,
		"external_url": file.ExternalURL,
		"channels":     file.Channels,
		"created_at":   file.CreatedAt,
		"updated_at":   file.UpdatedAt,
	})
}

func fileDeletePayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var file domain.File
	if err := json.Unmarshal(internalEvent.Payload, &file); err != nil {
		return nil, fmt.Errorf("decode file delete payload: %w", err)
	}
	return marshalJSON(map[string]any{"id": file.ID})
}

func safeEventSubscriptionPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var sub domain.EventSubscription
	if err := json.Unmarshal(internalEvent.Payload, &sub); err != nil {
		return nil, fmt.Errorf("decode event subscription payload: %w", err)
	}
	return marshalJSON(map[string]any{
		"id":            sub.ID,
		"workspace_id":       sub.WorkspaceID,
		"url":           sub.URL,
		"type":          sub.Type,
		"resource_type": sub.ResourceType,
		"resource_id":   sub.ResourceID,
		"enabled":       sub.Enabled,
		"created_at":    sub.CreatedAt,
		"updated_at":    sub.UpdatedAt,
	})
}

func eventSubscriptionDeletePayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode event subscription delete payload: %w", err)
	}
	return marshalJSON(map[string]any{"id": payload.ID})
}

func safeExternalPrincipalAccessPayload(internalEvent domain.InternalEvent) (json.RawMessage, error) {
	var access domain.ExternalPrincipalAccess
	if err := json.Unmarshal(internalEvent.Payload, &access); err != nil {
		return nil, fmt.Errorf("decode external principal access payload: %w", err)
	}
	return marshalJSON(access)
}

func pinPayloadAndChannel(internalEvent domain.InternalEvent) (json.RawMessage, string, error) {
	var payload struct {
		ChannelID string `json:"channel_id"`
		MessageTS string `json:"message_ts"`
		PinnedBy  string `json:"pinned_by,omitempty"`
		UserID    string `json:"user_id,omitempty"`
		PinnedAt  any    `json:"pinned_at,omitempty"`
	}
	if err := json.Unmarshal(internalEvent.Payload, &payload); err != nil {
		return nil, "", fmt.Errorf("decode pin payload: %w", err)
	}
	body, err := marshalJSON(map[string]any{
		"channel_id": payload.ChannelID,
		"message_ts": payload.MessageTS,
		"user_id":    firstNonEmpty(payload.PinnedBy, payload.UserID, internalEvent.ActorID),
	})
	if err != nil {
		return nil, "", err
	}
	return body, payload.ChannelID, nil
}

func conversationResourceIDFromDeletedMessage(internalEvent domain.InternalEvent) string {
	var msg domain.Message
	if err := json.Unmarshal(internalEvent.Payload, &msg); err == nil && msg.ChannelID != "" {
		return msg.ChannelID
	}
	return conversationIDFromAggregateID(internalEvent.AggregateID)
}

func conversationIDFromAggregateID(aggregateID string) string {
	return aggregateID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func marshalJSON(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func int64Ptr(v int64) *int64 {
	return &v
}
