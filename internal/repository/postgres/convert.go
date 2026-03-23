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
	PrincipalType, OwnerID, AccountType            string
	IsBot, Deleted                                 bool
	Profile                                        []byte
	CreatedAt, UpdatedAt                           time.Time
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
		TeamID:        u.TeamID,
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
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func getUserRowToFields(r sqlcgen.GetUserRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func getUserByTeamEmailRowToFields(r sqlcgen.GetUserByTeamEmailRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func updateUserRowToFields(r sqlcgen.UpdateUserRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func listUserRowToFields(r sqlcgen.ListUsersRow) userFields {
	return userFields{
		ID: r.ID, TeamID: r.TeamID, Name: r.Name, RealName: r.RealName,
		DisplayName: r.DisplayName, Email: r.Email, PrincipalType: r.PrincipalType,
		OwnerID: r.OwnerID, AccountType: r.AccountType, IsBot: r.IsBot, Deleted: r.Deleted, Profile: r.Profile,
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
		TeamID:             f.TeamID,
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
		TeamID:             f.TeamID,
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
		TeamID:             f.TeamID,
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
		TeamID:             f.TeamID,
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
		TeamID:             f.TeamID,
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
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func getEventSubRowToDomain(e sqlcgen.GetEventSubscriptionRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func updateEventSubRowToDomain(e sqlcgen.UpdateEventSubscriptionRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func listEventSubRowToDomain(e sqlcgen.ListEventSubscriptionsRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func listEventSubByTeamEventRowToDomain(e sqlcgen.ListEventSubscriptionsByTeamAndEventRow) *domain.EventSubscription {
	return &domain.EventSubscription{
		ID: e.ID, TeamID: e.TeamID, URL: e.Url, Type: e.EventType, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		EncryptedSecret: e.EncryptedSecret, Enabled: e.Enabled,
		CreatedAt: tsToTime(e.CreatedAt), UpdatedAt: tsToTime(e.UpdatedAt),
	}
}

func authSessionToDomain(s sqlcgen.AuthSession) *domain.AuthSession {
	return &domain.AuthSession{
		ID:        s.ID,
		TeamID:    s.TeamID,
		UserID:    s.UserID,
		Provider:  domain.AuthProvider(s.Provider),
		ExpiresAt: tsToTime(s.ExpiresAt),
		RevokedAt: tsToTimePtr(s.RevokedAt),
		CreatedAt: tsToTime(s.CreatedAt),
	}
}

func oauthAccountToDomain(a sqlcgen.OauthAccount) *domain.OAuthAccount {
	return &domain.OAuthAccount{
		ID:              a.ID,
		TeamID:          a.TeamID,
		UserID:          a.UserID,
		Provider:        domain.AuthProvider(a.Provider),
		ProviderSubject: a.ProviderSubject,
		Email:           a.Email,
		CreatedAt:       tsToTime(a.CreatedAt),
		UpdatedAt:       tsToTime(a.UpdatedAt),
	}
}
