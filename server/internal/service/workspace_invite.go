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
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

const workspaceInviteTTL = 7 * 24 * time.Hour

type WorkspaceInviteService struct {
	repo        repository.WorkspaceInviteRepository
	userRepo    repository.UserRepository
	accountRepo repository.AccountRepository
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

func (s *WorkspaceInviteService) SetIdentityRepositories(accountRepo repository.AccountRepository, _ ...any) {
	s.accountRepo = accountRepo
}

func (s *WorkspaceInviteService) Create(ctx context.Context, workspaceID, email string) (*domain.CreateWorkspaceInviteResult, error) {
	if s.repo == nil || s.accountRepo == nil {
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
	account, err := s.accountRepo.GetByEmail(ctx, email)
	if err == nil {
		if _, userErr := s.userRepo.GetByWorkspaceAndAccount(ctx, resolvedWorkspaceID, account.ID); userErr == nil {
			return nil, domain.ErrAlreadyExists
		} else if userErr != domain.ErrNotFound {
			return nil, userErr
		}
	} else if err != domain.ErrNotFound {
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
	if s.repo == nil || s.userRepo == nil || s.db == nil || s.accountRepo == nil {
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
	accountRepo := s.accountRepo.WithTx(tx)

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

	account, err := resolveInviteActorAccount(ctx, accountRepo, actor)
	if err != nil {
		return nil, err
	}
	targetUser, err := ensureInviteUser(ctx, userRepo, account, invite.WorkspaceID, s.recorder.WithTx(tx))
	if err != nil {
		return nil, err
	}
	if targetUser != nil && (targetUser.PrincipalType != domain.PrincipalTypeHuman || targetUser.Deleted) {
		return nil, domain.ErrForbidden
	}

	acceptedAt := time.Now().UTC()
	if err := inviteRepo.MarkAccepted(ctx, invite.ID, account.ID, acceptedAt); err != nil {
		return nil, err
	}

	invite.AcceptedByAccountID = account.ID
	invite.AcceptedAt = &acceptedAt
	invite.UpdatedAt = acceptedAt

	auditPayload := map[string]any{
		"accepted_via": "api",
	}
	if targetUser != nil {
		auditPayload["user_id"] = targetUser.ID
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, tx, invite.WorkspaceID, domain.AuditActionWorkspaceInviteAccepted, "workspace_invite", invite.ID, auditPayload); err != nil {
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

func resolveInviteActorAccount(ctx context.Context, accountRepo repository.AccountRepository, actor *domain.User) (*domain.Account, error) {
	if accountRepo == nil || actor == nil {
		return nil, fmt.Errorf("account: %w", domain.ErrInvalidArgument)
	}
	if accountID := strings.TrimSpace(ctxutil.GetAccountID(ctx)); accountID != "" {
		account, err := accountRepo.Get(ctx, accountID)
		if err == nil {
			return account, nil
		}
		if err != nil && err != domain.ErrNotFound {
			return nil, err
		}
	}
	return resolveOrCreateAccountForUser(ctx, accountRepo, actor)
}

func ensureInviteUser(ctx context.Context, userRepo repository.UserRepository, account *domain.Account, workspaceID string, recorder EventRecorder) (*domain.User, error) {
	if userRepo == nil || account == nil || workspaceID == "" {
		return nil, fmt.Errorf("invite user: %w", domain.ErrInvalidArgument)
	}
	user, err := userRepo.GetByWorkspaceAndAccount(ctx, workspaceID, account.ID)
	if err == nil {
		return user, nil
	}
	if err != domain.ErrNotFound {
		return nil, err
	}
	return createWorkspaceUserForAccount(ctx, userRepo, account, workspaceID, domain.AccountTypeMember, recorder)
}
