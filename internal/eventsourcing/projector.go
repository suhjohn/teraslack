package eventsourcing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

// Projector rebuilds projection tables by replaying events from the event log.
// It supports rebuilding individual aggregate types or all projections at once.
type Projector struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewProjector creates a new Projector.
func NewProjector(pool *pgxpool.Pool, logger *slog.Logger) *Projector {
	return &Projector{pool: pool, logger: logger}
}

// timeToTs returns the given time unchanged; it exists to keep projector code
// readable alongside generated SQL parameter names.
func timeToTs(t time.Time) time.Time {
	return t
}

// timePtrToTs returns the given pointer unchanged; it exists to keep projector
// code readable alongside nullable SQL fields.
func timePtrToTs(t *time.Time) *time.Time {
	return t
}

func timeToPgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// projStringPtrToText converts a *string to pgtype.Text.
func projStringPtrToText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// RebuildAll replays all events and rebuilds every projection table from scratch.
func (p *Projector) RebuildAll(ctx context.Context) error {
	aggregates := []string{
		domain.AggregateUser,
		domain.AggregateConversation,
		domain.AggregateMessage,
		domain.AggregateUsergroup,
		domain.AggregatePin,
		domain.AggregateBookmark,
		domain.AggregateFile,
		domain.AggregateSubscription,
		domain.AggregateAPIKey,
	}
	for _, agg := range aggregates {
		if err := p.RebuildAggregate(ctx, agg); err != nil {
			return fmt.Errorf("rebuild %s: %w", agg, err)
		}
	}
	return nil
}

