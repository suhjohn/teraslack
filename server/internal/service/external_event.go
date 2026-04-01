package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ExternalEventService struct {
	repo repository.ExternalEventRepository
}

func NewExternalEventService(repo repository.ExternalEventRepository) *ExternalEventService {
	return &ExternalEventService{repo: repo}
}

func (s *ExternalEventService) List(ctx context.Context, params domain.ListExternalEventsParams) (*domain.CursorPage[domain.ExternalEvent], error) {
	workspaceID := ctxutil.GetWorkspaceID(ctx)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace_id: %w", domain.ErrInvalidAuth)
	}
	if params.ResourceID != "" && params.ResourceType == "" {
		return nil, fmt.Errorf("resource_type: %w", domain.ErrInvalidArgument)
	}
	if err := validateExternalResourceType(params.ResourceType); err != nil {
		return nil, err
	}

	principal := repository.ExternalEventPrincipal{
		WorkspaceID:      workspaceID,
		UserID:      ctxutil.GetActingUserID(ctx),
		APIKeyID:    ctxutil.GetAPIKeyID(ctx),
		Permissions: ctxutil.GetPermissions(ctx),
	}
	cursorState := externalEventCursor{
		WorkspaceID:       principal.WorkspaceID,
		UserID:       principal.UserID,
		APIKeyID:     principal.APIKeyID,
		Type:         params.Type,
		ResourceType: params.ResourceType,
		ResourceID:   params.ResourceID,
	}

	if params.Cursor != "" {
		decoded, err := decodeExternalEventCursor(params.Cursor)
		if err != nil {
			return nil, fmt.Errorf("after: %w", domain.ErrInvalidArgument)
		}
		if decoded.WorkspaceID != cursorState.WorkspaceID ||
			decoded.UserID != cursorState.UserID ||
			decoded.APIKeyID != cursorState.APIKeyID ||
			decoded.Type != cursorState.Type ||
			decoded.ResourceType != cursorState.ResourceType ||
			decoded.ResourceID != cursorState.ResourceID {
			return nil, fmt.Errorf("after: %w", domain.ErrInvalidArgument)
		}
		params.AfterID = decoded.AfterID
	}

	page, err := s.repo.ListVisible(ctx, principal, params)
	if err != nil {
		return nil, err
	}
	if len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		cursorState.AfterID = last.ID
	}
	if len(page.Items) > 0 {
		nextCursor, err := encodeExternalEventCursor(cursorState)
		if err != nil {
			return nil, fmt.Errorf("encode next cursor: %w", err)
		}
		page.NextCursor = nextCursor
	}
	return page, nil
}

type externalEventCursor struct {
	AfterID      int64  `json:"after_id"`
	WorkspaceID       string `json:"workspace_id"`
	UserID       string `json:"user_id,omitempty"`
	APIKeyID     string `json:"api_key_id,omitempty"`
	Type         string `json:"type,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
}

func encodeExternalEventCursor(cursor externalEventCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeExternalEventCursor(raw string) (externalEventCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return externalEventCursor{}, err
	}
	var cursor externalEventCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return externalEventCursor{}, err
	}
	return cursor, nil
}

func validateExternalResourceType(resourceType string) error {
	switch resourceType {
	case "", domain.ResourceTypeWorkspace, domain.ResourceTypeConversation, domain.ResourceTypeFile, domain.ResourceTypeUser:
		return nil
	default:
		return fmt.Errorf("resource_type: %w", domain.ErrInvalidArgument)
	}
}
