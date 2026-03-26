package domain

import "time"

type AuthProvider string

const (
	AuthProviderGitHub AuthProvider = "github"
	AuthProviderGoogle AuthProvider = "google"
)

type AuthSession struct {
	ID          string       `json:"id"`
	WorkspaceID string       `json:"workspace_id"`
	UserID      string       `json:"user_id"`
	Provider    AuthProvider `json:"provider"`
	Token       string       `json:"token,omitempty"`
	ExpiresAt   time.Time    `json:"expires_at"`
	RevokedAt   *time.Time   `json:"revoked_at,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
}

func (s *AuthSession) Redacted() *AuthSession {
	copy := *s
	copy.Token = ""
	return &copy
}

type AuthContext struct {
	WorkspaceID   string        `json:"workspace_id"`
	UserID        string        `json:"user_id"`
	PrincipalType PrincipalType `json:"principal_type"`
	AccountType   AccountType   `json:"account_type,omitempty"`
	IsBot         bool          `json:"is_bot"`
	Permissions   []string      `json:"permissions,omitempty"`
	Scopes        []string      `json:"scopes,omitempty"`
}

type StartOAuthParams struct {
	Provider    AuthProvider `json:"provider"`
	WorkspaceID string       `json:"workspace_id"`
	InviteToken string       `json:"invite_token"`
	RedirectTo  string       `json:"redirect_to"`
}

type CompleteOAuthParams struct {
	Provider AuthProvider `json:"provider"`
	Code     string       `json:"code"`
	State    string       `json:"state"`
	Nonce    string       `json:"nonce"`
}

type StartOAuthResult struct {
	AuthorizationURL string
	Nonce            string
}

type CompleteOAuthResult struct {
	Session    *AuthSession
	RedirectTo string
}

type OAuthAccount struct {
	ID              string       `json:"id"`
	WorkspaceID     string       `json:"workspace_id"`
	UserID          string       `json:"user_id"`
	Provider        AuthProvider `json:"provider"`
	ProviderSubject string       `json:"provider_subject"`
	Email           string       `json:"email"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type CreateAuthSessionParams struct {
	WorkspaceID string
	UserID      string
	Provider    AuthProvider
	ExpiresAt   time.Time
}

type UpsertOAuthAccountParams struct {
	WorkspaceID     string
	UserID          string
	Provider        AuthProvider
	ProviderSubject string
	Email           string
}
