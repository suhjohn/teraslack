package eventsourcing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
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
	// Collect all events first to avoid "conn busy" — pgx doesn't allow
	// executing statements while iterating rows on the same connection.
	entries, err := p.collectEvents(ctx, aggregateType)
	if err != nil {
		return err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Truncate the projection table(s) for this aggregate
	if err := p.truncateProjection(ctx, tx, aggregateType); err != nil {
		return fmt.Errorf("truncate projection: %w", err)
	}

	for i, entry := range entries {
		if err := p.applyEvent(ctx, tx, entry); err != nil {
			return fmt.Errorf("apply event seq=%d type=%s: %w", entry.SequenceID, entry.EventType, err)
		}
		_ = i
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	p.logger.Info("projection rebuilt", "aggregate_type", aggregateType, "events_applied", len(entries))
	return nil
}

// collectEvents reads all events for an aggregate type into memory.
func (p *Projector) collectEvents(ctx context.Context, aggregateType string) ([]domain.EventLogEntry, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT sequence_id, aggregate_type, aggregate_id, event_type, event_data, metadata, created_at
		 FROM event_log
		 WHERE aggregate_type = $1
		 ORDER BY sequence_id ASC`, aggregateType)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var entries []domain.EventLogEntry
	for rows.Next() {
		var entry domain.EventLogEntry
		if err := rows.Scan(
			&entry.SequenceID,
			&entry.AggregateType,
			&entry.AggregateID,
			&entry.EventType,
			&entry.EventData,
			&entry.Metadata,
			&entry.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return entries, nil
}

// RebuildSince replays events since a given sequence_id across all aggregate types.
// This is useful for incremental rebuilds.
func (p *Projector) RebuildSince(ctx context.Context, sinceSequenceID int64) error {
	// Collect events first to avoid conn busy.
	rows, err := p.pool.Query(ctx,
		`SELECT sequence_id, aggregate_type, aggregate_id, event_type, event_data, metadata, created_at
		 FROM event_log
		 WHERE sequence_id > $1
		 ORDER BY sequence_id ASC`, sinceSequenceID)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	var entries []domain.EventLogEntry
	for rows.Next() {
		var entry domain.EventLogEntry
		if err := rows.Scan(
			&entry.SequenceID,
			&entry.AggregateType,
			&entry.AggregateID,
			&entry.EventType,
			&entry.EventData,
			&entry.Metadata,
			&entry.CreatedAt,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan event: %w", err)
		}
		entries = append(entries, entry)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate events: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, entry := range entries {
		if err := p.applyEvent(ctx, tx, entry); err != nil {
			return fmt.Errorf("apply event seq=%d type=%s: %w", entry.SequenceID, entry.EventType, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	p.logger.Info("incremental rebuild complete", "since_sequence_id", sinceSequenceID, "events_applied", len(entries))
	return nil
}

func (p *Projector) truncateProjection(ctx context.Context, tx pgx.Tx, aggregateType string) error {
	// Use TRUNCATE ... CASCADE to handle foreign key constraints.
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
	default:
		return fmt.Errorf("unknown aggregate type: %s", aggregateType)
	}
}

func (p *Projector) applyEvent(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	switch entry.EventType {
	// User events
	case domain.EventUserCreated, domain.EventUserUpdated:
		return p.applyUserUpsert(ctx, tx, entry)
	case domain.EventUserDeleted:
		return p.applyUserDeleted(ctx, tx, entry)

	// Conversation events
	case domain.EventConversationCreated, domain.EventConversationUpdated,
		domain.EventConversationArchived, domain.EventConversationUnarchived,
		domain.EventConversationTopicSet, domain.EventConversationPurposeSet:
		return p.applyConversationUpsert(ctx, tx, entry)
	case domain.EventMemberJoined:
		return p.applyMemberJoined(ctx, tx, entry)
	case domain.EventMemberLeft:
		return p.applyMemberLeft(ctx, tx, entry)

	// Message events
	case domain.EventMessagePosted, domain.EventMessageUpdated:
		return p.applyMessageUpsert(ctx, tx, entry)
	case domain.EventMessageDeleted:
		return p.applyMessageDeleted(ctx, tx, entry)
	case domain.EventReactionAdded:
		return p.applyReactionAdded(ctx, tx, entry)
	case domain.EventReactionRemoved:
		return p.applyReactionRemoved(ctx, tx, entry)

	// Usergroup events
	case domain.EventUsergroupCreated, domain.EventUsergroupUpdated,
		domain.EventUsergroupEnabled, domain.EventUsergroupDisabled:
		return p.applyUsergroupUpsert(ctx, tx, entry)
	case domain.EventUsergroupUserSet:
		return p.applyUsergroupUsersSet(ctx, tx, entry)

	// Pin events
	case domain.EventPinAdded:
		return p.applyPinAdded(ctx, tx, entry)
	case domain.EventPinRemoved:
		return p.applyPinRemoved(ctx, tx, entry)

	// Bookmark events
	case domain.EventBookmarkCreated, domain.EventBookmarkUpdated:
		return p.applyBookmarkUpsert(ctx, tx, entry)
	case domain.EventBookmarkDeleted:
		return p.applyBookmarkDeleted(ctx, tx, entry)

	// File events
	case domain.EventFileCreated, domain.EventFileUpdated:
		return p.applyFileUpsert(ctx, tx, entry)
	case domain.EventFileDeleted:
		return p.applyFileDeleted(ctx, tx, entry)
	case domain.EventFileShared:
		return p.applyFileShared(ctx, tx, entry)

	// Token events
	case domain.EventTokenCreated:
		return p.applyTokenCreated(ctx, tx, entry)
	case domain.EventTokenRevoked:
		return p.applyTokenRevoked(ctx, tx, entry)

	// Subscription events
	case domain.EventSubscriptionCreated, domain.EventSubscriptionUpdated:
		return p.applySubscriptionUpsert(ctx, tx, entry)
	case domain.EventSubscriptionDeleted:
		return p.applySubscriptionDeleted(ctx, tx, entry)

	default:
		p.logger.Warn("unknown event type", "event_type", entry.EventType, "sequence_id", entry.SequenceID)
		return nil
	}
}

// --- User projections ---

func (p *Projector) applyUserUpsert(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var u domain.User
	if err := json.Unmarshal(entry.EventData, &u); err != nil {
		return fmt.Errorf("unmarshal user: %w", err)
	}

	profileJSON, _ := json.Marshal(u.Profile)

	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, team_id, name, real_name, display_name, email, is_bot, is_admin, is_owner, is_restricted, deleted, profile, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO UPDATE SET
			team_id = EXCLUDED.team_id, name = EXCLUDED.name, real_name = EXCLUDED.real_name,
			display_name = EXCLUDED.display_name, email = EXCLUDED.email, is_bot = EXCLUDED.is_bot,
			is_admin = EXCLUDED.is_admin, is_owner = EXCLUDED.is_owner, is_restricted = EXCLUDED.is_restricted,
			deleted = EXCLUDED.deleted, profile = EXCLUDED.profile, updated_at = EXCLUDED.updated_at`,
		u.ID, u.TeamID, u.Name, u.RealName, u.DisplayName, u.Email,
		u.IsBot, u.IsAdmin, u.IsOwner, u.IsRestricted, u.Deleted,
		profileJSON, u.CreatedAt, u.UpdatedAt)
	return err
}

