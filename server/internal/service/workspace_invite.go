package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

const workspaceInviteTTL = 7 * 24 * time.Hour

type WorkspaceInviteService struct {
	repo        repository.WorkspaceInviteRepository
	userRepo    repository.UserRepository
	frontendURL string
}

func NewWorkspaceInviteService(repo repository.WorkspaceInviteRepository, userRepo repository.UserRepository, frontendURL string) *WorkspaceInviteService {
	return &WorkspaceInviteService{
		repo:        repo,
		userRepo:    userRepo,
		frontendURL: strings.TrimRight(frontendURL, "/"),
	}
}

func (s *WorkspaceInviteService) Create(ctx context.Context, teamID, email string) (*domain.CreateWorkspaceInviteResult, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("invite repo: %w", domain.ErrInvalidArgument)
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, fmt.Errorf("email: %w", domain.ErrInvalidArgument)
	}

	resolvedTeamID, err := resolveTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	actor, err := requireWorkspaceAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	if actor.TeamID != resolvedTeamID {
		return nil, domain.ErrForbidden
	}
	if existing, err := s.userRepo.GetByTeamEmail(ctx, resolvedTeamID, email); err == nil && existing != nil {
		return nil, domain.ErrAlreadyExists
	} else if err != nil && err != domain.ErrNotFound {
		return nil, err
	}

	token, err := randomInviteToken()
	if err != nil {
		return nil, err
	}
	invite, err := s.repo.Create(ctx, domain.CreateWorkspaceInviteParams{
		TeamID:    resolvedTeamID,
		Email:     email,
		InvitedBy: actor.ID,
		ExpiresAt: time.Now().UTC().Add(workspaceInviteTTL),
	}, crypto.HashToken(token))
	if err != nil {
		return nil, err
	}

	return &domain.CreateWorkspaceInviteResult{
		Invite:    invite,
		InviteURL: s.inviteURL(token),
	}, nil
}

func (s *WorkspaceInviteService) inviteURL(token string) string {
	if s.frontendURL == "" {
		return "/login?invite=" + url.QueryEscape(token)
	}
	return s.frontendURL + "/login?invite=" + url.QueryEscape(token)
}

func randomInviteToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate invite token: %w", err)
	}
	return "invite_" + hex.EncodeToString(buf), nil
}
