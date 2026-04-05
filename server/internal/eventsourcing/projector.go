package eventsourcing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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

func timePtrToPgTimestamptz(t *time.Time) pgtype.Timestamptz {
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

func projStringToText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

type projectorPrincipalIDs struct {
	UserID    string
	AccountID string
}

func normalizeProjectorConversationOwner(conv *domain.Conversation, workspaceID string) {
	if conv == nil {
		return
	}
	if conv.WorkspaceID == "" {
		conv.WorkspaceID = workspaceID
	}
	switch conv.OwnerType {
	case domain.ConversationOwnerTypeAccount:
		if conv.OwnerAccountID == "" {
			conv.OwnerType = domain.ConversationOwnerTypeWorkspace
			if conv.OwnerWorkspaceID == "" {
				conv.OwnerWorkspaceID = conv.WorkspaceID
			}
			return
		}
		conv.OwnerWorkspaceID = ""
	case domain.ConversationOwnerTypeWorkspace:
		if conv.OwnerWorkspaceID == "" {
			conv.OwnerWorkspaceID = conv.WorkspaceID
		}
		conv.OwnerAccountID = ""
	default:
		if conv.OwnerAccountID != "" {
			conv.OwnerType = domain.ConversationOwnerTypeAccount
			conv.OwnerWorkspaceID = ""
		} else {
			conv.OwnerType = domain.ConversationOwnerTypeWorkspace
			if conv.OwnerWorkspaceID == "" {
				conv.OwnerWorkspaceID = conv.WorkspaceID
			}
			conv.OwnerAccountID = ""
		}
	}
}

func resolveProjectorPrincipalIDs(ctx context.Context, q *sqlcgen.Queries, workspaceID, userID, accountID string) projectorPrincipalIDs {
	userID = strings.TrimSpace(userID)
	accountID = strings.TrimSpace(accountID)

	if accountID == "" && userID != "" {
		if user, err := q.GetUser(ctx, userID); err == nil && user.AccountID.Valid {
			accountID = user.AccountID.String
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			_ = err
		}
	}
	if userID == "" && accountID != "" && workspaceID != "" {
		if user, err := q.GetUserByWorkspaceAndAccount(ctx, sqlcgen.GetUserByWorkspaceAndAccountParams{
			WorkspaceID: workspaceID,
			AccountID:   projStringToText(accountID),
		}); err == nil {
			userID = user.ID
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			_ = err
		}
	}
	return projectorPrincipalIDs{UserID: userID, AccountID: accountID}
}

func resolveProjectorConversationWorkspaceID(ctx context.Context, q *sqlcgen.Queries, conversationID string) string {
	if conversationID == "" {
		return ""
	}
	conv, err := q.GetConversation(ctx, conversationID)
	if err != nil {
		return ""
	}
	return conv.WorkspaceID.String
}

func resolveProjectorWorkspaceMembershipID(ctx context.Context, q *sqlcgen.Queries, workspaceID, accountID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	accountID = strings.TrimSpace(accountID)
	if workspaceID == "" || accountID == "" {
		return ""
	}
	workspaceMembershipID, err := q.ProjectorGetWorkspaceMembershipByWorkspaceAndAccount(ctx, sqlcgen.ProjectorGetWorkspaceMembershipByWorkspaceAndAccountParams{
		WorkspaceID: workspaceID,
		AccountID:   accountID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			_ = err
		}
		return ""
	}
	return workspaceMembershipID
}

func normalizeProjectorAllowedAccountIDs(ctx context.Context, q *sqlcgen.Queries, policy domain.ConversationPostingPolicy) []string {
	seen := make(map[string]struct{}, len(policy.AllowedAccountIDs))
	accountIDs := make([]string, 0, len(policy.AllowedAccountIDs))

	addAccountID := func(accountID string) {
		accountID = strings.TrimSpace(accountID)
		if accountID == "" {
			return
		}
		if _, ok := seen[accountID]; ok {
			return
		}
		seen[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}

	for _, accountID := range policy.AllowedAccountIDs {
		addAccountID(accountID)
	}
	sort.Strings(accountIDs)
	return accountIDs
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// RebuildAll replays all events and rebuilds every projection table from scratch.
func (p *Projector) RebuildAll(ctx context.Context) error {
	aggregates := []string{
		domain.AggregateUser,
		domain.AggregateConversation,
		domain.AggregateMessage,
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

	// Projection resets are routed through explicit sqlc exec queries so the
	// projector no longer carries ad hoc SQL.
	if err := p.truncateProjection(ctx, q.WithTx(tx), aggregateType); err != nil {
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

// sqlcEventToDomain converts generated internal event rows to domain.InternalEvent.
func sqlcEventToDomain(row any) domain.InternalEvent {
	switch e := row.(type) {
	case sqlcgen.InternalEvent:
		return sqlcEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, e.ShardKey, e.ShardID, e.Payload, e.Metadata, e.CreatedAt)
	case sqlcgen.ProjectorGetInternalEventsByAggregateTypeRow:
		return sqlcEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, "", 0, e.Payload, e.Metadata, e.CreatedAt)
	case sqlcgen.ProjectorGetInternalEventsSinceRow:
		return sqlcEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, "", 0, e.Payload, e.Metadata, e.CreatedAt)
	default:
		panic("unsupported projector internal event row type")
	}
}

func sqlcEventFieldsToDomain(
	id int64,
	eventType, aggregateType, aggregateID, workspaceID, actorID, shardKey string,
	shardID int32,
	payload, metadata json.RawMessage,
	createdAt time.Time,
) domain.InternalEvent {
	return domain.InternalEvent{
		ID:            id,
		EventType:     eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		WorkspaceID:   workspaceID,
		ActorID:       actorID,
		ShardKey:      shardKey,
		ShardID:       int(shardID),
		Payload:       payload,
		Metadata:      metadata,
		CreatedAt:     createdAt,
	}
}

func (p *Projector) truncateProjection(ctx context.Context, q *sqlcgen.Queries, aggregateType string) error {
	switch aggregateType {
	case domain.AggregateUser:
		return q.ProjectorTruncateUserProjection(ctx)
	case domain.AggregateConversation:
		return q.ProjectorTruncateConversationProjection(ctx)
	case domain.AggregateMessage:
		return q.ProjectorTruncateMessageProjection(ctx)
	case domain.AggregateFile:
		return q.ProjectorTruncateFileProjection(ctx)
	case domain.AggregateSubscription:
		return q.ProjectorTruncateSubscriptionProjection(ctx)
	case domain.AggregateAPIKey:
		return q.ProjectorTruncateAPIKeyProjection(ctx)
	default:
		return fmt.Errorf("unknown aggregate type: %s", aggregateType)
	}
}

func (p *Projector) applyEvent(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	switch entry.EventType {
	// User events
	case domain.EventUserCreated, domain.EventUserUpdated:
		return p.applyUserUpsert(ctx, q, entry)
	case domain.EventUserDeleted:
		return p.applyUserDeleted(ctx, q, entry)
	case domain.EventUserRolesUpdated:
		return p.applyUserRolesUpdated(ctx, q, entry)

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
		AccountID:     projStringToText(u.AccountID),
		WorkspaceID:   u.WorkspaceID,
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

func (p *Projector) applyUserRolesUpdated(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
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
	if err := q.ProjectorDeleteUserRoleAssignments(ctx, sqlcgen.ProjectorDeleteUserRoleAssignmentsParams{
		WorkspaceID: entry.WorkspaceID,
		UserID:      payload.UserID,
	}); err != nil {
		return fmt.Errorf("delete user roles: %w", err)
	}
	for _, role := range payload.DelegatedRoles {
		if err := q.ProjectorInsertUserRoleAssignment(ctx, sqlcgen.ProjectorInsertUserRoleAssignmentParams{
			ID:          generateProjectionID("URA", entry.ID, string(role)),
			WorkspaceID: entry.WorkspaceID,
			UserID:      payload.UserID,
			RoleKey:     string(role),
			AssignedBy:  entry.ActorID,
			CreatedAt:   timeToPgTimestamptz(entry.CreatedAt),
		}); err != nil {
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
	normalizeProjectorConversationOwner(&c, entry.WorkspaceID)

	return q.ProjectorUpsertConversation(ctx, sqlcgen.ProjectorUpsertConversationParams{
		ID:               c.ID,
		WorkspaceID:      projStringToText(c.WorkspaceID),
		OwnerType:        string(c.OwnerType),
		OwnerAccountID:   projStringToText(c.OwnerAccountID),
		OwnerWorkspaceID: projStringToText(c.OwnerWorkspaceID),
		Name:             c.Name,
		Type:             string(c.Type),
		CreatorID:        projStringToText(c.CreatorID),
		IsArchived:       c.IsArchived,
		TopicValue:       c.Topic.Value,
		TopicCreator:     c.Topic.Creator,
		TopicLastSet:     timePtrToTs(c.Topic.LastSet),
		PurposeValue:     c.Purpose.Value,
		PurposeCreator:   c.Purpose.Creator,
		PurposeLastSet:   timePtrToTs(c.Purpose.LastSet),
		NumMembers:       int32(c.NumMembers),
		CreatedAt:        timeToTs(c.CreatedAt),
		UpdatedAt:        timeToTs(c.UpdatedAt),
	})
}

func (p *Projector) applyMemberJoined(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		AccountID    string               `json:"account_id"`
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

	convWorkspaceID := entry.WorkspaceID
	if data.Conversation != nil && data.Conversation.WorkspaceID != "" {
		convWorkspaceID = data.Conversation.WorkspaceID
	}
	principal := resolveProjectorPrincipalIDs(ctx, q, convWorkspaceID, data.UserID, data.AccountID)
	actorPrincipal := resolveProjectorPrincipalIDs(ctx, q, convWorkspaceID, entry.ActorID, "")
	addedByAccountID := actorPrincipal.AccountID
	if addedByAccountID == "" {
		addedByAccountID = principal.AccountID
	}
	if principal.UserID != "" {
		if err := q.ProjectorUpsertMember(ctx, sqlcgen.ProjectorUpsertMemberParams{
			ConversationID: entry.AggregateID,
			UserID:         principal.UserID,
			JoinedAt:       timeToTs(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	if principal.AccountID != "" {
		if err := q.ProjectorUpsertMemberV2(ctx, sqlcgen.ProjectorUpsertMemberV2Params{
			ConversationID:   entry.AggregateID,
			AccountID:        principal.AccountID,
			MembershipRole:   "member",
			AddedByAccountID: projStringToText(addedByAccountID),
			CreatedAt:        timeToTs(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Projector) applyMemberLeft(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		AccountID    string               `json:"account_id"`
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

	convWorkspaceID := entry.WorkspaceID
	if data.Conversation != nil && data.Conversation.WorkspaceID != "" {
		convWorkspaceID = data.Conversation.WorkspaceID
	}
	principal := resolveProjectorPrincipalIDs(ctx, q, convWorkspaceID, data.UserID, data.AccountID)
	if principal.UserID != "" {
		if err := q.ProjectorDeleteMember(ctx, sqlcgen.ProjectorDeleteMemberParams{
			ConversationID: entry.AggregateID,
			UserID:         principal.UserID,
		}); err != nil {
			return err
		}
	}
	if principal.AccountID != "" {
		if err := q.ProjectorDeleteMemberV2(ctx, sqlcgen.ProjectorDeleteMemberV2Params{
			ConversationID: entry.AggregateID,
			AccountID:      principal.AccountID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Projector) applyConversationManagerAdded(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		ConversationID string `json:"conversation_id"`
		AccountID      string `json:"account_id"`
		UserID         string `json:"user_id"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal conversation manager added: %w", err)
	}
	workspaceID := resolveProjectorConversationWorkspaceID(ctx, q, firstNonEmptyString(data.ConversationID, entry.AggregateID))
	principal := resolveProjectorPrincipalIDs(ctx, q, workspaceID, data.UserID, data.AccountID)
	actorPrincipal := resolveProjectorPrincipalIDs(ctx, q, workspaceID, entry.ActorID, "")
	assignedByAccountID := actorPrincipal.AccountID
	if assignedByAccountID == "" {
		assignedByAccountID = principal.AccountID
	}
	if principal.UserID != "" {
		if err := q.ProjectorUpsertConversationManager(ctx, sqlcgen.ProjectorUpsertConversationManagerParams{
			ConversationID: data.ConversationID,
			UserID:         principal.UserID,
			AssignedBy:     entry.ActorID,
			CreatedAt:      timeToPgTimestamptz(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	if principal.AccountID != "" {
		if err := q.ProjectorUpsertConversationManagerV2(ctx, sqlcgen.ProjectorUpsertConversationManagerV2Params{
			ConversationID:      data.ConversationID,
			AccountID:           principal.AccountID,
			AssignedByAccountID: projStringToText(assignedByAccountID),
			CreatedAt:           timeToTs(entry.CreatedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Projector) applyConversationManagerRemoved(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var data struct {
		ConversationID string `json:"conversation_id"`
		AccountID      string `json:"account_id"`
		UserID         string `json:"user_id"`
	}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		return fmt.Errorf("unmarshal conversation manager removed: %w", err)
	}
	workspaceID := resolveProjectorConversationWorkspaceID(ctx, q, firstNonEmptyString(data.ConversationID, entry.AggregateID))
	principal := resolveProjectorPrincipalIDs(ctx, q, workspaceID, data.UserID, data.AccountID)
	if principal.UserID != "" {
		if err := q.ProjectorDeleteConversationManager(ctx, sqlcgen.ProjectorDeleteConversationManagerParams{
			ConversationID: data.ConversationID,
			UserID:         principal.UserID,
		}); err != nil {
			return err
		}
	}
	if principal.AccountID != "" {
		if err := q.ProjectorDeleteConversationManagerV2(ctx, sqlcgen.ProjectorDeleteConversationManagerV2Params{
			ConversationID: data.ConversationID,
			AccountID:      principal.AccountID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Projector) applyConversationPostingPolicyUpdated(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var policy domain.ConversationPostingPolicy
	if err := json.Unmarshal(entry.Payload, &policy); err != nil {
		return fmt.Errorf("unmarshal conversation posting policy: %w", err)
	}
	if policy.PolicyType == "" {
		policy.PolicyType = domain.ConversationPostingPolicyEveryone
	}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal conversation posting policy: %w", err)
	}
	if err := q.ProjectorUpsertConversationPostingPolicy(ctx, sqlcgen.ProjectorUpsertConversationPostingPolicyParams{
		ConversationID: policy.ConversationID,
		PolicyType:     string(policy.PolicyType),
		PolicyJson:     policyJSON,
		UpdatedBy:      policy.UpdatedBy,
		UpdatedAt:      timeToPgTimestamptz(policy.UpdatedAt),
	}); err != nil {
		return err
	}
	accountIDs := normalizeProjectorAllowedAccountIDs(ctx, q, policy)
	return q.ProjectorReplaceConversationPostingPolicyAllowedAccounts(ctx, sqlcgen.ProjectorReplaceConversationPostingPolicyAllowedAccountsParams{
		ConversationID: policy.ConversationID,
		AccountIds:     accountIDs,
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
	convWorkspaceID := resolveProjectorConversationWorkspaceID(ctx, q, m.ChannelID)
	principal := resolveProjectorPrincipalIDs(ctx, q, convWorkspaceID, m.UserID, m.AuthorAccountID)
	authorWorkspaceMembershipID := strings.TrimSpace(m.AuthorWorkspaceMembershipID)
	if authorWorkspaceMembershipID == "" {
		authorWorkspaceMembershipID = resolveProjectorWorkspaceMembershipID(ctx, q, convWorkspaceID, principal.AccountID)
	}

	return q.ProjectorUpsertMessage(ctx, sqlcgen.ProjectorUpsertMessageParams{
		Ts:                          m.TS,
		ChannelID:                   m.ChannelID,
		UserID:                      principal.UserID,
		AuthorAccountID:             projStringToText(principal.AccountID),
		AuthorWorkspaceMembershipID: projStringToText(authorWorkspaceMembershipID),
		Text:                        m.Text,
		ThreadTs:                    projStringPtrToText(m.ThreadTS),
		Type:                        m.Type,
		Subtype:                     projStringPtrToText(m.Subtype),
		Blocks:                      blocksJSON,
		Metadata:                    metadataJSON,
		EditedBy:                    projStringPtrToText(m.EditedBy),
		EditedAt:                    projStringPtrToText(m.EditedAt),
		ReplyCount:                  int32(m.ReplyCount),
		ReplyUsersCount:             int32(m.ReplyUsersCount),
		LatestReply:                 projStringPtrToText(m.LatestReply),
		IsDeleted:                   m.IsDeleted,
		CreatedAt:                   timeToTs(m.CreatedAt),
		UpdatedAt:                   timeToTs(m.UpdatedAt),
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

// --- File projections ---

func (p *Projector) applyFileUpsert(ctx context.Context, q *sqlcgen.Queries, entry domain.InternalEvent) error {
	var f domain.File
	if err := json.Unmarshal(entry.Payload, &f); err != nil {
		return fmt.Errorf("unmarshal file: %w", err)
	}
	if f.WorkspaceID == "" {
		if entry.WorkspaceID != "" {
			f.WorkspaceID = entry.WorkspaceID
		} else if user, err := q.GetUser(ctx, f.UserID); err == nil {
			f.WorkspaceID = user.WorkspaceID
		}
	}
	if f.WorkspaceID == "" {
		return fmt.Errorf("file workspace_id missing for file %s", f.ID)
	}

	return q.ProjectorUpsertFile(ctx, sqlcgen.ProjectorUpsertFileParams{
		ID:                 f.ID,
		WorkspaceID:        f.WorkspaceID,
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
		WorkspaceID:     s.WorkspaceID,
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
		Scope:             string(k.Scope),
		Column8:           nilIfEmpty(k.WorkspaceID),
		Column9:           nilIfEmpty(k.AccountID),
		WorkspaceIds:      k.WorkspaceIDs,
		CreatedBy:         k.CreatedBy,
		OnBehalfOf:        "",
		Type:              "persistent",
		Environment:       "live",
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

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
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
