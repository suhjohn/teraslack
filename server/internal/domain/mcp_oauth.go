package domain

import "time"

const (
	MCPOAuthScopeTools         = "mcp:tools"
	MCPOAuthScopeBootstrap     = "mcp:bootstrap"
	MCPOAuthScopeOfflineAccess = "offline_access"
)

var MCPOAuthSupportedScopes = []string{
	MCPOAuthScopeTools,
	MCPOAuthScopeBootstrap,
	MCPOAuthScopeOfflineAccess,
	PermissionMessagesRead,
	PermissionMessagesWrite,
	PermissionUsersCreate,
	PermissionAPIKeysCreate,
	PermissionConversationsCreate,
	PermissionConversationsManagersWrite,
	PermissionConversationsMembersWrite,
	PermissionConversationsPostingPolicyWrite,
	PermissionFilesRead,
	PermissionFilesWrite,
}

type MCPOAuthClientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

type MCPOAuthAuthorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
}

type MCPOAuthAuthorizePrompt struct {
	Request         MCPOAuthAuthorizeRequest
	Client          MCPOAuthClientMetadata
	WorkspaceID     string
	UserID          string
	UserName        string
	UserEmail       string
	RequestedScopes []string
}

type MCPOAuthApproveRequest struct {
	MCPOAuthAuthorizeRequest
	Approved bool
}

type MCPOAuthAuthorizationCode struct {
	ID                  string
	CodeHash            string
	ClientID            string
	ClientName          string
	RedirectURI         string
	WorkspaceID         string
	UserID              string
	Scopes              []string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
	UsedAt              *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreateMCPOAuthAuthorizationCodeParams struct {
	ClientID            string
	ClientName          string
	RedirectURI         string
	WorkspaceID         string
	UserID              string
	Scopes              []string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
}

type MCPOAuthRefreshToken struct {
	ID          string
	TokenHash   string
	ClientID    string
	ClientName  string
	WorkspaceID string
	UserID      string
	Scopes      []string
	Resource    string
	ExpiresAt   time.Time
	RevokedAt   *time.Time
	RotatedToID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateMCPOAuthRefreshTokenParams struct {
	ClientID    string
	ClientName  string
	WorkspaceID string
	UserID      string
	Scopes      []string
	Resource    string
	ExpiresAt   time.Time
}

type MCPOAuthTokenExchangeParams struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	CodeVerifier string
	RefreshToken string
	Scope        string
	Resource     string
}

type MCPOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type MCPOAuthProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
}

type MCPOAuthAuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
}

type OAuthProtocolError struct {
	StatusCode   int
	Code         string
	Description  string
	ResourceMeta string
	Scope        string
}

func (e *OAuthProtocolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Description != "" {
		return e.Description
	}
	return e.Code
}
