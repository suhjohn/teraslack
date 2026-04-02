package mcpoauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/suhjohn/teraslack/internal/domain"
)

const (
	AccessTokenTTL  = time.Hour
	RefreshTokenTTL = 30 * 24 * time.Hour
	AuthCodeTTL     = 10 * time.Minute
)

type TokenConfig struct {
	Issuer      string
	MCPAudience string
	APIAudience string
	SigningKey  string
}

type AccessTokenClaims struct {
	WorkspaceID   string   `json:"workspace_id"`
	UserID        string   `json:"user_id"`
	AccountID     string   `json:"account_id,omitempty"`
	MembershipID  string   `json:"membership_id,omitempty"`
	PrincipalType string   `json:"principal_type"`
	AccountType   string   `json:"account_type,omitempty"`
	IsBot         bool     `json:"is_bot"`
	ClientID      string   `json:"client_id"`
	Scope         string   `json:"scope,omitempty"`
	Permissions   []string `json:"permissions,omitempty"`
	TokenUse      string   `json:"token_use"`
	jwt.RegisteredClaims
}

func IssueAccessToken(cfg TokenConfig, now time.Time, subject AccessTokenClaims) (string, int64, error) {
	issuer, err := CanonicalURL(cfg.Issuer)
	if err != nil {
		return "", 0, err
	}
	mcpAudience, err := CanonicalURL(cfg.MCPAudience)
	if err != nil {
		return "", 0, err
	}
	apiAudience, err := CanonicalURL(cfg.APIAudience)
	if err != nil {
		return "", 0, err
	}
	if strings.TrimSpace(cfg.SigningKey) == "" {
		return "", 0, fmt.Errorf("signing key is required")
	}

	exp := now.UTC().Add(AccessTokenTTL)
	subject.TokenUse = "access"
	subject.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   subject.UserID,
		Audience:  jwt.ClaimStrings{mcpAudience, apiAudience},
		ExpiresAt: jwt.NewNumericDate(exp),
		NotBefore: jwt.NewNumericDate(now.UTC().Add(-1 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now.UTC()),
		ID:        randomID("mcpat"),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, subject)
	raw, err := token.SignedString([]byte(cfg.SigningKey))
	if err != nil {
		return "", 0, fmt.Errorf("sign access token: %w", err)
	}
	return raw, int64(time.Until(exp).Seconds()), nil
}

func ValidateAccessToken(cfg TokenConfig, raw, expectedAudience string) (*AccessTokenClaims, error) {
	if strings.TrimSpace(cfg.SigningKey) == "" {
		return nil, fmt.Errorf("signing key is required")
	}
	expectedAudience, err := CanonicalURL(expectedAudience)
	if err != nil {
		return nil, err
	}
	issuer, err := CanonicalURL(cfg.Issuer)
	if err != nil {
		return nil, err
	}

	token, err := jwt.ParseWithClaims(raw, &AccessTokenClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %s", token.Method.Alg())
		}
		return []byte(cfg.SigningKey), nil
	}, jwt.WithAudience(expectedAudience), jwt.WithIssuer(issuer))
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*AccessTokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	if claims.TokenUse != "access" {
		return nil, fmt.Errorf("invalid token_use %q", claims.TokenUse)
	}
	return claims, nil
}

func CanonicalURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse url %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("absolute https URL required")
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("fragments are not allowed")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func NormalizeScopes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Fields(raw)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || slices.Contains(out, part) {
			continue
		}
		out = append(out, part)
	}
	return out
}

func ScopeString(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	return strings.Join(scopes, " ")
}

func PermissionsFromScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		switch scope {
		case domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionUsersCreate,
			domain.PermissionAPIKeysCreate,
			domain.PermissionConversationsCreate,
			domain.PermissionConversationsManagersWrite,
			domain.PermissionConversationsMembersWrite,
			domain.PermissionConversationsPostingPolicyWrite,
			domain.PermissionFilesRead,
			domain.PermissionFilesWrite:
			if !slices.Contains(out, scope) {
				out = append(out, scope)
			}
		}
	}
	return out
}

func ValidateRequestedScopes(scopes []string) error {
	for _, scope := range scopes {
		if !slices.Contains(domain.MCPOAuthSupportedScopes, scope) {
			return fmt.Errorf("unsupported scope %q", scope)
		}
	}
	return nil
}

func ValidateResource(resource, expected string) error {
	want, err := CanonicalURL(expected)
	if err != nil {
		return err
	}
	got, err := CanonicalURL(resource)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("resource must be %s", want)
	}
	return nil
}

func ValidatePKCEChallenge(challenge, method string) error {
	if strings.TrimSpace(challenge) == "" {
		return fmt.Errorf("code_challenge is required")
	}
	if strings.TrimSpace(method) != "S256" {
		return fmt.Errorf("code_challenge_method must be S256")
	}
	return nil
}

func VerifyPKCEVerifier(challenge, verifier string) bool {
	sum := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	return expected == challenge
}

func RandomSecret(prefix string) (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate random secret: %w", err)
	}
	raw := prefix + "_" + hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:]), nil
}

func randomID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return prefix
	}
	return prefix + "_" + hex.EncodeToString(buf)
}
