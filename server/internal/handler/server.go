package handler

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/api"
	"github.com/johnsuh/teraslack/server/internal/config"
	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/domain"
	"github.com/johnsuh/teraslack/server/internal/eventsourcing"
)

const sessionCookieName = "teraslack_session"

type Server struct {
	cfg        config.Config
	db         *pgxpool.Pool
	queries    *dbsqlc.Queries
	httpClient *http.Client
	logger     *slog.Logger
	limiter    *rateLimiter
	protector  *teracrypto.StringProtector
}

type appError struct {
	Status  int
	Code    string
	Message string
	Errors  []api.ValidationDetail
}

func (e *appError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type userRow struct {
	ID            uuid.UUID
	PrincipalType string
	Status        string
	Email         *string
	Handle        string
	DisplayName   string
	AvatarURL     *string
	Bio           *string
}

type workspaceRow struct {
	ID              uuid.UUID
	Slug            string
	Name            string
	CreatedByUserID uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type conversationRow struct {
	ID               uuid.UUID
	WorkspaceID      *uuid.UUID
	AccessPolicy     string
	Title            *string
	Description      *string
	CreatedByUserID  uuid.UUID
	ArchivedAt       *time.Time
	LastMessageAt    *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ParticipantCount int
}

type messageRow struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	AuthorUserID   uuid.UUID
	BodyText       string
	BodyRich       map[string]any
	Metadata       map[string]any
	EditedAt       *time.Time
	DeletedAt      *time.Time
	CreatedAt      time.Time
}

func New(cfg config.Config, db *pgxpool.Pool, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	protector, err := teracrypto.NewStringProtector(context.Background(), teracrypto.Options{
		EnvKey:         cfg.EncryptionKey,
		AWSKMSKeyID:    cfg.AWSKMSKeyID,
		AWSKMSRegion:   cfg.AWSKMSRegion,
		AWSKMSEndpoint: cfg.AWSKMSEndpoint,
	})
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:     cfg,
		db:      db,
		queries: dbsqlc.New(db),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger:    logger,
		limiter:   newRateLimiter(),
		protector: protector,
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.registerOpenAPIRoutes(mux)
	return s.withCORS(s.withRequestContext(mux))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) withRequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := "req_" + uuid.NewString()
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(s.cfg.CORSAllowedOrigins))
	for _, origin := range s.cfg.CORSAllowedOrigins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
				w.Header().Add("Vary", "Origin")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next func(http.ResponseWriter, *http.Request, domain.AuthContext)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth, err := s.authenticateRequest(r.Context(), r)
		if err != nil {
			s.writeAppError(w, r, err)
			return
		}
		if auth.APIKeyID != nil {
			if !s.limiter.allow("auth:key:"+auth.APIKeyID.String(), 5000, time.Minute) {
				s.writeAppError(w, r, rateLimited("API key rate limit exceeded."))
				return
			}
		} else {
			if !s.limiter.allow("auth:user:"+auth.UserID.String(), 1000, time.Minute) {
				s.writeAppError(w, r, rateLimited("User rate limit exceeded."))
				return
			}
		}
		next(w, r, auth)
	}
}

