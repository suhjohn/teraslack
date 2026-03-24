package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockAuthRepo struct {
	sessions map[string]*domain.AuthSession
}

func newMockAuthRepo() *mockAuthRepo {
	return &mockAuthRepo{sessions: make(map[string]*domain.AuthSession)}
}

func (m *mockAuthRepo) WithTx(_ pgx.Tx) repository.AuthRepository { return m }

func (m *mockAuthRepo) CreateSession(_ context.Context, params domain.CreateAuthSessionParams) (*domain.AuthSession, error) {
	session := &domain.AuthSession{
		ID:        "AS123",
		TeamID:    params.TeamID,
		UserID:    params.UserID,
		Provider:  params.Provider,
		Token:     "sess_test",
		ExpiresAt: params.ExpiresAt,
		CreatedAt: time.Now().UTC(),
	}
	m.sessions[crypto.HashToken(session.Token)] = session
	return session, nil
}

func (m *mockAuthRepo) GetSessionByHash(_ context.Context, sessionHash string) (*domain.AuthSession, error) {
	session, ok := m.sessions[sessionHash]
	if !ok {
		return nil, domain.ErrInvalidAuth
	}
	return session, nil
}

func (m *mockAuthRepo) RevokeSessionByHash(_ context.Context, sessionHash string) error {
	session, ok := m.sessions[sessionHash]
	if !ok {
		return domain.ErrInvalidAuth
	}
	now := time.Now().UTC()
	session.RevokedAt = &now
	return nil
}

func (m *mockAuthRepo) GetOAuthAccount(_ context.Context, _ string, _ domain.AuthProvider, _ string) (*domain.OAuthAccount, error) {
	return nil, domain.ErrNotFound
}

func (m *mockAuthRepo) UpsertOAuthAccount(_ context.Context, params domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error) {
	return &domain.OAuthAccount{
		ID:              "OA123",
		TeamID:          params.TeamID,
		UserID:          params.UserID,
		Provider:        params.Provider,
		ProviderSubject: params.ProviderSubject,
		Email:           params.Email,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}, nil
}

func TestAuthService_ValidateSession(t *testing.T) {
	repo := newMockAuthRepo()
	session := &domain.AuthSession{
		ID:        "AS123",
		TeamID:    "T123",
		UserID:    "U123",
		Provider:  domain.AuthProviderGitHub,
		Token:     "sess_valid",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		CreatedAt: time.Now().UTC(),
	}
	repo.sessions[crypto.HashToken(session.Token)] = session

	svc := NewAuthService(repo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil, AuthConfig{})
	auth, err := svc.ValidateSession(context.Background(), "Bearer "+session.Token)
	if err != nil {
		t.Fatalf("ValidateSession() error = %v", err)
	}
	if auth.TeamID != "T123" || auth.UserID != "U123" || auth.IsBot {
		t.Fatalf("unexpected auth context: %+v", auth)
	}
	if auth.PrincipalType != domain.PrincipalTypeHuman || auth.AccountType != domain.AccountTypeMember {
		t.Fatalf("unexpected principal in auth context: %+v", auth)
	}
}

func TestAuthService_ValidateSession_RejectsRevokedSession(t *testing.T) {
	repo := newMockAuthRepo()
	raw := "sess_revoked"
	now := time.Now().UTC()
	repo.sessions[crypto.HashToken(raw)] = &domain.AuthSession{
		ID:        "AS123",
		TeamID:    "T123",
		UserID:    "U123",
		Provider:  domain.AuthProviderGoogle,
		ExpiresAt: now.Add(time.Hour),
		RevokedAt: &now,
		CreatedAt: now,
	}

	svc := NewAuthService(repo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil, AuthConfig{})
	_, err := svc.ValidateSession(context.Background(), "Bearer "+raw)
	if !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("ValidateSession() error = %v", err)
	}
}

func TestAuthService_RevokeSession(t *testing.T) {
	repo := newMockAuthRepo()
	session := &domain.AuthSession{
		ID:        "AS123",
		TeamID:    "T123",
		UserID:    "U123",
		Provider:  domain.AuthProviderGitHub,
		Token:     "sess_valid",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		CreatedAt: time.Now().UTC(),
	}
	hash := crypto.HashToken(session.Token)
	repo.sessions[hash] = session

	svc := NewAuthService(repo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil, AuthConfig{})
	if err := svc.RevokeSession(context.Background(), session.Token); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if repo.sessions[hash].RevokedAt == nil {
		t.Fatal("expected session to be revoked")
	}
}
