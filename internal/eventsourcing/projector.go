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

// timeToTs converts a time.Time to pgtype.Timestamptz.
// Always returns Valid=true because time.Time is non-pointer (caller already
// decided the value is present). Use timePtrToTs for nullable timestamps.
func timeToTs(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// timePtrToTs converts a *time.Time to pgtype.Timestamptz.
func timePtrToTs(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
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
		domain.AggregateToken,
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
	entries, err := q.ProjectorGetEventsByAggregateType(ctx, aggregateType)
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
		if err := p.applyEvent(ctx, qtx, domainEvt); err != nil {
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
	entries, err := q.ProjectorGetEventsSince(ctx, sinceID)
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
		if err := p.applyEvent(ctx, qtx, domainEvt); err != nil {
			return fmt.Errorf("apply event id=%d type=%s: %w", entry.ID, entry.EventType, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	p.logger.Info("incremental rebuild complete", "since_id", sinceID, "events_applied", len(entries))
	return nil
}

// sqlcEventToDomain converts the sqlcgen.ServiceEvent model to domain.ServiceEvent.
func sqlcEventToDomain(e sqlcgen.ServiceEvent) domain.ServiceEvent {
	return domain.ServiceEvent{
		ID:            e.ID,
		EventType:     e.EventType,
		AggregateType: e.AggregateType,
		AggregateID:   e.AggregateID,
		TeamID:        e.TeamID,
		ActorID:       e.ActorID,
		Payload:       e.Payload,
		Metadata:      e.Metadata,
		CreatedAt:     e.CreatedAt.Time,
	}
}

func (p *Projector) truncateProjection(ctx context.Context, tx pgx.Tx, aggregateType string) error {
	switch aggregateType {
	case domain.AggregateUser:
		_, err := tx.Exec(ctx, "TRUNCATE users CASCADE")
		return err
	case domain.AggregateConversation:
		_, err := tx.Exec(ctx, "TRUNCATE conversation_members, conversations CASCADE")
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
	case domain.AggregateToken:
		_, err := tx.Exec(ctx, "TRUNCATE tokens CASCADE")
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

func (p *Projector) applyEvent(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	switch entry.EventType {
	// User events
	case domain.EventUserCreated, domain.EventUserUpdated:
		return p.applyUserUpsert(ctx, q, entry)
	case domain.EventUserDeleted:
		return p.applyUserDeleted(ctx, q, entry)

	// Conversation events
	case domain.EventConversationCreated, domain.EventConversationUpdated,
		domain.EventConversationArchived, domain.EventConversationUnarchived,
		domain.EventConversationTopicSet, domain.EventConversationPurposeSet:
		return p.applyConversationUpsert(ctx, q, entry)
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

	// Token events
	case domain.EventTokenCreated:
		return p.applyTokenCreated(ctx, q, entry)
	case domain.EventTokenRevoked:
		return p.applyTokenRevoked(ctx, q, entry)

	// Subscription events
	case domain.EventSubscriptionCreated, domain.EventSubscriptionUpdated:
		return p.applySubscriptionUpsert(ctx, q, entry)
	case domain.EventSubscriptionDeleted:
		return p.applySubscriptionDeleted(ctx, q, entry)

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

func (p *Projector) applyUserUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	var u domain.User
	if err := json.Unmarshal(entry.Payload, &u); err != nil {
		return fmt.Errorf("unmarshal user: %w", err)
	}

	profileJSON, _ := json.Marshal(u.Profile)

	return q.ProjectorUpsertUser(ctx, sqlcgen.ProjectorUpsertUserParams{
		ID:            u.ID,
		TeamID:        u.TeamID,
		Name:          u.Name,
		RealName:      u.RealName,
		DisplayName:   u.DisplayName,
		Email:         u.Email,
		IsBot:         u.IsBot,
		IsAdmin:       u.IsAdmin,
		IsOwner:       u.IsOwner,
		IsRestricted:  u.IsRestricted,
		Deleted:       u.Deleted,
		Profile:       profileJSON,
		PrincipalType: string(u.PrincipalType),
		OwnerID:       u.OwnerID,
		CreatedAt:     timeToTs(u.CreatedAt),
		UpdatedAt:     timeToTs(u.UpdatedAt),
	})
}

func (p *Projector) applyUserDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	var u domain.User
	if err := json.Unmarshal(entry.Payload, &u); err != nil {
		return fmt.Errorf("unmarshal user: %w", err)
	}
	return q.ProjectorMarkUserDeleted(ctx, sqlcgen.ProjectorMarkUserDeletedParams{
		ID:        u.ID,
		UpdatedAt: timeToTs(entry.CreatedAt),
	})
}

// --- Conversation projections ---

func (p *Projector) applyConversationUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyMemberJoined(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyMemberLeft(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

// --- Message projections ---

func (p *Projector) applyMessageUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyMessageDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyReactionAdded(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyReactionRemoved(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyUsergroupUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyUsergroupUsersSet(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyPinAdded(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyPinRemoved(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyBookmarkUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyBookmarkDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyFileUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	var f domain.File
	if err := json.Unmarshal(entry.Payload, &f); err != nil {
		return fmt.Errorf("unmarshal file: %w", err)
	}

	return q.ProjectorUpsertFile(ctx, sqlcgen.ProjectorUpsertFileParams{
		ID:                 f.ID,
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

func (p *Projector) applyFileDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	var f domain.File
	if err := json.Unmarshal(entry.Payload, &f); err != nil {
		return fmt.Errorf("unmarshal deleted file: %w", err)
	}
	return q.ProjectorDeleteFile(ctx, f.ID)
}

func (p *Projector) applyFileShared(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

// --- Token projections ---

func (p *Projector) applyTokenCreated(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	var t domain.Token
	if err := json.Unmarshal(entry.Payload, &t); err != nil {
		return fmt.Errorf("unmarshal token: %w", err)
	}

	return q.ProjectorInsertToken(ctx, sqlcgen.ProjectorInsertTokenParams{
		ID:        t.ID,
		TeamID:    t.TeamID,
		UserID:    t.UserID,
		Token:     t.Token,
		TokenHash: t.TokenHash,
		Scopes:    t.Scopes,
		IsBot:     t.IsBot,
		ExpiresAt: timePtrToTs(t.ExpiresAt),
		CreatedAt: timeToTs(t.CreatedAt),
	})
}

func (p *Projector) applyTokenRevoked(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	var t domain.Token
	if err := json.Unmarshal(entry.Payload, &t); err != nil {
		return fmt.Errorf("unmarshal revoked token: %w", err)
	}
	return q.ProjectorDeleteTokenByHash(ctx, t.TokenHash)
}

// --- Subscription projections ---

func (p *Projector) applySubscriptionUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
	var s domain.EventSubscription
	if err := json.Unmarshal(entry.Payload, &s); err != nil {
		return fmt.Errorf("unmarshal subscription: %w", err)
	}

	return q.ProjectorUpsertSubscription(ctx, sqlcgen.ProjectorUpsertSubscriptionParams{
		ID:              s.ID,
		TeamID:          s.TeamID,
		Url:             s.URL,
		EventTypes:      s.EventTypes,
		Secret:          s.Secret,
		EncryptedSecret: s.EncryptedSecret,
		Enabled:         s.Enabled,
		CreatedAt:       timeToTs(s.CreatedAt),
		UpdatedAt:       timeToTs(s.UpdatedAt),
	})
}

func (p *Projector) applySubscriptionDeleted(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

// --- API Key projections ---

func (p *Projector) applyAPIKeyUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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

func (p *Projector) applyAPIKeyRotated(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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
		RevokedAt:         timeToTs(entry.CreatedAt),
	})
}

func (p *Projector) applyAPIKeyRevoked(ctx context.Context, q *sqlcgen.Queries, entry domain.ServiceEvent) error {
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