func (s *Server) authenticateRequest(ctx context.Context, r *http.Request) (domain.AuthContext, *appError) {
	token := ""
	if header := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(header), "bearer ") {
		token = strings.TrimSpace(header[7:])
	} else if cookie, err := r.Cookie(sessionCookieName); err == nil {
		token = strings.TrimSpace(cookie.Value)
	}
	if token == "" {
		return domain.AuthContext{}, unauthorized("Missing bearer token.")
	}
	tokenHash := teracrypto.SHA256Hex(token)
	now := time.Now().UTC()

	var auth domain.AuthContext
	var sessionID uuid.UUID
	sessionAuth, err := s.queries.GetSessionAuthByTokenHash(ctx, dbsqlc.GetSessionAuthByTokenHashParams{
		TokenHash: tokenHash,
		ExpiresAt: dbsqlc.Timestamptz(now),
	})
	if err == nil {
		sessionID = sessionAuth.ID
		auth.UserID = sessionAuth.UserID
		auth.SessionID = &sessionID
		_ = s.queries.TouchAuthSessionLastSeen(ctx, dbsqlc.TouchAuthSessionLastSeenParams{
			ID:         sessionID,
			LastSeenAt: dbsqlc.Timestamptz(now),
		})
		return auth, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthContext{}, internalError(err)
	}

	keyAuth, err := s.queries.GetAPIKeyAuthBySecretHash(ctx, dbsqlc.GetAPIKeyAuthBySecretHashParams{
		SecretHash: tokenHash,
		ExpiresAt:  dbsqlc.Timestamptz(now),
	})
	if err == nil {
		keyID := keyAuth.ID
		auth.APIKeyID = &keyID
		auth.UserID = keyAuth.UserID
		auth.APIKeyScopeType = keyAuth.ScopeType
		auth.APIKeyWorkspaceID = keyAuth.ScopeWorkspaceID
		_ = s.queries.TouchAPIKeyLastUsed(ctx, dbsqlc.TouchAPIKeyLastUsedParams{
			ID:         keyID,
			LastUsedAt: dbsqlc.Timestamptz(now),
		})
		return auth, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthContext{}, unauthorized("Invalid bearer token.")
	}
	return domain.AuthContext{}, internalError(err)
}

func (s *Server) loadUser(ctx context.Context, userID uuid.UUID) (userRow, error) {
	row, err := s.queries.GetUser(ctx, userID)
	if err != nil {
		return userRow{}, err
	}
	return userRow{
		ID:            row.ID,
		PrincipalType: row.PrincipalType,
		Status:        row.Status,
		Email:         row.Email,
		Handle:        row.Handle,
		DisplayName:   row.DisplayName,
		AvatarURL:     row.AvatarUrl,
		Bio:           row.Bio,
	}, nil
}

func (s *Server) loadWorkspace(ctx context.Context, workspaceID uuid.UUID) (workspaceRow, error) {
	row, err := s.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return workspaceRow{}, err
	}
	return workspaceRow{
		ID:              row.ID,
		Slug:            row.Slug,
		Name:            row.Name,
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       dbsqlc.TimeValue(row.CreatedAt),
		UpdatedAt:       dbsqlc.TimeValue(row.UpdatedAt),
	}, nil
}

func (s *Server) loadConversation(ctx context.Context, conversationID uuid.UUID) (conversationRow, error) {
	row, err := s.queries.GetConversation(ctx, conversationID)
	if err != nil {
		return conversationRow{}, err
	}
	return conversationRow{
		ID:               row.ID,
		WorkspaceID:      row.WorkspaceID,
		AccessPolicy:     row.AccessPolicy,
		Title:            row.Title,
		Description:      row.Description,
		CreatedByUserID:  row.CreatedByUserID,
		ArchivedAt:       dbsqlc.TimePtr(row.ArchivedAt),
		LastMessageAt:    dbsqlc.TimePtr(row.LastMessageAt),
		CreatedAt:        dbsqlc.TimeValue(row.CreatedAt),
		UpdatedAt:        dbsqlc.TimeValue(row.UpdatedAt),
		ParticipantCount: int(row.ParticipantCount),
	}, nil
}

func (s *Server) ensureWorkspaceActiveMember(ctx context.Context, auth domain.AuthContext, workspaceID uuid.UUID) (role string, appErr *appError) {
	if auth.APIKeyWorkspaceID != nil && *auth.APIKeyWorkspaceID != workspaceID {
		return "", forbidden("This API key cannot access that workspace.")
	}
	membership, err := s.queries.GetWorkspaceMembership(ctx, dbsqlc.GetWorkspaceMembershipParams{
		WorkspaceID: workspaceID,
		UserID:      auth.UserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", forbidden("You do not have access to this workspace.")
	}
	if err != nil {
		return "", internalError(err)
	}
	if membership.Status != "active" {
		return "", forbidden("You do not have access to this workspace.")
	}
	return membership.Role, nil
}

func (s *Server) ensureWorkspaceAdmin(ctx context.Context, auth domain.AuthContext, workspaceID uuid.UUID) *appError {
	role, err := s.ensureWorkspaceActiveMember(ctx, auth, workspaceID)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return forbidden("You do not have access to this resource.")
	}
	return nil
}

