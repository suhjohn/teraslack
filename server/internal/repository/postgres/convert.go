package postgres

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

func tsToTime(ts any) time.Time {
	switch v := ts.(type) {
	case pgtype.Timestamptz:
		if v.Valid {
			return v.Time
		}
		return time.Time{}
	case time.Time:
		return v
	case *time.Time:
		if v != nil {
			return *v
		}
		return time.Time{}
	default:
		return time.Time{}
	}
}

func tsToTimePtr(ts any) *time.Time {
	switch v := ts.(type) {
	case pgtype.Timestamptz:
		if v.Valid {
			return &v.Time
		}
		return nil
	case time.Time:
		return &v
	case *time.Time:
		return v
	default:
		return nil
	}
}

func textToStringPtr(t pgtype.Text) *string {
	if t.Valid {
		return &t.String
	}
	return nil
}

func textToString(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

func boolToBoolPtr(b any) *bool {
	switch v := b.(type) {
	case bool:
		return &v
	case pgtype.Bool:
		if v.Valid {
			return &v.Bool
		}
		return nil
	default:
		return nil
	}
}

func stringPtrToText(s *string) pgtype.Text {
	if s != nil {
		return pgtype.Text{String: *s, Valid: true}
	}
	return pgtype.Text{}
}

func stringToText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func timeToPgTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// userFields is a common struct for user row conversion.
type userFields struct {
	ID, AccountID, WorkspaceID, Name, RealName, DisplayName, Email string
	PrincipalType, OwnerID, AccountType                            string
	IsBot, Deleted                                                 bool
	Profile                                                        []byte
	CreatedAt, UpdatedAt                                           time.Time
}

func userFieldsToDomain(u userFields) (*domain.User, error) {
	var profile domain.UserProfile
	if len(u.Profile) > 0 {
		if err := json.Unmarshal(u.Profile, &profile); err != nil {
			return nil, err
		}
	}
	return &domain.User{
		ID:            u.ID,
		AccountID:     u.AccountID,
		WorkspaceID:   u.WorkspaceID,
		Name:          u.Name,
		RealName:      u.RealName,
		DisplayName:   u.DisplayName,
		Email:         u.Email,
		PrincipalType: domain.PrincipalType(u.PrincipalType),
		OwnerID:       u.OwnerID,
		AccountType:   domain.AccountType(u.AccountType),
		IsBot:         u.IsBot,
		Deleted:       u.Deleted,
		Profile:       profile,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}, nil
}

func createUserRowToFields(r sqlcgen.CreateUserRow) userFields {
	return userFields{
		ID: r.ID, AccountID: textToString(r.AccountID), WorkspaceID: r.WorkspaceID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func getUserRowToFields(r sqlcgen.GetUserRow) userFields {
	return userFields{
		ID: r.ID, AccountID: textToString(r.AccountID), WorkspaceID: r.WorkspaceID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func getUserByWorkspaceAndAccountRowToFields(r sqlcgen.GetUserByWorkspaceAndAccountRow) userFields {
	return userFields{
		ID: r.ID, AccountID: textToString(r.AccountID), WorkspaceID: r.WorkspaceID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func updateUserRowToFields(r sqlcgen.UpdateUserRow) userFields {
	return userFields{
		ID: r.ID, AccountID: textToString(r.AccountID), WorkspaceID: r.WorkspaceID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func listUserRowToFields(r sqlcgen.ListUsersRow) userFields {
	return userFields{
		ID: r.ID, AccountID: textToString(r.AccountID), WorkspaceID: r.WorkspaceID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func listUsersByAccountRowToFields(r sqlcgen.ListUsersByAccountRow) userFields {
	return userFields{
		ID: r.ID, AccountID: textToString(r.AccountID), WorkspaceID: r.WorkspaceID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func apiKeyToDomain(row any) *domain.APIKey {
	switch k := row.(type) {
	case sqlcgen.ApiKey:
		return apiKeyFieldsToDomain(k.ID, k.Name, k.Description, k.KeyHash, k.KeyPrefix, k.KeyHint, k.Scope, k.WorkspaceID.String, k.OwnerAccountID.String, k.WorkspaceIds, k.CreatedBy, k.Permissions, k.ExpiresAt, k.LastUsedAt, k.RequestCount, k.Revoked, k.RevokedAt, k.RotatedToID, k.GracePeriodEndsAt, k.CreatedAt, k.UpdatedAt)
	case sqlcgen.CreateAPIKeyRow:
		return apiKeyFieldsToDomain(k.ID, k.Name, k.Description, k.KeyHash, k.KeyPrefix, k.KeyHint, k.Scope, k.WorkspaceID, k.OwnerAccountID, k.WorkspaceIds, k.CreatedBy, k.Permissions, k.ExpiresAt, k.LastUsedAt, k.RequestCount, k.Revoked, k.RevokedAt, k.RotatedToID, k.GracePeriodEndsAt, k.CreatedAt, k.UpdatedAt)
	case sqlcgen.GetAPIKeyRow:
		return apiKeyFieldsToDomain(k.ID, k.Name, k.Description, k.KeyHash, k.KeyPrefix, k.KeyHint, k.Scope, k.WorkspaceID, k.OwnerAccountID, k.WorkspaceIds, k.CreatedBy, k.Permissions, k.ExpiresAt, k.LastUsedAt, k.RequestCount, k.Revoked, k.RevokedAt, k.RotatedToID, k.GracePeriodEndsAt, k.CreatedAt, k.UpdatedAt)
	case sqlcgen.GetAPIKeyByHashRow:
		return apiKeyFieldsToDomain(k.ID, k.Name, k.Description, k.KeyHash, k.KeyPrefix, k.KeyHint, k.Scope, k.WorkspaceID, k.OwnerAccountID, k.WorkspaceIds, k.CreatedBy, k.Permissions, k.ExpiresAt, k.LastUsedAt, k.RequestCount, k.Revoked, k.RevokedAt, k.RotatedToID, k.GracePeriodEndsAt, k.CreatedAt, k.UpdatedAt)
	case sqlcgen.ListAPIKeysRow:
		return apiKeyFieldsToDomain(k.ID, k.Name, k.Description, k.KeyHash, k.KeyPrefix, k.KeyHint, k.Scope, k.WorkspaceID, k.OwnerAccountID, k.WorkspaceIds, k.CreatedBy, k.Permissions, k.ExpiresAt, k.LastUsedAt, k.RequestCount, k.Revoked, k.RevokedAt, k.RotatedToID, k.GracePeriodEndsAt, k.CreatedAt, k.UpdatedAt)
	case sqlcgen.ListAPIKeysIncludeRevokedRow:
		return apiKeyFieldsToDomain(k.ID, k.Name, k.Description, k.KeyHash, k.KeyPrefix, k.KeyHint, k.Scope, k.WorkspaceID, k.OwnerAccountID, k.WorkspaceIds, k.CreatedBy, k.Permissions, k.ExpiresAt, k.LastUsedAt, k.RequestCount, k.Revoked, k.RevokedAt, k.RotatedToID, k.GracePeriodEndsAt, k.CreatedAt, k.UpdatedAt)
	case sqlcgen.UpdateAPIKeyRow:
		return apiKeyFieldsToDomain(k.ID, k.Name, k.Description, k.KeyHash, k.KeyPrefix, k.KeyHint, k.Scope, k.WorkspaceID, k.OwnerAccountID, k.WorkspaceIds, k.CreatedBy, k.Permissions, k.ExpiresAt, k.LastUsedAt, k.RequestCount, k.Revoked, k.RevokedAt, k.RotatedToID, k.GracePeriodEndsAt, k.CreatedAt, k.UpdatedAt)
	default:
		panic("unsupported api key row type")
	}
}

func apiKeyFieldsToDomain(
	id, name, description, keyHash, keyPrefix, keyHint, scope, workspaceID, accountID string,
	workspaceIDs []string,
	createdBy string,
	permissions []string,
	expiresAt, lastUsedAt any,
	requestCount int64,
	revoked bool,
	revokedAt any,
	rotatedToID string,
	gracePeriodEndsAt, createdAt, updatedAt any,
) *domain.APIKey {
	return &domain.APIKey{
		ID:                id,
		Name:              name,
		Description:       description,
		KeyHash:           keyHash,
		KeyPrefix:         keyPrefix,
		KeyHint:           keyHint,
		Scope:             domain.APIKeyScope(scope),
		WorkspaceID:       workspaceID,
		AccountID:         accountID,
		WorkspaceIDs:      workspaceIDs,
		CreatedBy:         createdBy,
		Permissions:       permissions,
		ExpiresAt:         tsToTimePtr(expiresAt),
		LastUsedAt:        tsToTimePtr(lastUsedAt),
		RequestCount:      requestCount,
		Revoked:           revoked,
		RevokedAt:         tsToTimePtr(revokedAt),
		RotatedToID:       rotatedToID,
		GracePeriodEndsAt: tsToTimePtr(gracePeriodEndsAt),
		CreatedAt:         tsToTime(createdAt),
		UpdatedAt:         tsToTime(updatedAt),
	}
}

func convToDomain(row any) *domain.Conversation {
	switch c := row.(type) {
	case sqlcgen.Conversation:
		return convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
	case sqlcgen.CreateConversationRow:
		return convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
	case sqlcgen.GetConversationRow:
		return convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
	case sqlcgen.UpdateConversationRow:
		return convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
	case sqlcgen.SetConversationTopicRow:
		return convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
	case sqlcgen.SetConversationPurposeRow:
		return convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
	case sqlcgen.ListVisibleConversationsRow:
		conv := convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
		conv.LastReadTS = textToStringPtr(c.LastReadTs)
		conv.HasUnread = boolToBoolPtr(c.HasUnread)
		return conv
	case sqlcgen.GetCanonicalDMConversationRow:
		return convFieldsToDomain(c.ID, c.WorkspaceID, c.Name, c.Type, c.CreatorID, c.IsArchived, c.TopicValue, c.TopicCreator, c.TopicLastSet, c.PurposeValue, c.PurposeCreator, c.PurposeLastSet, c.NumMembers, c.LastMessageTs, c.LastActivityTs, c.CreatedAt, c.UpdatedAt)
	default:
		panic("unsupported conversation row type")
	}
}

func convFieldsToDomain(
	id, workspaceID, name, convType, creatorID string,
	isArchived bool,
	topicValue, topicCreator string,
	topicLastSet *time.Time,
	purposeValue, purposeCreator string,
	purposeLastSet *time.Time,
	numMembers int32,
	lastMessageTs, lastActivityTs pgtype.Text,
	createdAt, updatedAt time.Time,
) *domain.Conversation {
	return &domain.Conversation{
		ID:          id,
		WorkspaceID: workspaceID,
		Name:        name,
		Type:        domain.ConversationType(convType),
		CreatorID:   creatorID,
		IsArchived:  isArchived,
		Topic: domain.TopicPurpose{
			Value:   topicValue,
			Creator: topicCreator,
			LastSet: tsToTimePtr(topicLastSet),
		},
		Purpose: domain.TopicPurpose{
			Value:   purposeValue,
			Creator: purposeCreator,
			LastSet: tsToTimePtr(purposeLastSet),
		},
		NumMembers:     int(numMembers),
		LastMessageTS:  textToStringPtr(lastMessageTs),
		LastActivityTS: textToStringPtr(lastActivityTs),
		CreatedAt:      tsToTime(createdAt),
		UpdatedAt:      tsToTime(updatedAt),
	}
}

func msgToDomain(m sqlcgen.Message) *domain.Message {
	return &domain.Message{
		TS:              m.Ts,
		ChannelID:       m.ChannelID,
		UserID:          m.UserID,
		Text:            m.Text,
		ThreadTS:        textToStringPtr(m.ThreadTs),
		Type:            m.Type,
		Subtype:         textToStringPtr(m.Subtype),
		Blocks:          json.RawMessage(m.Blocks),
		Metadata:        json.RawMessage(m.Metadata),
		EditedBy:        textToStringPtr(m.EditedBy),
		EditedAt:        textToStringPtr(m.EditedAt),
		ReplyCount:      int(m.ReplyCount),
		ReplyUsersCount: int(m.ReplyUsersCount),
		LatestReply:     textToStringPtr(m.LatestReply),
		IsDeleted:       m.IsDeleted,
		CreatedAt:       tsToTime(m.CreatedAt),
		UpdatedAt:       tsToTime(m.UpdatedAt),
	}
}

func fileToDomain(f sqlcgen.GetFileRow) *domain.File {
	return &domain.File{
		ID:                 f.ID,
		WorkspaceID:        f.WorkspaceID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		URLPrivate:         f.UrlPrivate,
		URLPrivateDownload: f.UrlPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalURL:        f.ExternalUrl,
		CreatedAt:          tsToTime(f.CreatedAt),
		UpdatedAt:          tsToTime(f.UpdatedAt),
	}
}

func fileByIDToDomain(f sqlcgen.GetFileByIDRow) *domain.File {
	return &domain.File{
		ID:                 f.ID,
		WorkspaceID:        f.WorkspaceID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		URLPrivate:         f.UrlPrivate,
		URLPrivateDownload: f.UrlPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalURL:        f.ExternalUrl,
		CreatedAt:          tsToTime(f.CreatedAt),
		UpdatedAt:          tsToTime(f.UpdatedAt),
	}
}

func fileListToDomain(f sqlcgen.ListFilesRow) *domain.File {
	return &domain.File{
		ID:                 f.ID,
		WorkspaceID:        f.WorkspaceID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		URLPrivate:         f.UrlPrivate,
		URLPrivateDownload: f.UrlPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalURL:        f.ExternalUrl,
		CreatedAt:          tsToTime(f.CreatedAt),
		UpdatedAt:          tsToTime(f.UpdatedAt),
	}
}

func fileByUserToDomain(f sqlcgen.ListFilesByUserRow) *domain.File {
	return &domain.File{
		ID:                 f.ID,
		WorkspaceID:        f.WorkspaceID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		URLPrivate:         f.UrlPrivate,
		URLPrivateDownload: f.UrlPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalURL:        f.ExternalUrl,
		CreatedAt:          tsToTime(f.CreatedAt),
		UpdatedAt:          tsToTime(f.UpdatedAt),
	}
}

func fileByChannelToDomain(f sqlcgen.ListFilesByChannelRow) *domain.File {
	return &domain.File{
		ID:                 f.ID,
		WorkspaceID:        f.WorkspaceID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		URLPrivate:         f.UrlPrivate,
		URLPrivateDownload: f.UrlPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalURL:        f.ExternalUrl,
		CreatedAt:          tsToTime(f.CreatedAt),
		UpdatedAt:          tsToTime(f.UpdatedAt),
	}
}

func fileByChannelAndUserToDomain(f sqlcgen.ListFilesByChannelAndUserRow) *domain.File {
	return &domain.File{
		ID:                 f.ID,
		WorkspaceID:        f.WorkspaceID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		URLPrivate:         f.UrlPrivate,
		URLPrivateDownload: f.UrlPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalURL:        f.ExternalUrl,
		CreatedAt:          tsToTime(f.CreatedAt),
		UpdatedAt:          tsToTime(f.UpdatedAt),
	}
}

func createEventSubRowToDomain(e sqlcgen.CreateEventSubscriptionRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, WorkspaceID: e.WorkspaceID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func getEventSubRowToDomain(e sqlcgen.GetEventSubscriptionRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, WorkspaceID: e.WorkspaceID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func updateEventSubRowToDomain(e sqlcgen.UpdateEventSubscriptionRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, WorkspaceID: e.WorkspaceID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func listEventSubRowToDomain(e sqlcgen.ListEventSubscriptionsRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, WorkspaceID: e.WorkspaceID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func listEventSubByTeamEventRowToDomain(e sqlcgen.ListEventSubscriptionsByWorkspaceAndEventRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, WorkspaceID: e.WorkspaceID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func authSessionToDomain(row any) *domain.AuthSession {
	switch s := row.(type) {
	case sqlcgen.CreateAuthSessionRow:
		return &domain.AuthSession{
			ID:          s.ID,
			WorkspaceID: s.WorkspaceID,
			AccountID:   textToString(s.AccountID),
			UserID:      textToString(s.UserID),
			Provider:    domain.AuthProvider(s.Provider),
			ExpiresAt:   tsToTime(s.ExpiresAt),
			RevokedAt:   tsToTimePtr(s.RevokedAt),
			CreatedAt:   tsToTime(s.CreatedAt),
		}
	case sqlcgen.GetAuthSessionByHashRow:
		return &domain.AuthSession{
			ID:          s.ID,
			WorkspaceID: s.WorkspaceID,
			AccountID:   textToString(s.AccountID),
			UserID:      textToString(s.UserID),
			Provider:    domain.AuthProvider(s.Provider),
			ExpiresAt:   tsToTime(s.ExpiresAt),
			RevokedAt:   tsToTimePtr(s.RevokedAt),
			CreatedAt:   tsToTime(s.CreatedAt),
		}
	default:
		return &domain.AuthSession{}
	}
}

func oauthAccountToDomain(row any) *domain.OAuthAccount {
	switch a := row.(type) {
	case sqlcgen.GetOAuthAccountRow:
		return &domain.OAuthAccount{
			ID:              a.ID,
			WorkspaceID:     a.WorkspaceID,
			AccountID:       textToString(a.AccountID),
			UserID:          textToString(a.UserID),
			Provider:        domain.AuthProvider(a.Provider),
			ProviderSubject: a.ProviderSubject,
			Email:           a.Email,
			CreatedAt:       tsToTime(a.CreatedAt),
			UpdatedAt:       tsToTime(a.UpdatedAt),
		}
	case sqlcgen.ListOAuthAccountsBySubjectRow:
		return &domain.OAuthAccount{
			ID:              a.ID,
			WorkspaceID:     a.WorkspaceID,
			AccountID:       textToString(a.AccountID),
			UserID:          textToString(a.UserID),
			Provider:        domain.AuthProvider(a.Provider),
			ProviderSubject: a.ProviderSubject,
			Email:           a.Email,
			CreatedAt:       tsToTime(a.CreatedAt),
			UpdatedAt:       tsToTime(a.UpdatedAt),
		}
	case sqlcgen.UpsertOAuthAccountRow:
		return &domain.OAuthAccount{
			ID:              a.ID,
			WorkspaceID:     a.WorkspaceID,
			AccountID:       textToString(a.AccountID),
			UserID:          textToString(a.UserID),
			Provider:        domain.AuthProvider(a.Provider),
			ProviderSubject: a.ProviderSubject,
			Email:           a.Email,
			CreatedAt:       tsToTime(a.CreatedAt),
			UpdatedAt:       tsToTime(a.UpdatedAt),
		}
	default:
		return &domain.OAuthAccount{}
	}
}
