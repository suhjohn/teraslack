package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// UsergroupService contains business logic for usergroup operations.
type UsergroupService struct {
	repo     repository.UsergroupRepository
	userRepo repository.UserRepository
	recorder EventRecorder
	db       repository.TxBeginner
	logger   *slog.Logger
}

// NewUsergroupService creates a new UsergroupService.
func NewUsergroupService(repo repository.UsergroupRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *UsergroupService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &UsergroupService{repo: repo, userRepo: userRepo, recorder: recorder, db: db, logger: logger}
}

func (s *UsergroupService) Create(ctx context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error) {
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return nil, err
		}
	}
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	if params.Name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	if params.Handle == "" {
		return nil, fmt.Errorf("handle: %w", domain.ErrInvalidArgument)
	}
	actorID, err := resolveActorID(ctx, params.CreatedBy)
	if err != nil {
		return nil, err
	}
	params.CreatedBy = actorID
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	ug, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(ug)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUsergroupCreated,
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   ug.ID,
		TeamID:        ug.TeamID,
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record usergroup.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return ug, nil
}

func (s *UsergroupService) Get(ctx context.Context, id string) (*domain.Usergroup, error) {
	if id == "" {
		return nil, fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	ug, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, ug.TeamID); err != nil {
		return nil, err
	}
	return ug, nil
}

func (s *UsergroupService) Update(ctx context.Context, id string, params domain.UpdateUsergroupParams) (*domain.Usergroup, error) {
	if id == "" {
		return nil, fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return nil, err
		}
	}

	actorID, err := resolveActorID(ctx, params.UpdatedBy)
	if err != nil {
		return nil, err
	}
	params.UpdatedBy = actorID
	ug, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, ug.TeamID); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	ug, err = s.repo.WithTx(tx).Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(ug)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUsergroupUpdated,
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   ug.ID,
		TeamID:        ug.TeamID,
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record usergroup.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return ug, nil
}

func (s *UsergroupService) List(ctx context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error) {
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	return s.repo.List(ctx, params)
}

func (s *UsergroupService) Enable(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return err
		}
	}

	ug, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ensureTeamAccess(ctx, ug.TeamID); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.Enable(ctx, id); err != nil {
		return err
	}
	updatedUg, _ := txRepo.Get(ctx, id)
	if updatedUg != nil {
		payload, _ := json.Marshal(updatedUg)
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventUsergroupEnabled,
			AggregateType: domain.AggregateUsergroup,
			AggregateID:   id,
			TeamID:        updatedUg.TeamID,
			ActorID:       ctxutil.GetActingUserID(ctx),
			Payload:       payload,
		}); err != nil {
			return fmt.Errorf("record usergroup.enabled event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *UsergroupService) Disable(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return err
		}
	}

	ug, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ensureTeamAccess(ctx, ug.TeamID); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.Disable(ctx, id); err != nil {
		return err
	}
	updatedUg, _ := txRepo.Get(ctx, id)
	if updatedUg != nil {
		payload, _ := json.Marshal(updatedUg)
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventUsergroupDisabled,
			AggregateType: domain.AggregateUsergroup,
			AggregateID:   id,
			TeamID:        updatedUg.TeamID,
			ActorID:       ctxutil.GetActingUserID(ctx),
			Payload:       payload,
		}); err != nil {
			return fmt.Errorf("record usergroup.disabled event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *UsergroupService) ListUsers(ctx context.Context, usergroupID string) ([]string, error) {
	if usergroupID == "" {
		return nil, fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	ug, err := s.repo.Get(ctx, usergroupID)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, ug.TeamID); err != nil {
		return nil, err
	}
	return s.repo.ListUsers(ctx, usergroupID)
}

func (s *UsergroupService) SetUsers(ctx context.Context, usergroupID string, userIDs []string) error {
	if usergroupID == "" {
		return fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return err
		}
	}
	// Verify usergroup exists
	ug, err := s.repo.Get(ctx, usergroupID)
	if err != nil {
		return err
	}
	if err := ensureTeamAccess(ctx, ug.TeamID); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.SetUsers(ctx, usergroupID, userIDs); err != nil {
		return err
	}
	updatedUg, _ := txRepo.Get(ctx, usergroupID)
	if updatedUg != nil {
		payload, _ := json.Marshal(struct {
			UserIDs   []string          `json:"user_ids"`
			Usergroup *domain.Usergroup `json:"usergroup"`
		}{UserIDs: userIDs, Usergroup: updatedUg})
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventUsergroupUserSet,
			AggregateType: domain.AggregateUsergroup,
			AggregateID:   usergroupID,
			TeamID:        updatedUg.TeamID,
			ActorID:       ctxutil.GetActingUserID(ctx),
			Payload:       payload,
		}); err != nil {
			return fmt.Errorf("record usergroup.users_set event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