func (s *Server) ensureGlobalUserSurfaceAccess(auth domain.AuthContext) *appError {
	if auth.APIKeyWorkspaceID != nil {
		return forbidden("This API key cannot access that resource.")
	}
	return nil
}

func (s *Server) ensureConversationAccess(ctx context.Context, auth domain.AuthContext, conversation conversationRow) *appError {
	if auth.APIKeyWorkspaceID != nil {
		if conversation.WorkspaceID == nil || *conversation.WorkspaceID != *auth.APIKeyWorkspaceID {
			return forbidden("This API key cannot access that resource.")
		}
	}
	if conversation.WorkspaceID == nil {
		switch conversation.AccessPolicy {
		case "authenticated":
			return nil
		case "members":
			ok, err := s.isConversationParticipant(ctx, conversation.ID, auth.UserID)
			if err != nil {
				return internalError(err)
			}
			if !ok {
				return forbidden("You do not have access to this resource.")
			}
			return nil
		default:
			return forbidden("You do not have access to this resource.")
		}
	}

	if _, err := s.ensureWorkspaceActiveMember(ctx, auth, *conversation.WorkspaceID); err != nil {
		return err
	}
	if conversation.AccessPolicy == "workspace" {
		return nil
	}
	ok, err := s.isConversationParticipant(ctx, conversation.ID, auth.UserID)
	if err != nil {
		return internalError(err)
	}
	if !ok {
		return forbidden("You do not have access to this resource.")
	}
	return nil
}

func (s *Server) isConversationParticipant(ctx context.Context, conversationID uuid.UUID, userID uuid.UUID) (bool, error) {
	return s.queries.IsConversationParticipant(ctx, dbsqlc.IsConversationParticipantParams{
		ConversationID: conversationID,
		UserID:         userID,
	})
}

func (s *Server) isDirectMessage(ctx context.Context, conversationID uuid.UUID) (bool, error) {
	return s.queries.IsDirectMessage(ctx, conversationID)
}

func (s *Server) insertUserWithProfile(ctx context.Context, tx pgx.Tx, email string) (userRow, error) {
	queries := s.queries.WithTx(tx)
	now := time.Now().UTC()
	userID := uuid.New()
	handle := deriveHandle(email)
	displayName := deriveDisplayName(email)
	normalized := normalizeEmail(email)
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			handle = fmt.Sprintf("%s-%d", handle, attempt+1)
		}
		if err := queries.CreateUser(ctx, dbsqlc.CreateUserParams{
			ID:        userID,
			Email:     stringPtr(normalized),
			CreatedAt: dbsqlc.Timestamptz(now),
			UpdatedAt: dbsqlc.Timestamptz(now),
		}); err != nil {
			return userRow{}, err
		}
		err := queries.CreateUserProfile(ctx, dbsqlc.CreateUserProfileParams{
			UserID:      userID,
			Handle:      handle,
			DisplayName: displayName,
			CreatedAt:   dbsqlc.Timestamptz(now),
			UpdatedAt:   dbsqlc.Timestamptz(now),
		})
		if err == nil {
			return userRow{
				ID:            userID,
				PrincipalType: "human",
				Status:        "active",
				Email:         stringPtr(normalized),
				Handle:        handle,
				DisplayName:   displayName,
			}, nil
		}
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			continue
		}
		return userRow{}, err
	}
	return userRow{}, fmt.Errorf("could not allocate unique user handle")
}

