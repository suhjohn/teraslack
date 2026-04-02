package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// ConversationHandler handles HTTP requests for conversation operations.
type ConversationHandler struct {
	svc       *service.ConversationService
	accessSvc *service.ConversationAccessService
}

// NewConversationHandler creates a new ConversationHandler.
func NewConversationHandler(svc *service.ConversationService, accessSvc *service.ConversationAccessService) *ConversationHandler {
	return &ConversationHandler{svc: svc, accessSvc: accessSvc}
}

// Create handles POST /conversations.
func (h *ConversationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateConversationParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if workspaceID := ctxutil.GetWorkspaceID(r.Context()); workspaceID != "" && params.WorkspaceID == "" {
		params.WorkspaceID = workspaceID
	}

	conv, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/conversations/"+conv.ID, conv)
}

// Info handles GET /conversations/{id}.
func (h *ConversationHandler) Info(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.Get(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, conv)
}

// Update handles PATCH /conversations/{id}.
func (h *ConversationHandler) Update(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req ConversationUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if req.Name != nil {
		if _, err := h.svc.Update(r.Context(), channelID, domain.UpdateConversationParams{Name: req.Name}); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	if req.IsArchived != nil {
		var err error
		if *req.IsArchived {
			err = h.svc.Archive(r.Context(), channelID)
		} else {
			err = h.svc.Unarchive(r.Context(), channelID)
		}
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	if req.Topic != nil {
		actorID := service.CompatibilityActorID(r.Context())
		if _, err := h.svc.SetTopic(r.Context(), channelID, domain.SetTopicParams{
			Topic:   *req.Topic,
			SetByID: actorID,
		}); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	if req.Purpose != nil {
		actorID := service.CompatibilityActorID(r.Context())
		if _, err := h.svc.SetPurpose(r.Context(), channelID, domain.SetPurposeParams{
			Purpose: *req.Purpose,
			SetByID: actorID,
		}); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}

	conv, err := h.svc.Get(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, conv)
}

// List handles GET /conversations.
func (h *ConversationHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	var types []domain.ConversationType
	if t := q.Get("types"); t != "" {
		for _, s := range strings.Split(t, ",") {
			types = append(types, domain.ConversationType(strings.TrimSpace(s)))
		}
	}

	excludeArchived, _ := strconv.ParseBool(q.Get("exclude_archived"))

	params := domain.ListConversationsParams{
		WorkspaceID:     q.Get("workspace_id"),
		Types:           types,
		ExcludeArchived: excludeArchived,
		Cursor:          q.Get("cursor"),
		Limit:           limit,
	}
	if workspaceID := ctxutil.GetWorkspaceID(r.Context()); workspaceID != "" {
		params.WorkspaceID = workspaceID
	}

	page, err := h.svc.List(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	nextCursor := ""
	if page.HasMore {
		nextCursor = page.NextCursor
	}
	httputil.WriteCollection(w, http.StatusOK, page.Items, nextCursor)
}

// Invite handles POST /conversations/{id}/members.
func (h *ConversationHandler) Invite(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req ConversationInviteRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	for _, uid := range req.UserIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if err := h.svc.Invite(r.Context(), channelID, uid); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}

	conv, err := h.svc.Get(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, conv)
}

// Kick handles DELETE /conversations/{id}/members/{user_id}.
func (h *ConversationHandler) Kick(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	userID := r.PathValue("user_id")
	if channelID == "" || userID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Kick(r.Context(), channelID, userID); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// Members handles GET /conversations/{id}/members.
func (h *ConversationHandler) Members(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := h.svc.ListMembers(r.Context(), channelID, q.Get("cursor"), limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	userIDs := make([]string, len(page.Items))
	for i, m := range page.Items {
		userIDs[i] = m.UserID
	}

	nextCursor := ""
	if page.HasMore {
		nextCursor = page.NextCursor
	}
	httputil.WriteCollection(w, http.StatusOK, userIDs, nextCursor)
}

func (h *ConversationHandler) ListManagers(w http.ResponseWriter, r *http.Request) {
	if h.accessSvc == nil {
		httputil.WriteInternalError(w, r)
		return
	}
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	userIDs, err := h.accessSvc.ListManagers(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, ConversationManagersResponse{
		ConversationID: channelID,
		UserIDs:        userIDs,
	})
}

func (h *ConversationHandler) SetManagers(w http.ResponseWriter, r *http.Request) {
	if h.accessSvc == nil {
		httputil.WriteInternalError(w, r)
		return
	}
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req ConversationManagersUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	userIDs, err := h.accessSvc.SetManagers(r.Context(), channelID, req.UserIDs)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, ConversationManagersResponse{
		ConversationID: channelID,
		UserIDs:        userIDs,
	})
}

func (h *ConversationHandler) GetPostingPolicy(w http.ResponseWriter, r *http.Request) {
	if h.accessSvc == nil {
		httputil.WriteInternalError(w, r)
		return
	}
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	policy, err := h.accessSvc.GetPostingPolicy(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	resp := ConversationPostingPolicyResponse{
		ConversationID:        policy.ConversationID,
		PolicyType:            policy.PolicyType,
		AllowedAccountTypes:   policy.AllowedAccountTypes,
		AllowedDelegatedRoles: policy.AllowedDelegatedRoles,
		AllowedUserIDs:        policy.AllowedUserIDs,
		UpdatedBy:             policy.UpdatedBy,
	}
	if !policy.UpdatedAt.IsZero() {
		resp.UpdatedAt = &policy.UpdatedAt
	}
	httputil.WriteResource(w, http.StatusOK, resp)
}

func (h *ConversationHandler) SetPostingPolicy(w http.ResponseWriter, r *http.Request) {
	if h.accessSvc == nil {
		httputil.WriteInternalError(w, r)
		return
	}
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req ConversationPostingPolicyUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	policy, err := h.accessSvc.SetPostingPolicy(r.Context(), domain.ConversationPostingPolicy{
		ConversationID:        channelID,
		PolicyType:            req.PolicyType,
		AllowedAccountTypes:   req.AllowedAccountTypes,
		AllowedDelegatedRoles: req.AllowedDelegatedRoles,
		AllowedUserIDs:        req.AllowedUserIDs,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, ConversationPostingPolicyResponse{
		ConversationID:        policy.ConversationID,
		PolicyType:            policy.PolicyType,
		AllowedAccountTypes:   policy.AllowedAccountTypes,
		AllowedDelegatedRoles: policy.AllowedDelegatedRoles,
		AllowedUserIDs:        policy.AllowedUserIDs,
		UpdatedBy:             policy.UpdatedBy,
		UpdatedAt:             &policy.UpdatedAt,
	})
}
