package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockAuthRepo struct {
	tokens map[string]*domain.Token
}

func newMockAuthRepo() *mockAuthRepo {
	return &mockAuthRepo{tokens: make(map[string]*domain.Token)}
}

func (m *mockAuthRepo) CreateToken(_ context.Context, params domain.CreateTokenParams) (*domain.Token, error) {
	rawToken := "xoxb-test-token-123"
	tokenHash := crypto.HashToken(rawToken)
	t := &domain.Token{
		ID:        "TK123",
		TeamID:    params.TeamID,
		UserID:    params.UserID,
		Token:     rawToken,
		TokenHash: tokenHash,
		Scopes:    params.Scopes,
		IsBot:     params.IsBot,
	}
	m.tokens[tokenHash] = t
	return t, nil
}

func (m *mockAuthRepo) GetByTokenHash(_ context.Context, tokenHash string) (*domain.Token, error) {
	t, ok := m.tokens[tokenHash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return t, nil
}

func (m *mockAuthRepo) RevokeToken(_ context.Context, token string) error {
	tokenHash := crypto.HashToken(token)
	if _, ok := m.tokens[tokenHash]; !ok {
		return domain.ErrNotFound
	}
	delete(m.tokens, tokenHash)
	return nil
}

func (m *mockAuthRepo) WithTx(_ pgx.Tx) repository.AuthRepository { return m }

func TestAuthService_CreateAndValidate(t *testing.T) {
	authRepo := newMockAuthRepo()
	svc := NewAuthService(authRepo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil)

	// Create token
	tok, err := svc.CreateToken(context.Background(), domain.CreateTokenParams{
		TeamID: "T123",
		UserID: "U123",
		Scopes: []string{"chat:write", "channels:read"},
		IsBot:  true,
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if tok.Token == "" {
		t.Fatal("token should not be empty")
	}

	// Validate token
	resp, err := svc.ValidateToken(context.Background(), "Bearer "+tok.Token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if resp.TeamID != "T123" {
		t.Errorf("got team_id %q, want T123", resp.TeamID)
	}
	if resp.UserID != "U123" {
		t.Errorf("got user_id %q, want U123", resp.UserID)
	}
	if !resp.IsBot {
		t.Error("expected is_bot to be true")
	}

	// Validate invalid token
	_, err = svc.ValidateToken(context.Background(), "Bearer invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}

	// Validate empty token
	_, err = svc.ValidateToken(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestAuthService_RevokeToken(t *testing.T) {
	authRepo := newMockAuthRepo()
	svc := NewAuthService(authRepo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil)

	tok, err := svc.CreateToken(context.Background(), domain.CreateTokenParams{
		TeamID: "T123",
		UserID: "U123",
		IsBot:  false,
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Revoke
	if err := svc.RevokeToken(context.Background(), tok.Token); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Validate after revoke should fail
	_, err = svc.ValidateToken(context.Background(), "Bearer "+tok.Token)
	if err == nil {
		t.Fatal("expected error after revoke")
	}

	// Revoke non-existent
	err = svc.RevokeToken(context.Background(), "non-existent")
	if err == nil {
		t.Fatal("expected error for non-existent token")
	}
}

func TestAuthService_ValidationErrors(t *testing.T) {
	authRepo := newMockAuthRepo()
	svc := NewAuthService(authRepo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil)

	// Missing team_id
	_, err := svc.CreateToken(context.Background(), domain.CreateTokenParams{
		UserID: "U123",
	})
	if err == nil {
		t.Fatal("expected error for missing team_id")
	}

	// Missing user_id
	_, err = svc.CreateToken(context.Background(), domain.CreateTokenParams{
		TeamID: "T123",
	})
	if err == nil {
		t.Fatal("expected error for missing user_id")
	}

	// Empty revoke
	err = svc.RevokeToken(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}