func (s *Server) appendEvent(ctx context.Context, tx pgx.Tx, eventType string, aggregateType string, aggregateID uuid.UUID, workspaceID *uuid.UUID, actorUserID *uuid.UUID, payload any) error {
	_, err := eventsourcing.AppendInternalEvent(ctx, tx, eventsourcing.InternalEvent{
		EventType:     eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		WorkspaceID:   workspaceID,
		ActorUserID:   actorUserID,
		ShardID:       eventsourcing.ShardForAggregate(aggregateID),
		Payload:       payload,
	})
	return err
}

func (s *Server) writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	appErr, ok := err.(*appError)
	if !ok {
		appErr = internalError(err)
	}
	response := api.ErrorResponse{
		Code:      appErr.Code,
		Message:   appErr.Message,
		RequestID: requestIDFromContext(r.Context()),
		Errors:    appErr.Errors,
	}
	writeJSON(w, appErr.Status, response)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(r *http.Request, target any) *appError {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return malformed("Could not read request body.")
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return malformed("Request body is required.")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return malformed("Malformed JSON request body.")
	}
	return nil
}

func parseUUIDPath(r *http.Request, name string) (uuid.UUID, error) {
	value := strings.TrimSpace(r.PathValue(name))
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.UUID{}, validationFailed(name, "invalid_uuid", "Must be a valid UUID.")
	}
	return id, nil
}

func parseOptionalUUID(raw *string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, validationFailed("id", "invalid_uuid", "Must be a valid UUID.")
	}
	return &id, nil
}

func parseLimitQuery(raw string, defaultValue int, maxValue int) (int, *appError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, malformed("Malformed query parameter: limit.")
	}
	if value <= 0 {
		return 0, validationFailed("limit", "invalid_value", "Must be greater than 0.")
	}
	if value > maxValue {
		return 0, validationFailed("limit", "invalid_value", fmt.Sprintf("Must be less than or equal to %d.", maxValue))
	}
	return value, nil
}

func parseCursor(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(string(decoded))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func parseQueryCursor(raw string) (int, *appError) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, malformed("Malformed query parameter: cursor.")
	}
	value, err := strconv.Atoi(string(decoded))
	if err != nil || value < 0 {
		return 0, malformed("Malformed query parameter: cursor.")
	}
	return value, nil
}

func formatNextCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func parseInt64Cursor(raw string) int64 {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseInt(string(decoded), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func parseQueryInt64Cursor(raw string) (int64, *appError) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, malformed("Malformed query parameter: cursor.")
	}
	value, err := strconv.ParseInt(string(decoded), 10, 64)
	if err != nil || value < 0 {
		return 0, malformed("Malformed query parameter: cursor.")
	}
	return value, nil
}

func parseBodyCursor(raw *string) (int, *appError) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(*raw))
	if err != nil {
		return 0, validationFailed("cursor", "invalid_value", "Must be a valid cursor.")
	}
	value, err := strconv.Atoi(string(decoded))
	if err != nil || value < 0 {
		return 0, validationFailed("cursor", "invalid_value", "Must be a valid cursor.")
	}
	return value, nil
}

func formatInt64Cursor(value int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(value, 10)))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func deriveHandle(email string) string {
	local := normalizeEmail(email)
	if idx := strings.Index(local, "@"); idx > 0 {
		local = local[:idx]
	}
	local = strings.Trim(local, ".-_ ")
	local = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, local)
	if local == "" {
		return "user"
	}
	return local
}

func deriveDisplayName(email string) string {
	local := normalizeEmail(email)
	if idx := strings.Index(local, "@"); idx > 0 {
		local = local[:idx]
	}
	local = strings.ReplaceAll(local, ".", " ")
	local = strings.ReplaceAll(local, "_", " ")
	local = strings.ReplaceAll(local, "-", " ")
	local = strings.TrimSpace(local)
	if local == "" {
		return "User"
	}
	return strings.Title(local)
}