func (p *Projector) applyUserDeleted(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var u domain.User
	if err := json.Unmarshal(entry.EventData, &u); err != nil {
		return fmt.Errorf("unmarshal user: %w", err)
	}
	_, err := tx.Exec(ctx, `UPDATE users SET deleted = TRUE, updated_at = $2 WHERE id = $1`, u.ID, entry.CreatedAt)
	return err
}

// --- Conversation projections ---

func (p *Projector) applyConversationUpsert(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var c domain.Conversation
	if err := json.Unmarshal(entry.EventData, &c); err != nil {
		return fmt.Errorf("unmarshal conversation: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO conversations (id, team_id, name, type, creator_id, is_archived,
			topic_value, topic_creator, topic_last_set,
			purpose_value, purpose_creator, purpose_last_set,
			num_members, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			team_id = EXCLUDED.team_id, name = EXCLUDED.name, type = EXCLUDED.type,
			creator_id = EXCLUDED.creator_id, is_archived = EXCLUDED.is_archived,
			topic_value = EXCLUDED.topic_value, topic_creator = EXCLUDED.topic_creator,
			topic_last_set = EXCLUDED.topic_last_set,
			purpose_value = EXCLUDED.purpose_value, purpose_creator = EXCLUDED.purpose_creator,
			purpose_last_set = EXCLUDED.purpose_last_set,
			num_members = EXCLUDED.num_members, updated_at = EXCLUDED.updated_at`,
		c.ID, c.TeamID, c.Name, string(c.Type), c.CreatorID, c.IsArchived,
		c.Topic.Value, c.Topic.Creator, c.Topic.LastSet,
		c.Purpose.Value, c.Purpose.Creator, c.Purpose.LastSet,
		c.NumMembers, c.CreatedAt, c.UpdatedAt)
	return err
}

func (p *Projector) applyMemberJoined(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var data struct {
		UserID       string              `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}
	if err := json.Unmarshal(entry.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal member joined: %w", err)
	}

	// Upsert the conversation state
	if data.Conversation != nil {
		convEntry := entry
		convData, _ := json.Marshal(data.Conversation)
		convEntry.EventData = convData
		if err := p.applyConversationUpsert(ctx, tx, convEntry); err != nil {
			return err
		}
	}

	// Insert the membership
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_members (conversation_id, user_id, joined_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (conversation_id, user_id) DO NOTHING`,
		entry.AggregateID, data.UserID, entry.CreatedAt)
	return err
}

func (p *Projector) applyMemberLeft(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var data struct {
		UserID       string              `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}
	if err := json.Unmarshal(entry.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal member left: %w", err)
	}

	// Upsert the conversation state
	if data.Conversation != nil {
		convEntry := entry
		convData, _ := json.Marshal(data.Conversation)
		convEntry.EventData = convData
		if err := p.applyConversationUpsert(ctx, tx, convEntry); err != nil {
			return err
		}
	}

	// Delete the membership
	_, err := tx.Exec(ctx, `
		DELETE FROM conversation_members WHERE conversation_id = $1 AND user_id = $2`,
		entry.AggregateID, data.UserID)
	return err
}

// --- Message projections ---

func (p *Projector) applyMessageUpsert(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var m domain.Message
	if err := json.Unmarshal(entry.EventData, &m); err != nil {
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

	_, err := tx.Exec(ctx, `
		INSERT INTO messages (ts, channel_id, user_id, text, thread_ts, type, subtype,
			blocks, metadata, edited_by, edited_at, reply_count, reply_users_count,
			latest_reply, is_deleted, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (channel_id, ts) DO UPDATE SET
			user_id = EXCLUDED.user_id, text = EXCLUDED.text, thread_ts = EXCLUDED.thread_ts,
			type = EXCLUDED.type, subtype = EXCLUDED.subtype, blocks = EXCLUDED.blocks,
			metadata = EXCLUDED.metadata, edited_by = EXCLUDED.edited_by,
			edited_at = EXCLUDED.edited_at, reply_count = EXCLUDED.reply_count,
			reply_users_count = EXCLUDED.reply_users_count, latest_reply = EXCLUDED.latest_reply,
			is_deleted = EXCLUDED.is_deleted, updated_at = EXCLUDED.updated_at`,
		m.TS, m.ChannelID, m.UserID, m.Text, m.ThreadTS, m.Type, m.Subtype,
		blocksJSON, metadataJSON, m.EditedBy, m.EditedAt,
		m.ReplyCount, m.ReplyUsersCount, m.LatestReply,
		m.IsDeleted, m.CreatedAt, m.UpdatedAt)
	return err
}

func (p *Projector) applyMessageDeleted(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var m domain.Message
	if err := json.Unmarshal(entry.EventData, &m); err != nil {
		return fmt.Errorf("unmarshal deleted message: %w", err)
	}
	_, err := tx.Exec(ctx, `UPDATE messages SET is_deleted = TRUE, updated_at = $3 WHERE channel_id = $1 AND ts = $2`,
		m.ChannelID, m.TS, entry.CreatedAt)
	return err
}

func (p *Projector) applyReactionAdded(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var data struct {
		Reaction domain.AddReactionParams `json:"reaction"`
		Message  *domain.Message          `json:"message"`
	}
	if err := json.Unmarshal(entry.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal reaction added: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO reactions (channel_id, message_ts, user_id, emoji, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (channel_id, message_ts, user_id, emoji) DO NOTHING`,
		data.Reaction.ChannelID, data.Reaction.MessageTS,
		data.Reaction.UserID, data.Reaction.Emoji, entry.CreatedAt)
	return err
}

func (p *Projector) applyReactionRemoved(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var data struct {
		Reaction domain.RemoveReactionParams `json:"reaction"`
		Message  *domain.Message             `json:"message"`
	}
	if err := json.Unmarshal(entry.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal reaction removed: %w", err)
	}

	_, err := tx.Exec(ctx, `
		DELETE FROM reactions WHERE channel_id = $1 AND message_ts = $2 AND user_id = $3 AND emoji = $4`,
		data.Reaction.ChannelID, data.Reaction.MessageTS,
		data.Reaction.UserID, data.Reaction.Emoji)
	return err
}

// --- Usergroup projections ---

func (p *Projector) applyUsergroupUpsert(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var ug domain.Usergroup
	if err := json.Unmarshal(entry.EventData, &ug); err != nil {
		return fmt.Errorf("unmarshal usergroup: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO usergroups (id, team_id, name, handle, description, is_external, enabled, user_count, created_by, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			team_id = EXCLUDED.team_id, name = EXCLUDED.name, handle = EXCLUDED.handle,
			description = EXCLUDED.description, is_external = EXCLUDED.is_external,
			enabled = EXCLUDED.enabled, user_count = EXCLUDED.user_count,
			updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		ug.ID, ug.TeamID, ug.Name, ug.Handle, ug.Description,
		ug.IsExternal, ug.Enabled, ug.UserCount,
		ug.CreatedBy, ug.UpdatedBy, ug.CreatedAt, ug.UpdatedAt)
	return err
}

func (p *Projector) applyUsergroupUsersSet(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var data struct {
		UserIDs   []string         `json:"user_ids"`
		Usergroup *domain.Usergroup `json:"usergroup"`
	}
	if err := json.Unmarshal(entry.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal usergroup users set: %w", err)
	}

	// Upsert the usergroup state
	if data.Usergroup != nil {
		ugEntry := entry
		ugData, _ := json.Marshal(data.Usergroup)
		ugEntry.EventData = ugData
		if err := p.applyUsergroupUpsert(ctx, tx, ugEntry); err != nil {
			return err
		}
	}

	// Replace membership
	_, err := tx.Exec(ctx, `DELETE FROM usergroup_members WHERE usergroup_id = $1`, entry.AggregateID)
	if err != nil {
		return err
	}
	for _, uid := range data.UserIDs {
		_, err := tx.Exec(ctx, `
			INSERT INTO usergroup_members (usergroup_id, user_id, added_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (usergroup_id, user_id) DO NOTHING`,
			entry.AggregateID, uid, entry.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

// --- Pin projections ---

func (p *Projector) applyPinAdded(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var pin domain.Pin
	if err := json.Unmarshal(entry.EventData, &pin); err != nil {
		return fmt.Errorf("unmarshal pin: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO pins (channel_id, message_ts, pinned_by, pinned_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id, message_ts) DO NOTHING`,
		pin.ChannelID, pin.MessageTS, pin.PinnedBy, pin.PinnedAt)
	return err
}

func (p *Projector) applyPinRemoved(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var data struct {
		ChannelID string `json:"channel_id"`
		MessageTS string `json:"message_ts"`
	}
	if err := json.Unmarshal(entry.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal pin removed: %w", err)
	}

	_, err := tx.Exec(ctx, `DELETE FROM pins WHERE channel_id = $1 AND message_ts = $2`,
		data.ChannelID, data.MessageTS)
	return err
}

// --- Bookmark projections ---

func (p *Projector) applyBookmarkUpsert(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var b domain.Bookmark
	if err := json.Unmarshal(entry.EventData, &b); err != nil {
		return fmt.Errorf("unmarshal bookmark: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO bookmarks (id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			channel_id = EXCLUDED.channel_id, title = EXCLUDED.title, type = EXCLUDED.type,
			link = EXCLUDED.link, emoji = EXCLUDED.emoji,
			updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		b.ID, b.ChannelID, b.Title, b.Type, b.Link, b.Emoji,
		b.CreatedBy, b.UpdatedBy, b.CreatedAt, b.UpdatedAt)
	return err
}

func (p *Projector) applyBookmarkDeleted(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var b domain.Bookmark
	if err := json.Unmarshal(entry.EventData, &b); err != nil {
		return fmt.Errorf("unmarshal deleted bookmark: %w", err)
	}
	_, err := tx.Exec(ctx, `DELETE FROM bookmarks WHERE id = $1`, b.ID)
	return err
}

// --- File projections ---

func (p *Projector) applyFileUpsert(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var f domain.File
	if err := json.Unmarshal(entry.EventData, &f); err != nil {
		return fmt.Errorf("unmarshal file: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO files (id, name, title, mimetype, filetype, size, user_id, s3_key,
			url_private, url_private_download, permalink, is_external, external_url, upload_complete, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '', $8, $9, $10, $11, $12, TRUE, $13, $14)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, title = EXCLUDED.title, mimetype = EXCLUDED.mimetype,
			filetype = EXCLUDED.filetype, size = EXCLUDED.size, user_id = EXCLUDED.user_id,
			url_private = EXCLUDED.url_private, url_private_download = EXCLUDED.url_private_download,
			permalink = EXCLUDED.permalink, is_external = EXCLUDED.is_external,
			external_url = EXCLUDED.external_url, updated_at = EXCLUDED.updated_at`,
		f.ID, f.Name, f.Title, f.Mimetype, f.Filetype, f.Size, f.UserID,
		f.URLPrivate, f.URLPrivateDownload, f.Permalink,
		f.IsExternal, f.ExternalURL, f.CreatedAt, f.UpdatedAt)
	return err
}

func (p *Projector) applyFileDeleted(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var f domain.File
	if err := json.Unmarshal(entry.EventData, &f); err != nil {
		return fmt.Errorf("unmarshal deleted file: %w", err)
	}
	_, err := tx.Exec(ctx, `DELETE FROM files WHERE id = $1`, f.ID)
	return err
}

func (p *Projector) applyFileShared(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var data struct {
		FileID    string `json:"file_id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(entry.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal file shared: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO file_channels (file_id, channel_id, shared_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (file_id, channel_id) DO NOTHING`,
		data.FileID, data.ChannelID, entry.CreatedAt)
	return err
}

// --- Token projections ---

func (p *Projector) applyTokenCreated(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var t domain.Token
	if err := json.Unmarshal(entry.EventData, &t); err != nil {
		return fmt.Errorf("unmarshal token: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO tokens (id, team_id, user_id, token, scopes, is_bot, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING`,
		t.ID, t.TeamID, t.UserID, t.Token, t.Scopes, t.IsBot, t.ExpiresAt, t.CreatedAt)
	return err
}

func (p *Projector) applyTokenRevoked(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var t domain.Token
	if err := json.Unmarshal(entry.EventData, &t); err != nil {
		return fmt.Errorf("unmarshal revoked token: %w", err)
	}
	_, err := tx.Exec(ctx, `UPDATE tokens SET expires_at = NOW() WHERE token = $1`, t.Token)
	return err
}

// --- Subscription projections ---

func (p *Projector) applySubscriptionUpsert(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var s domain.EventSubscription
	if err := json.Unmarshal(entry.EventData, &s); err != nil {
		return fmt.Errorf("unmarshal subscription: %w", err)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO event_subscriptions (id, team_id, url, event_types, secret, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			team_id = EXCLUDED.team_id, url = EXCLUDED.url, event_types = EXCLUDED.event_types,
			secret = EXCLUDED.secret, enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at`,
		s.ID, s.TeamID, s.URL, s.EventTypes, s.Secret, s.Enabled, s.CreatedAt, s.UpdatedAt)
	return err
}

func (p *Projector) applySubscriptionDeleted(ctx context.Context, tx pgx.Tx, entry domain.EventLogEntry) error {
	var s domain.EventSubscription
	if err := json.Unmarshal(entry.EventData, &s); err != nil {
		return fmt.Errorf("unmarshal deleted subscription: %w", err)
	}
	_, err := tx.Exec(ctx, `DELETE FROM event_subscriptions WHERE id = $1`, s.ID)
	return err
}
