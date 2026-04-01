package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

const workspaceInviteTTL = 7 * 24 * time.Hour

type WorkspaceInviteService struct {
	repo        repository.WorkspaceInviteRepository
	userRepo    repository.UserRepository
	auditRepo   repository.AuthorizationAuditRepository
	recorder    EventRecorder
	db          repository.TxBeginner
	frontendURL string
}

func NewWorkspaceInviteService(repo repository.WorkspaceInviteRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, frontendURL string) *WorkspaceInviteService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &WorkspaceInviteService{
		repo:        repo,
		userRepo:    userRepo,
		recorder:    recorder,
		db:          db,
		frontendURL: strings.TrimRight(frontendURL, "/"),
	}
}

func (s *WorkspaceInviteService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *WorkspaceInviteService) Create(ctx context.Context, workspaceID, email string) (*domain.CreateWorkspaceInviteResult, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("invite repo: %w", domain.ErrInvalidArgument)
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, fmt.Errorf("email: %w", domain.ErrInvalidArgument)
	}

	resolvedWorkspaceID, err := resolveWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	actor, err := requireWorkspaceAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	if actor.WorkspaceID != resolvedWorkspaceID {
		return nil, domain.ErrForbidden
	}
	if existing, err := s.userRepo.GetByTeamEmail(ctx, resolvedWorkspaceID, email); err == nil && existing != nil {
		return nil, domain.ErrAlreadyExists
	} else if err != nil && err != domain.ErrNotFound {
		return nil, err
	}

	token, err := randomInviteToken()
	if err != nil {
		return nil, err
	}
	invite, err := s.repo.Create(ctx, domain.CreateWorkspaceInviteParams{
		WorkspaceID: resolvedWorkspaceID,
		Email:       email,
		InvitedBy:   actor.ID,
		ExpiresAt:   time.Now().UTC().Add(workspaceInviteTTL),
	}, crypto.HashToken(token))
	if err != nil {
		return nil, err
	}

	return &domain.CreateWorkspaceInviteResult{
		Invite:    invite,
		Code:      token,
		InviteURL: s.inviteURL(token),
	}, nil
}

func (s *WorkspaceInviteService) Accept(ctx context.Context, code string) (*domain.AcceptWorkspaceInviteResult, error) {
	if s.repo == nil || s.userRepo == nil || s.db == nil {
		return nil, fmt.Errorf("invite service: %w", domain.ErrInvalidArgument)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("code: %w", domain.ErrInvalidArgument)
	}

	actor, err := loadActingUser(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	if actor.PrincipalType != domain.PrincipalTypeHuman || actor.Deleted || strings.TrimSpace(actor.Email) == "" {
		return nil, domain.ErrForbidden
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	inviteRepo := s.repo.WithTx(tx)
	userRepo := s.userRepo.WithTx(tx)

	invite, err := inviteRepo.GetByTokenHash(ctx, crypto.HashToken(code))
	if err != nil {
		return nil, err
	}
	if invite.AcceptedAt != nil || invite.ExpiresAt.Before(time.Now().UTC()) {
		return nil, domain.ErrForbidden
	}
	if !strings.EqualFold(strings.TrimSpace(invite.Email), strings.TrimSpace(actor.Email)) {
		return nil, domain.ErrForbidden
	}

	targetUser, err := userRepo.GetByTeamEmail(ctx, invite.WorkspaceID, actor.Email)
	if err == domain.ErrNotFound {
		targetUser, err = s.createAcceptedInviteUser(ctx, tx, userRepo, actor, invite.WorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	if targetUser.PrincipalType != domain.PrincipalTypeHuman || targetUser.Deleted {
		return nil, domain.ErrForbidden
	}

	acceptedAt := time.Now().UTC()
	if err := inviteRepo.MarkAccepted(ctx, invite.ID, targetUser.ID, acceptedAt); err != nil {
		return nil, err
	}

	invite.AcceptedByUserID = targetUser.ID
	invite.AcceptedAt = &acceptedAt
	invite.UpdatedAt = acceptedAt

	if err := recordAuthorizationAudit(ctx, s.auditRepo, tx, invite.WorkspaceID, domain.AuditActionWorkspaceInviteAccepted, "workspace_invite", invite.ID, map[string]any{
		"user_id":      targetUser.ID,
		"accepted_via": "api",
	}); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &domain.AcceptWorkspaceInviteResult{
		Invite: invite,
		User:   targetUser,
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

func (s *WorkspaceInviteService) createAcceptedInviteUser(ctx context.Context, tx pgx.Tx, userRepo repository.UserRepository, actor *domain.User, workspaceID string) (*domain.User, error) {
	name := strings.TrimSpace(actor.Name)
	if name == "" {
		name = emailLocalPart(actor.Email)
	}
	realName := strings.TrimSpace(actor.RealName)
	if realName == "" {
		realName = name
	}
	displayName := strings.TrimSpace(actor.DisplayName)
	if displayName == "" {
		displayName = realName
	}

	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   workspaceID,
		Name:          name,
		RealName:      realName,
		DisplayName:   displayName,
		Email:         actor.Email,
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
		IsBot:         false,
		Profile:       actor.Profile,
	})
	if err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		WorkspaceID:   user.WorkspaceID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}

	return user, nil
}