func userToAPI(row userRow) api.User {
	return api.User{
		ID:            row.ID.String(),
		PrincipalType: row.PrincipalType,
		Status:        row.Status,
		Email:         row.Email,
		Profile: api.UserProfile{
			Handle:      row.Handle,
			DisplayName: row.DisplayName,
			AvatarURL:   row.AvatarURL,
			Bio:         row.Bio,
		},
	}
}

func workspaceToAPI(row workspaceRow) api.Workspace {
	return api.Workspace{
		ID:              row.ID.String(),
		Slug:            row.Slug,
		Name:            row.Name,
		CreatedByUserID: row.CreatedByUserID.String(),
		CreatedAt:       row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Format(time.RFC3339),
	}
}

func conversationToAPI(row conversationRow) api.Conversation {
	return api.Conversation{
		ID:               row.ID.String(),
		WorkspaceID:      uuidPtrToStringPtr(row.WorkspaceID),
		AccessPolicy:     row.AccessPolicy,
		ParticipantCount: row.ParticipantCount,
		Title:            row.Title,
		Description:      row.Description,
		CreatedByUserID:  row.CreatedByUserID.String(),
		Archived:         row.ArchivedAt != nil,
		LastMessageAt:    timePtrToStringPtr(row.LastMessageAt),
		CreatedAt:        row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Format(time.RFC3339),
	}
}

func messageToAPI(row messageRow) api.Message {
	return api.Message{
		ID:             row.ID.String(),
		ConversationID: row.ConversationID.String(),
		AuthorUserID:   row.AuthorUserID.String(),
		BodyText:       row.BodyText,
		BodyRich:       row.BodyRich,
		Metadata:       row.Metadata,
		EditedAt:       timePtrToStringPtr(row.EditedAt),
		DeletedAt:      timePtrToStringPtr(row.DeletedAt),
		CreatedAt:      row.CreatedAt.Format(time.RFC3339),
	}
}

func readJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func uuidPtrToStringPtr(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	s := value.String()
	return &s
}

func timePtrToStringPtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	s := value.UTC().Format(time.RFC3339)
	return &s
}

func stringPtr(value string) *string {
	return &value
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx >= 0 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return forwarded
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if host == "" {
		return "unknown"
	}
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		return host[:idx]
	}
	return host
}

type requestIDContextKey struct{}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

func unauthorized(message string) *appError {
	return &appError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: message}
}

func forbidden(message string) *appError {
	return &appError{Status: http.StatusForbidden, Code: "forbidden", Message: message}
}

func notFound(message string) *appError {
	return &appError{Status: http.StatusNotFound, Code: "not_found", Message: message}
}

func conflict(message string) *appError {
	return &appError{Status: http.StatusConflict, Code: "conflict", Message: message}
}

func malformed(message string) *appError {
	return &appError{Status: http.StatusBadRequest, Code: "bad_request", Message: message}
}

func internalError(err error) *appError {
	return &appError{Status: http.StatusInternalServerError, Code: "internal_error", Message: err.Error()}
}

func validationFailed(field string, code string, message string) *appError {
	return &appError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "Request validation failed.",
		Errors: []api.ValidationDetail{{
			Field:   field,
			Code:    code,
			Message: message,
		}},
	}
}

func rateLimited(message string) *appError {
	return &appError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: message}
}

func notConfigured(code string, message string) *appError {
	return &appError{Status: http.StatusNotImplemented, Code: code, Message: message}
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parseTimeRFC3339(raw *string, field string) (*time.Time, *appError) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, validationFailed(field, "invalid_datetime", "Must be an RFC3339 timestamp.")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func expectURL(raw string, field string) *appError {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return validationFailed(field, "invalid_format", "Must be a valid URL.")
	}
	return nil
}

func nullIfBlank(value *string) sql.NullString {
	if value == nil || strings.TrimSpace(*value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSpace(*value), Valid: true}
}