// RebuildAggregate replays events for a specific aggregate type and rebuilds its projection table.
func (p *Projector) RebuildAggregate(ctx context.Context, aggregateType string) error {
	q := sqlcgen.New(p.pool)
	entries, err := q.ProjectorGetInternalEventsByAggregateType(ctx, aggregateType)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// TRUNCATE is DDL -- not supported by sqlc, so keep as raw exec.
	if err := p.truncateProjection(ctx, tx, aggregateType); err != nil {
		return fmt.Errorf("truncate projection: %w", err)
	}

	qtx := q.WithTx(tx)
	for _, entry := range entries {
		domainEvt := sqlcEventToDomain(entry)
		if err := p.applyEvent(ctx, tx, qtx, domainEvt); err != nil {
			return fmt.Errorf("apply event id=%d type=%s: %w", entry.ID, entry.EventType, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	p.logger.Info("projection rebuilt", "aggregate_type", aggregateType, "events_applied", len(entries))
	return nil
}

// RebuildSince replays events since a given event ID across all aggregate types.
func (p *Projector) RebuildSince(ctx context.Context, sinceID int64) error {
	q := sqlcgen.New(p.pool)
	entries, err := q.ProjectorGetInternalEventsSince(ctx, sinceID)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := q.WithTx(tx)
	for _, entry := range entries {
		domainEvt := sqlcEventToDomain(entry)
		if err := p.applyEvent(ctx, tx, qtx, domainEvt); err != nil {
			return fmt.Errorf("apply event id=%d type=%s: %w", entry.ID, entry.EventType, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	p.logger.Info("incremental rebuild complete", "since_id", sinceID, "events_applied", len(entries))
	return nil
}

// sqlcEventToDomain converts the sqlcgen.InternalEvent model to domain.InternalEvent.
func sqlcEventToDomain(e sqlcgen.InternalEvent) domain.InternalEvent {
	return domain.InternalEvent{
		ID:            e.ID,
		EventType:     e.EventType,
		AggregateType: e.AggregateType,
		AggregateID:   e.AggregateID,
		TeamID:        e.TeamID,
		ActorID:       e.ActorID,
		Payload:       e.Payload,
		Metadata:      e.Metadata,
		CreatedAt:     e.CreatedAt,
	}
}

func (p *Projector) truncateProjection(ctx context.Context, tx pgx.Tx, aggregateType string) error {
	switch aggregateType {
	case domain.AggregateUser:
		_, err := tx.Exec(ctx, "TRUNCATE external_principal_conversation_assignments, external_principal_access, user_role_assignments, users CASCADE")
		return err
	case domain.AggregateConversation:
		_, err := tx.Exec(ctx, "TRUNCATE conversation_posting_policies, conversation_manager_assignments, conversation_members, conversations CASCADE")
		return err
	case domain.AggregateMessage:
		_, err := tx.Exec(ctx, "TRUNCATE reactions, messages CASCADE")
		return err
	case domain.AggregateUsergroup:
		_, err := tx.Exec(ctx, "TRUNCATE usergroup_members, usergroups CASCADE")
		return err
	case domain.AggregatePin:
		_, err := tx.Exec(ctx, "TRUNCATE pins CASCADE")
		return err
	case domain.AggregateBookmark:
		_, err := tx.Exec(ctx, "TRUNCATE bookmarks CASCADE")
		return err
	case domain.AggregateFile:
		_, err := tx.Exec(ctx, "TRUNCATE file_channels, files CASCADE")
		return err
	case domain.AggregateSubscription:
		_, err := tx.Exec(ctx, "TRUNCATE event_subscriptions CASCADE")
		return err
	case domain.AggregateAPIKey:
		_, err := tx.Exec(ctx, "TRUNCATE api_keys CASCADE")
		return err
	default:
		return fmt.Errorf("unknown aggregate type: %s", aggregateType)
	}
}

func (p *Projector) applyEvent(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	switch entry.EventType {
	// User events
	case domain.EventUserCreated, domain.EventUserUpdated:
		return p.applyUserUpsert(ctx, q, entry)
	case domain.EventUserDeleted:
		return p.applyUserDeleted(ctx, q, entry)
	case domain.EventUserRolesUpdated:
		return p.applyUserRolesUpdated(ctx, tx, entry)

	// Conversation events
	case domain.EventConversationCreated, domain.EventConversationUpdated,
		domain.EventConversationArchived, domain.EventConversationUnarchived,
		domain.EventConversationTopicSet, domain.EventConversationPurposeSet:
		return p.applyConversationUpsert(ctx, q, entry)
	case domain.EventConversationManagerAdded:
		return p.applyConversationManagerAdded(ctx, q, entry)
	case domain.EventConversationManagerRemoved:
		return p.applyConversationManagerRemoved(ctx, q, entry)
	case domain.EventConversationPostingPolicyUpdated:
		return p.applyConversationPostingPolicyUpdated(ctx, q, entry)
	case domain.EventMemberJoined:
		return p.applyMemberJoined(ctx, q, entry)
	case domain.EventMemberLeft:
		return p.applyMemberLeft(ctx, q, entry)

	// Message events
	case domain.EventMessagePosted, domain.EventMessageUpdated:
		return p.applyMessageUpsert(ctx, q, entry)
	case domain.EventMessageDeleted:
		return p.applyMessageDeleted(ctx, q, entry)
	case domain.EventReactionAdded:
		return p.applyReactionAdded(ctx, q, entry)
	case domain.EventReactionRemoved:
		return p.applyReactionRemoved(ctx, q, entry)

	// Usergroup events
	case domain.EventUsergroupCreated, domain.EventUsergroupUpdated,
		domain.EventUsergroupEnabled, domain.EventUsergroupDisabled:
		return p.applyUsergroupUpsert(ctx, q, entry)
	case domain.EventUsergroupUserSet:
		return p.applyUsergroupUsersSet(ctx, q, entry)

	// Pin events
	case domain.EventPinAdded:
		return p.applyPinAdded(ctx, q, entry)
	case domain.EventPinRemoved:
		return p.applyPinRemoved(ctx, q, entry)

	// Bookmark events
	case domain.EventBookmarkCreated, domain.EventBookmarkUpdated:
		return p.applyBookmarkUpsert(ctx, q, entry)
	case domain.EventBookmarkDeleted:
		return p.applyBookmarkDeleted(ctx, q, entry)

	// File events
	case domain.EventFileCreated, domain.EventFileUpdated:
		return p.applyFileUpsert(ctx, q, entry)
	case domain.EventFileDeleted:
		return p.applyFileDeleted(ctx, q, entry)
	case domain.EventFileShared:
		return p.applyFileShared(ctx, q, entry)

	// Subscription events
	case domain.EventSubscriptionCreated, domain.EventSubscriptionUpdated:
		return p.applySubscriptionUpsert(ctx, q, entry)
	case domain.EventSubscriptionDeleted:
		return p.applySubscriptionDeleted(ctx, q, entry)
	case domain.EventExternalPrincipalAccessGranted, domain.EventExternalPrincipalAccessUpdated, domain.EventExternalPrincipalAccessRevoked:
		return p.applyExternalPrincipalAccessUpsert(ctx, tx, entry)

	// API Key events
	case domain.EventAPIKeyCreated, domain.EventAPIKeyUpdated:
		return p.applyAPIKeyUpsert(ctx, q, entry)
	case domain.EventAPIKeyRotated:
		return p.applyAPIKeyRotated(ctx, q, entry)
	case domain.EventAPIKeyRevoked:
		return p.applyAPIKeyRevoked(ctx, q, entry)

	default:
		p.logger.Warn("unknown event type", "event_type", entry.EventType, "id", entry.ID)
		return nil
	}
}

// --- User projections ---

func (p *Projector) applyUserUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var u domain.User
	if err := json.Unmarshal(entry.Payload, &u); err != nil {
		return fmt.Errorf("unmarshal user: %w", err)
	}

	profileJSON, _ := json.Marshal(u.Profile)
	accountType := u.EffectiveAccountType()

	return q.ProjectorUpsertUser(ctx, sqlcgen.ProjectorUpsertUserParams{
		ID:            u.ID,
		TeamID:        u.TeamID,
		Name:          u.Name,
		RealName:      u.RealName,
		DisplayName:   u.DisplayName,
		Email:         u.Email,
		IsBot:         u.IsBot,
		AccountType:   string(accountType),
		Deleted:       u.Deleted,
		Profile:       profileJSON,
		PrincipalType: string(u.PrincipalType),
		OwnerID:       u.OwnerID,
		CreatedAt:     timeToTs(u.CreatedAt),
		UpdatedAt:     timeToTs(u.UpdatedAt),
	})
}

func (p *Projector) applyUserDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var u domain.User
	if err := json.Unmarshal(entry.Payload, &u); err != nil {
		return fmt.Errorf("unmarshal user: %w", err)
	}
	return q.ProjectorMarkUserDeleted(ctx, sqlcgen.ProjectorMarkUserDeletedParams{
		ID:        u.ID,
		UpdatedAt: timeToTs(entry.CreatedAt),
	})
}

func (p *Projector) applyUserRolesUpdated(ctx context.Context, tx pgx.Tx, entry domain.InternalEvent) error {
	var payload struct {
		UserID         string                 `json:"user_id"`
		DelegatedRoles []domain.DelegatedRole `json:"delegated_roles"`
	}
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal user roles: %w", err)
	}
	if payload.UserID == "" {
		payload.UserID = entry.AggregateID
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_role_assignments WHERE team_id = $1 AND user_id = $2`, entry.TeamID, payload.UserID); err != nil {
		return fmt.Errorf("delete user roles: %w", err)
	}
	for _, role := range payload.DelegatedRoles {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_role_assignments (id, team_id, user_id, role_key, assigned_by, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, generateProjectionID("URA", entry.ID, string(role)), entry.TeamID, payload.UserID, string(role), entry.ActorID, entry.CreatedAt); err != nil {
			return fmt.Errorf("insert user role: %w", err)
		}
	}
	return nil
}

// --- Conversation projections ---

func (p *Projector) applyConversationUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var c domain.Conversation
	if err := json.Unmarshal(entry.Payload, &c); err != nil {
		return fmt.Errorf("unmarshal conversation: %w", err)
	}

	return q.ProjectorUpsertConversation(ctx, sqlcgen.ProjectorUpsertConversationParams{
		ID:             c.ID,
		TeamID:         c.TeamID,
		Name:           c.Name,
		Type:           string(c.Type),
		CreatorID:      c.CreatorID,
		IsArchived:     c.IsArchived,
		TopicValue:     c.Topic.Value,
		TopicCreator:   c.Topic.Creator,
		TopicLastSet:   timePtrToTs(c.Topic.LastSet),
		PurposeValue:   c.Purpose.Value,
		PurposeCreator: c.Purpose.Creator,
		PurposeLastSet: timePtrToTs(c.Purpose.LastSet),
		NumMembers:     int32(c.NumMembers),
		CreatedAt:      timeToTs(c.CreatedAt),
		UpdatedAt:      timeToTs(c.UpdatedAt),
	})
}

func (p *Projector) applyMemberJoined(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		UserID       string               `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal member joined: %w", err)
	}

	if data.Conversation != nil {
		convEntry := entry
		convData, _ := json.Marshal(data.Conversation)
		convEntry.Payload = convData
		if err := p.applyConversationUpsert(ctx, q, convEntry); err != nil {
			return err
		}
	}

	return q.ProjectorUpsertMember(ctx, sqlcgen.ProjectorUpsertMemberParams{
		ConversationID: entry.AggregateID,
		UserID:         data.UserID,
		JoinedAt:       timeToTs(entry.CreatedAt),
	})
}

func (p *Projector) applyMemberLeft(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		UserID       string               `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal member left: %w", err)
	}

	if data.Conversation != nil {
		convEntry := entry
		convData, _ := json.Marshal(data.Conversation)
		convEntry.Payload = convData
		if err := p.applyConversationUpsert(ctx, q, convEntry); err != nil {
			return err
		}
	}

	return q.ProjectorDeleteMember(ctx, sqlcgen.ProjectorDeleteMemberParams{
		ConversationID: entry.AggregateID,
		UserID:         data.UserID,
	})
}

func (p *Projector) applyConversationManagerAdded(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		ConversationID string `json:"conversation_id"`
		UserID         string `json:"user_id"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal conversation manager added: %w", err)
	}
	return q.ProjectorUpsertConversationManager(ctx, sqlcgen.ProjectorUpsertConversationManagerParams{
		ConversationID: data.ConversationID,
		UserID:         data.UserID,
		AssignedBy:     entry.ActorID,
		CreatedAt:      timeToPgTimestamptz(entry.CreatedAt),
	})
}

func (p *Projector) applyConversationManagerRemoved(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		ConversationID string `json:"conversation_id"`
		UserID         string `json:"user_id"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal conversation manager removed: %w", err)
	}
	return q.ProjectorDeleteConversationManager(ctx, sqlcgen.ProjectorDeleteConversationManagerParams{
		ConversationID: data.ConversationID,
		UserID:         data.UserID,
	})
}

func (p *Projector) applyConversationPostingPolicyUpdated(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var policy domain.ConversationPostingPolicy
	if err := json.Unmarshal(entry.Payload, &policy); err != nil {
		return fmt.Errorf("unmarshal conversation posting policy: %w", err)
	}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal conversation posting policy: %w", err)
	}
	return q.ProjectorUpsertConversationPostingPolicy(ctx, sqlcgen.ProjectorUpsertConversationPostingPolicyParams{
		ConversationID: policy.ConversationID,
		PolicyType:     string(policy.PolicyType),
		PolicyJson:     policyJSON,
		UpdatedBy:      policy.UpdatedBy,
		UpdatedAt:      timeToPgTimestamptz(policy.UpdatedAt),
	})
}

// --- Message projections ---

func (p *Projector) applyMessageUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var m domain.Message
	if err := json.Unmarshal(entry.Payload, &m); err != nil {
		return fmt.Errorf("unmarshal message: %w", err)
	}

	blocksJSON := []byte("null")
	if m.Blocks != nil {
		blocksJSON = m.Blocks
	}
	metadataJSON := []byte("null")
	if m.Metadata != nil {
		metadataJSON = m.Metadata
	}

	return q.ProjectorUpsertMessage(ctx, sqlcgen.ProjectorUpsertMessageParams{
		Ts:              m.TS,
		ChannelID:       m.ChannelID,
		UserID:          m.UserID,
		Text:            m.Text,
		ThreadTs:        projStringPtrToText(m.ThreadTS),
		Type:            m.Type,
		Subtype:         projStringPtrToText(m.Subtype),
		Blocks:          blocksJSON,
		Metadata:        metadataJSON,
		EditedBy:        projStringPtrToText(m.EditedBy),
		EditedAt:        projStringPtrToText(m.EditedAt),
		ReplyCount:      int32(m.ReplyCount),
		ReplyUsersCount: int32(m.ReplyUsersCount),
		LatestReply:     projStringPtrToText(m.LatestReply),
		IsDeleted:       m.IsDeleted,
		CreatedAt:       timeToTs(m.CreatedAt),
		UpdatedAt:       timeToTs(m.UpdatedAt),
	})
}

func (p *Projector) applyMessageDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var m domain.Message
	if err := json.Unmarshal(entry.Payload, &m); err != nil {
		return fmt.Errorf("unmarshal deleted message: %w", err)
	}
	return q.ProjectorMarkMessageDeleted(ctx, sqlcgen.ProjectorMarkMessageDeletedParams{
		ChannelID: m.ChannelID,
		Ts:        m.TS,
		UpdatedAt: timeToTs(entry.CreatedAt),
	})
}

func (p *Projector) applyReactionAdded(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		Reaction domain.AddReactionParams `json:"reaction"`
		Message  *domain.Message          `json:"message"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal reaction added: %w", err)
	}

	return q.ProjectorUpsertReaction(ctx, sqlcgen.ProjectorUpsertReactionParams{
		ChannelID: data.Reaction.ChannelID,
		MessageTs: data.Reaction.MessageTS,
		UserID:    data.Reaction.UserID,
		Emoji:     data.Reaction.Emoji,
		CreatedAt: timeToTs(entry.CreatedAt),
	})
}

func (p *Projector) applyReactionRemoved(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		Reaction domain.RemoveReactionParams `json:"reaction"`
		Message  *domain.Message             `json:"message"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal reaction removed: %w", err)
	}

	return q.ProjectorDeleteReaction(ctx, sqlcgen.ProjectorDeleteReactionParams{
		ChannelID: data.Reaction.ChannelID,
		MessageTs: data.Reaction.MessageTS,
		UserID:    data.Reaction.UserID,
		Emoji:     data.Reaction.Emoji,
	})
}

// --- Usergroup projections ---

func (p *Projector) applyUsergroupUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var ug domain.Usergroup
	if err := json.Unmarshal(entry.Payload, &ug); err != nil {
		return fmt.Errorf("unmarshal usergroup: %w", err)
	}

	return q.ProjectorUpsertUsergroup(ctx, sqlcgen.ProjectorUpsertUsergroupParams{
		ID:          ug.ID,
		TeamID:      ug.TeamID,
		Name:        ug.Name,
		Handle:      ug.Handle,
		Description: ug.Description,
		IsExternal:  ug.IsExternal,
		Enabled:     ug.Enabled,
		UserCount:   int32(ug.UserCount),
		CreatedBy:   ug.CreatedBy,
		UpdatedBy:   ug.UpdatedBy,
		CreatedAt:   timeToTs(ug.CreatedAt),
		UpdatedAt:   timeToTs(ug.UpdatedAt),
	})
}

func (p *Projector) applyUsergroupUsersSet(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		UserIDs   []string          `json:"user_ids"`
		Usergroup *domain.Usergroup `json:"usergroup"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal usergroup users set: %w", err)
	}

	if data.Usergroup != nil {
		ugEntry := entry
		ugData, _ := json.Marshal(data.Usergroup)
		ugEntry.Payload = ugData
		if err := p.applyUsergroupUpsert(ctx, q, ugEntry); err != nil {
			return err
		}
	}

	if err := q.ProjectorDeleteUsergroupMembers(ctx, entry.AggregateID); err != nil {
		return err
	}
	for _, uid := range data.UserIDs {
		if err := q.ProjectorUpsertUsergroupMember(ctx, sqlcgen.ProjectorUpsertUsergroupMemberParams{
			UsergroupID: entry.AggregateID,
			UserID:      uid,
			AddedAt:     timeToTs(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

// --- Pin projections ---

func (p *Projector) applyPinAdded(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var pin domain.Pin
	if err := json.Unmarshal(entry.Payload, &pin); err != nil {
		return fmt.Errorf("unmarshal pin: %w", err)
	}

	return q.ProjectorUpsertPin(ctx, sqlcgen.ProjectorUpsertPinParams{
		ChannelID: pin.ChannelID,
		MessageTs: pin.MessageTS,
		PinnedBy:  pin.PinnedBy,
		PinnedAt:  timeToTs(pin.PinnedAt),
	})
}

func (p *Projector) applyPinRemoved(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		ChannelID string `json:"channel_id"`
		MessageTS string `json:"message_ts"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal pin removed: %w", err)
	}

	return q.ProjectorDeletePin(ctx, sqlcgen.ProjectorDeletePinParams{
		ChannelID: data.ChannelID,
		MessageTs: data.MessageTS,
	})
}

// --- Bookmark projections ---

func (p *Projector) applyBookmarkUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var b domain.Bookmark
	if err := json.Unmarshal(entry.Payload, &b); err != nil {
		return fmt.Errorf("unmarshal bookmark: %w", err)
	}

	return q.ProjectorUpsertBookmark(ctx, sqlcgen.ProjectorUpsertBookmarkParams{
		ID:        b.ID,
		ChannelID: b.ChannelID,
		Title:     b.Title,
		Type:      b.Type,
		Link:      b.Link,
		Emoji:     b.Emoji,
		CreatedBy: b.CreatedBy,
		UpdatedBy: b.UpdatedBy,
		CreatedAt: timeToTs(b.CreatedAt),
		UpdatedAt: timeToTs(b.UpdatedAt),
	})
}

func (p *Projector) applyBookmarkDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var b domain.Bookmark
	if err := json.Unmarshal(entry.Payload, &b); err != nil {
		return fmt.Errorf("unmarshal deleted bookmark: %w", err)
	}
	id := b.ID
	if id == "" {
		var m map[string]string
		if err := json.Unmarshal(entry.Payload, &m); err == nil {
			id = m["bookmark_id"]
		}
	}
	if id == "" {
		id = entry.AggregateID
	}
	return q.ProjectorDeleteBookmark(ctx, id)
}

// --- File projections ---

func (p *Projector) applyFileUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var f domain.File
	if err := json.Unmarshal(entry.Payload, &f); err != nil {
		return fmt.Errorf("unmarshal file: %w", err)
	}
	if f.TeamID == "" {
		if entry.TeamID != "" {
			f.TeamID = entry.TeamID
		} else if user, err := q.GetUser(ctx, f.UserID); err == nil {
			f.TeamID = user.TeamID
		}
	}
	if f.TeamID == "" {
		return fmt.Errorf("file team_id missing for file %s", f.ID)
	}

	return q.ProjectorUpsertFile(ctx, sqlcgen.ProjectorUpsertFileParams{
		ID:                 f.ID,
		TeamID:             f.TeamID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		UrlPrivate:         f.URLPrivate,
		UrlPrivateDownload: f.URLPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalUrl:        f.ExternalURL,
		CreatedAt:          timeToTs(f.CreatedAt),
		UpdatedAt:          timeToTs(f.UpdatedAt),
	})
}

func (p *Projector) applyFileDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var f domain.File
	if err := json.Unmarshal(entry.Payload, &f); err != nil {
		return fmt.Errorf("unmarshal deleted file: %w", err)
	}
	return q.ProjectorDeleteFile(ctx, f.ID)
}

func (p *Projector) applyFileShared(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		FileID    string `json:"file_id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal file shared: %w", err)
	}

	return q.ProjectorUpsertFileChannel(ctx, sqlcgen.ProjectorUpsertFileChannelParams{
		FileID:    data.FileID,
		ChannelID: data.ChannelID,
		SharedAt:  timeToTs(entry.CreatedAt),
	})
}

// --- Subscription projections ---

func (p *Projector) applySubscriptionUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var s domain.EventSubscription
	if err := json.Unmarshal(entry.Payload, &s); err != nil {
		return fmt.Errorf("unmarshal subscription: %w", err)
	}

	return q.ProjectorUpsertSubscription(ctx, sqlcgen.ProjectorUpsertSubscriptionParams{
		ID:              s.ID,
		TeamID:          s.TeamID,
		Url:             s.URL,
		EventType:       s.Type,
		ResourceType:    s.ResourceType,
		ResourceID:      s.ResourceID,
		EncryptedSecret: s.EncryptedSecret,
		Enabled:         s.Enabled,
		CreatedAt:       timeToTs(s.CreatedAt),
		UpdatedAt:       timeToTs(s.UpdatedAt),
	})
}

func (p *Projector) applySubscriptionDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var s domain.EventSubscription
	if err := json.Unmarshal(entry.Payload, &s); err != nil {
		return fmt.Errorf("unmarshal deleted subscription: %w", err)
	}
	id := s.ID
	if id == "" {
		var m map[string]string
		if err := json.Unmarshal(entry.Payload, &m); err == nil {
			id = m["subscription_id"]
		}
	}
	if id == "" {
		id = entry.AggregateID
	}
	return q.ProjectorDeleteSubscription(ctx, id)
}

func (p *Projector) applyExternalPrincipalAccessUpsert(ctx context.Context, tx pgx.Tx, entry domain.InternalEvent) error {
	var access domain.ExternalPrincipalAccess
	if err := json.Unmarshal(entry.Payload, &access); err != nil {
		return fmt.Errorf("unmarshal external principal access: %w", err)
	}
	capsJSON, err := json.Marshal(access.AllowedCapabilities)
	if err != nil {
		return fmt.Errorf("marshal external principal capabilities: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO external_principal_access (
			id, host_team_id, principal_id, principal_type, home_team_id, access_mode,
			allowed_capabilities, granted_by, created_at, expires_at, revoked_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			host_team_id = EXCLUDED.host_team_id,
			principal_id = EXCLUDED.principal_id,
			principal_type = EXCLUDED.principal_type,
			home_team_id = EXCLUDED.home_team_id,
			access_mode = EXCLUDED.access_mode,
			allowed_capabilities = EXCLUDED.allowed_capabilities,
			granted_by = EXCLUDED.granted_by,
			expires_at = EXCLUDED.expires_at,
			revoked_at = EXCLUDED.revoked_at
	`, access.ID, access.HostTeamID, access.PrincipalID, string(access.PrincipalType), access.HomeTeamID, string(access.AccessMode), capsJSON, access.GrantedBy, access.CreatedAt, access.ExpiresAt, access.RevokedAt); err != nil {
		return fmt.Errorf("upsert external principal access: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM external_principal_conversation_assignments WHERE access_id = $1`, access.ID); err != nil {
		return fmt.Errorf("delete external principal conversation assignments: %w", err)
	}
	for _, conversationID := range access.ConversationIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO external_principal_conversation_assignments (access_id, conversation_id, granted_by, created_at)
			VALUES ($1, $2, $3, $4)
		`, access.ID, conversationID, access.GrantedBy, entry.CreatedAt); err != nil {
			return fmt.Errorf("insert external principal conversation assignment: %w", err)
		}
	}
	return nil
}

func generateProjectionID(prefix string, eventID int64, suffix string) string {
	if suffix == "" {
		return fmt.Sprintf("%s_%d", prefix, eventID)
	}
	return fmt.Sprintf("%s_%d_%s", prefix, eventID, suffix)
}

// --- API Key projections ---

func (p *Projector) applyAPIKeyUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var k domain.APIKey
	if err := json.Unmarshal(entry.Payload, &k); err != nil {
		return fmt.Errorf("unmarshal api key: %w", err)
	}

	return q.ProjectorUpsertAPIKey(ctx, sqlcgen.ProjectorUpsertAPIKeyParams{
		ID:                k.ID,
		Name:              k.Name,
		Description:       k.Description,
		KeyHash:           k.KeyHash,
		KeyPrefix:         k.KeyPrefix,
		KeyHint:           k.KeyHint,
		TeamID:            k.TeamID,
		PrincipalID:       k.PrincipalID,
		CreatedBy:         k.CreatedBy,
		OnBehalfOf:        k.OnBehalfOf,
		Type:              string(k.Type),
		Environment:       string(k.Environment),
		Permissions:       k.Permissions,
		ExpiresAt:         timePtrToTs(k.ExpiresAt),
		LastUsedAt:        timePtrToTs(k.LastUsedAt),
		RequestCount:      k.RequestCount,
		Revoked:           k.Revoked,
		RevokedAt:         timePtrToTs(k.RevokedAt),
		RotatedToID:       k.RotatedToID,
		GracePeriodEndsAt: timePtrToTs(k.GracePeriodEndsAt),
		CreatedAt:         timeToTs(k.CreatedAt),
		UpdatedAt:         timeToTs(k.UpdatedAt),
	})
}

func (p *Projector) applyAPIKeyRotated(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		OldKeyID          string     `json:"old_key_id"`
		NewKeyID          string     `json:"new_key_id"`
		GracePeriodEndsAt *time.Time `json:"grace_period_ends_at"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal api key rotated: %w", err)
	}
	return q.ProjectorMarkAPIKeyRotated(ctx, sqlcgen.ProjectorMarkAPIKeyRotatedParams{
		ID:                data.OldKeyID,
		RotatedToID:       data.NewKeyID,
		GracePeriodEndsAt: timePtrToTs(data.GracePeriodEndsAt),
		RevokedAt:         timePtrToTs(&entry.CreatedAt),
	})
}

func (p *Projector) applyAPIKeyRevoked(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var k domain.APIKey
	if err := json.Unmarshal(entry.Payload, &k); err != nil {
		return fmt.Errorf("unmarshal revoked api key: %w", err)
	}
	return q.ProjectorMarkAPIKeyRevoked(ctx, sqlcgen.ProjectorMarkAPIKeyRevokedParams{
		ID:        k.ID,
		RevokedAt: timePtrToTs(k.RevokedAt),
		UpdatedAt: timeToTs(entry.CreatedAt),
	})
}
