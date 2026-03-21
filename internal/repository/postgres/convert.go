package postgres

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

func tsToTime(ts pgtype.Timestamptz) time.Time {
	if ts.Valid {
		return ts.Time
	}
	return time.Time{}
}

func tsToTimePtr(ts pgtype.Timestamptz) *time.Time {
	if ts.Valid {
		return &ts.Time
	}
	return nil
}

func textToStringPtr(t pgtype.Text) *string {
	if t.Valid {
		return &t.String
	}
	return nil
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

// userFields is a common struct for user row conversion.
type userFields struct {
	ID, TeamID, Name, RealName, DisplayName, Email string
	PrincipalType, OwnerID                         string
	IsBot, IsAdmin, IsOwner, IsRestricted, Deleted  bool
	Profile                                         []byte
	CreatedAt, UpdatedAt                            pgtype.Timestamptz
}

func userFieldsToDomain(u userFields) (*domain.User, error) {
	var profile domain.UserProfile
	if len(u.Profile) > 0 {
		if err := json.Unmarshal(u.Profile, &profile); err != nil {
			return nil, err
		}
	}
	pt := domain.PrincipalType(u.PrincipalType)
	if pt == "" {
		pt = domain.PrincipalTypeHuman
	}
	return &domain.User{
		ID:            u.ID,
		TeamID:        u.TeamID,
		Name:          u.Name,
		RealName:      u.RealName,
		DisplayName:   u.DisplayName,
		Email:         u.Email,
		PrincipalType: pt,
		OwnerID:       u.OwnerID,
		IsBot:         u.IsBot,
		IsAdmin:       u.IsAdmin,
		IsOwner:       u.IsOwner,
		IsRestricted:  u.IsRestricted,
		Deleted:       u.Deleted,
		Profile:       profile,
		CreatedAt:     tsToTime(u.CreatedAt),
		UpdatedAt:     tsToTime(u.UpdatedAt),
	}, nil
}

func createUserRowToFields(r sqlcgen.CreateUserRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, IsBot: r.IsBot, IsAdmin: r.IsAdmin, IsOwner: r.IsOwner,
		IsRestricted: r.IsRestricted, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func getUserRowToFields(r sqlcgen.GetUserRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, IsBot: r.IsBot, IsAdmin: r.IsAdmin, IsOwner: r.IsOwner,
		IsRestricted: r.IsRestricted, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func getUserByEmailRowToFields(r sqlcgen.GetUserByEmailRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, IsBot: r.IsBot, IsAdmin: r.IsAdmin, IsOwner: r.IsOwner,
		IsRestricted: r.IsRestricted, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func updateUserRowToFields(r sqlcgen.UpdateUserRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, IsBot: r.IsBot, IsAdmin: r.IsAdmin, IsOwner: r.IsOwner,
		IsRestricted: r.IsRestricted, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func listUserRowToFields(r sqlcgen.ListUsersRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, IsBot: r.IsBot, IsAdmin: r.IsAdmin, IsOwner: r.IsOwner,
		IsRestricted: r.IsRestricted, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func apiKeyToDomain(k sqlcgen.ApiKey) *domain.APIKey {
	return &domain.APIKey{
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
		Type:              domain.APIKeyType(k.Type),
		Environment:       domain.APIKeyEnvironment(k.Environment),
		Permissions:       k.Permissions,
		ExpiresAt:         tsToTimePtr(k.ExpiresAt),
		LastUsedAt:        tsToTimePtr(k.LastUsedAt),
		RequestCount:      k.RequestCount,
		Revoked:           k.Revoked,
		RevokedAt:         tsToTimePtr(k.RevokedAt),
		RotatedToID:       k.RotatedToID,
		GracePeriodEndsAt: tsToTimePtr(k.GracePeriodEndsAt),
		CreatedAt:         tsToTime(k.CreatedAt),
		UpdatedAt:         tsToTime(k.UpdatedAt),
	}
}

func convToDomain(c sqlcgen.Conversation) *domain.Conversation {
	return &domain.Conversation{
		ID:         c.ID,
		TeamID:     c.TeamID,
		Name:       c.Name,
		Type:       domain.ConversationType(c.Type),
		CreatorID:  c.CreatorID,
		IsArchived: c.IsArchived,
		Topic: domain.TopicPurpose{
			Value:   c.TopicValue,
			Creator: c.TopicCreator,
			LastSet: tsToTimePtr(c.TopicLastSet),
		},
		Purpose: domain.TopicPurpose{
			Value:   c.PurposeValue,
			Creator: c.PurposeCreator,
			LastSet: tsToTimePtr(c.PurposeLastSet),
		},
		NumMembers: int(c.NumMembers),
		CreatedAt:  tsToTime(c.CreatedAt),
		UpdatedAt:  tsToTime(c.UpdatedAt),
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

func usergroupToDomain(ug sqlcgen.Usergroup) *domain.Usergroup {
	return &domain.Usergroup{
		ID:          ug.ID,
		TeamID:      ug.TeamID,
		Name:        ug.Name,
		Handle:      ug.Handle,
		Description: ug.Description,
		IsExternal:  ug.IsExternal,
		Enabled:     ug.Enabled,
		UserCount:   int(ug.UserCount),
		CreatedBy:   ug.CreatedBy,
		UpdatedBy:   ug.UpdatedBy,
		CreatedAt:   tsToTime(ug.CreatedAt),
		UpdatedAt:   tsToTime(ug.UpdatedAt),
	}
}

func pinToDomain(p sqlcgen.Pin) *domain.Pin {
	return &domain.Pin{
		ChannelID: p.ChannelID,
		MessageTS: p.MessageTs,
		PinnedBy:  p.PinnedBy,
		PinnedAt:  tsToTime(p.PinnedAt),
	}
}

func bookmarkToDomain(b sqlcgen.Bookmark) *domain.Bookmark {
	return &domain.Bookmark{
		ID:        b.ID,
		ChannelID: b.ChannelID,
		Title:     b.Title,
		Type:      b.Type,
		Link:      b.Link,
		Emoji:     b.Emoji,
		CreatedBy: b.CreatedBy,
		UpdatedBy: b.UpdatedBy,
		CreatedAt: tsToTime(b.CreatedAt),
		UpdatedAt: tsToTime(b.UpdatedAt),
	}
}

func fileToDomain(f sqlcgen.GetFileRow) *domain.File {
	return &domain.File{
		ID:                 f.ID,
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
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, EventTypes: e.EventTypes,
		Secret: e.Secret, EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func getEventSubRowToDomain(e sqlcgen.GetEventSubscriptionRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, EventTypes: e.EventTypes,
		Secret: e.Secret, EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func updateEventSubRowToDomain(e sqlcgen.UpdateEventSubscriptionRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, EventTypes: e.EventTypes,
		Secret: e.Secret, EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func listEventSubRowToDomain(e sqlcgen.ListEventSubscriptionsRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, EventTypes: e.EventTypes,
		Secret: e.Secret, EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func listEventSubByTeamEventRowToDomain(e sqlcgen.ListEventSubscriptionsByTeamAndEventRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, EventTypes: e.EventTypes,
		Secret: e.Secret, EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func createTokenRowToDomain(t sqlcgen.CreateTokenRow) *domain.Token {
	return &domain.Token{
		ID:        t.ID,
		TeamID:    t.TeamID,
		UserID:    t.UserID,
		Token:     t.Token,
		TokenHash: t.TokenHash,
		Scopes:    t.Scopes,
		IsBot:     t.IsBot,
		ExpiresAt: tsToTimePtr(t.ExpiresAt),
		CreatedAt: tsToTime(t.CreatedAt),
	}
}

func tokenHashRowToDomain(t sqlcgen.GetByTokenHashRow) *domain.Token {
	return &domain.Token{
		ID:        t.ID,
		TeamID:    t.TeamID,
		UserID:    t.UserID,
		Token:     t.Token,
		TokenHash: t.TokenHash,
		Scopes:    t.Scopes,
		IsBot:     t.IsBot,
		ExpiresAt: tsToTimePtr(t.ExpiresAt),
		CreatedAt: tsToTime(t.CreatedAt),
	}
}

func tokenByIDRowToDomain(t sqlcgen.GetTokenByIDRow) *domain.Token {
	return &domain.Token{
		ID:        t.ID,
		TeamID:    t.TeamID,
		UserID:    t.UserID,
		Token:     t.Token,
		TokenHash: t.TokenHash,
		Scopes:    t.Scopes,
		IsBot:     t.IsBot,
		ExpiresAt: tsToTimePtr(t.ExpiresAt),
		CreatedAt: tsToTime(t.CreatedAt),
	}
}
