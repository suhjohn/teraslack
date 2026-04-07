package domain

import "github.com/google/uuid"

type AuthKind string

const (
	AuthKindSession AuthKind = "session"
	AuthKindAPIKey  AuthKind = "api_key"
)

type AuthContext struct {
	UserID            uuid.UUID
	PrincipalType     string
	AgentMode         string
	SessionID         *uuid.UUID
	APIKeyID          *uuid.UUID
	APIKeyScopeType   string
	APIKeyWorkspaceID *uuid.UUID
}
