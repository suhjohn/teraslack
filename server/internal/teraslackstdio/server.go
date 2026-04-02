package teraslackstdio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/teraslackmcp"
)

type Config struct {
	BaseURL               string
	APIKey                string
	DefaultConversationID string
	DebugLogPath          string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:               strings.TrimSpace(os.Getenv("TERASLACK_BASE_URL")),
		APIKey:                strings.TrimSpace(os.Getenv("TERASLACK_API_KEY")),
		DefaultConversationID: strings.TrimSpace(os.Getenv("TERASLACK_DEFAULT_CONVERSATION_ID")),
		DebugLogPath:          strings.TrimSpace(firstNonEmptyEnv("TERASLACK_STDIO_MCP_DEBUG_LOG", "TERASLACK_MCP_DEBUG_LOG")),
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("missing required environment variable: TERASLACK_BASE_URL")
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("missing required environment variable: TERASLACK_API_KEY")
	}
	return cfg, nil
}

type Server struct {
	cfg       Config
	logger    *slog.Logger
	debug     io.Writer
	debugFile io.Closer
	client    *teraslackmcp.Client
	mcpServer *mcp.Server

	mu                    sync.RWMutex
	auth                  *domain.AuthContext
	defaultConversationID string
	userCache             map[string]domain.User
}

type messageSummary struct {
	TS          string `json:"ts"`
	Text        string `json:"text"`
	UserID      string `json:"user_id"`
	SenderEmail string `json:"sender_email,omitempty"`
}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	client, err := teraslackmcp.NewClient(cfg.BaseURL, cfg.APIKey)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	var debug io.Writer = io.Discard
	var debugFile io.Closer
	if cfg.DebugLogPath != "" {
		file, err := os.OpenFile(cfg.DebugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			debug = file
			debugFile = file
		}
	}

	s := &Server{
		cfg:                   cfg,
		logger:                logger,
		debug:                 debug,
		debugFile:             debugFile,
		client:                client,
		defaultConversationID: cfg.DefaultConversationID,
		userCache:             map[string]domain.User{},
	}
	s.mcpServer = s.newMCPServer()
	return s, nil
}

func (s *Server) Close() error {
	if s == nil || s.debugFile == nil {
		return nil
	}
	return s.debugFile.Close()
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := io.NopCloser(in)
	if rc, ok := in.(io.ReadCloser); ok {
		reader = rc
	}

	var writer io.WriteCloser = nopWriteCloser{Writer: out}
	if wc, ok := out.(io.WriteCloser); ok {
		writer = wc
	}

	return s.mcpServer.Run(ctx, &mcp.IOTransport{
		Reader: reader,
		Writer: writer,
	})
}

func (s *Server) newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "teraslack-stdio",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Logger: s.logger,
		Instructions: "This is a local stdio MCP server for Teraslack's HTTP API. " +
			"It uses TERASLACK_API_KEY directly and is separate from the hosted OAuth MCP server. " +
			"Use whoami first to inspect the active identity. Use create_dm or set_default_conversation to establish a default conversation. " +
			"Use api_request for endpoints that do not have a dedicated tool.",
	})

	for _, spec := range s.tools() {
		name, _ := spec["name"].(string)
		description, _ := spec["description"].(string)
		server.AddTool(&mcp.Tool{
			Name:        name,
			Description: description,
			InputSchema: spec["inputSchema"],
		}, s.toolHandler(name))
	}

	return server
}

func (s *Server) tools() []map[string]any {
	return []map[string]any{
		{
			"name":        "whoami",
			"description": "Return the active Teraslack identity behind the configured API key and the current default conversation, if any.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			"name":        "search_users",
			"description": "Search users in the current workspace by id, name, display name, real name, or email.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
					"exact": map[string]any{"type": "boolean"},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "create_dm",
			"description": "Create an IM conversation with another user and optionally make it the default conversation for later tools.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_id":     map[string]any{"type": "string"},
					"set_default": map[string]any{"type": "boolean"},
				},
				"required":             []string{"user_id"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "set_default_conversation",
			"description": "Set the default conversation used by send_message, list_messages, and wait_for_message.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"conversation_id": map[string]any{"type": "string"},
					"channel_id":      map[string]any{"type": "string"},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "send_message",
			"description": "Send a message to a Teraslack conversation as the active identity. Prefer conversation_id; channel_id is accepted as an alias.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"conversation_id": map[string]any{"type": "string"},
					"channel_id":      map[string]any{"type": "string"},
					"text":            map[string]any{"type": "string"},
					"metadata":        map[string]any{"type": "object"},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_messages",
			"description": "List recent messages in a Teraslack conversation. Prefer conversation_id; channel_id is accepted as an alias.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"conversation_id": map[string]any{"type": "string"},
					"channel_id":      map[string]any{"type": "string"},
					"limit":           map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_events",
			"description": "List recent external events from the Teraslack event stream.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":          map[string]any{"type": "string"},
					"resource_type": map[string]any{"type": "string"},
					"resource_id":   map[string]any{"type": "string"},
					"limit":         map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "wait_for_event",
			"description": "Wait until a matching Teraslack external event appears for the active identity.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":             map[string]any{"type": "string"},
					"resource_type":    map[string]any{"type": "string"},
					"resource_id":      map[string]any{"type": "string"},
					"timeout_seconds":  map[string]any{"type": "integer", "minimum": 1, "maximum": 300},
					"poll_interval_ms": map[string]any{"type": "integer", "minimum": 100, "maximum": 10000},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "wait_for_message",
			"description": "Wait until a matching top-level message appears in a Teraslack conversation. Prefer conversation_id; channel_id is accepted as an alias.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"conversation_id": map[string]any{"type": "string"},
					"channel_id":      map[string]any{"type": "string"},
					"text":            map[string]any{"type": "string"},
					"contains_text":   map[string]any{"type": "string"},
					"from_email":      map[string]any{"type": "string"},
					"from_user_id":    map[string]any{"type": "string"},
					"include_self":    map[string]any{"type": "boolean"},
					"include_existing": map[string]any{
						"type": "boolean",
					},
					"timeout_seconds":  map[string]any{"type": "integer", "minimum": 1, "maximum": 300},
					"poll_interval_ms": map[string]any{"type": "integer", "minimum": 100, "maximum": 10000},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "api_request",
			"description": "Call any Teraslack HTTP API with the configured API key.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{
						"type": "string",
						"enum": []string{"GET", "POST", "PATCH", "PUT", "DELETE"},
					},
					"path":  map[string]any{"type": "string"},
					"query": map[string]any{"type": "object"},
					"body":  map[string]any{},
				},
				"required":             []string{"method", "path"},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) toolHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw, err := json.Marshal(struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments,omitempty"`
		}{
			Name:      name,
			Arguments: req.Params.Arguments,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal tool call: %w", err)
		}

		text, err := s.callTool(ctx, raw)
		if err != nil {
			result := &mcp.CallToolResult{}
			result.SetError(err)
			return result, nil
		}

		result := &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}
		var structured map[string]any
		if err := json.Unmarshal([]byte(text), &structured); err == nil {
			result.StructuredContent = structured
		}
		return result, nil
	}
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return "", fmt.Errorf("decode tool call: %w", err)
	}

	switch req.Name {
	case "whoami":
		return s.handleWhoAmI(ctx)
	case "search_users":
		return s.handleSearchUsers(ctx, req.Arguments)
	case "create_dm":
		return s.handleCreateDM(ctx, req.Arguments)
	case "set_default_conversation":
		return s.handleSetDefaultConversation(req.Arguments)
	case "send_message":
		return s.handleSendMessage(ctx, req.Arguments)
	case "list_messages":
		return s.handleListMessages(ctx, req.Arguments)
	case "list_events":
		return s.handleListEvents(ctx, req.Arguments)
	case "wait_for_event":
		return s.handleWaitForEvent(ctx, req.Arguments)
	case "wait_for_message":
		return s.handleWaitForMessage(ctx, req.Arguments)
	case "api_request":
		return s.handleAPIRequest(ctx, req.Arguments)
	default:
		return "", fmt.Errorf("unknown tool: %s", req.Name)
	}
}

func (s *Server) handleWhoAmI(ctx context.Context) (string, error) {
	auth, err := s.authContext(ctx)
	if err != nil {
		return "", err
	}
	user, err := s.userByID(ctx, auth.UserID)
	if err != nil {
		return "", err
	}

	result := map[string]any{
		"workspace_id": auth.WorkspaceID,
		"user": map[string]any{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"principal_type": user.PrincipalType,
			"is_bot":         user.IsBot,
		},
	}
	if len(auth.Permissions) > 0 {
		result["permissions"] = auth.Permissions
	}
	if len(auth.Scopes) > 0 {
		result["scopes"] = auth.Scopes
	}
	if conversationID := s.defaultConversation(); conversationID != "" {
		result["default_conversation_id"] = conversationID
	}
	return marshalToolResult(result)
}

func (s *Server) handleSearchUsers(ctx context.Context, args map[string]any) (string, error) {
	auth, err := s.authContext(ctx)
	if err != nil {
		return "", err
	}

	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := intArg(args, "limit", 20)
	exact := boolArg(args, "exact", false)

	emailFilter := ""
	if exact && strings.Contains(query, "@") {
		emailFilter = query
	}

	out := make([]map[string]any, 0, limit)
	cursor := ""
	for len(out) < limit {
		page, err := s.client.ListUsers(ctx, auth.WorkspaceID, cursor, emailFilter, 100)
		if err != nil {
			return "", err
		}
		for _, user := range page.Items {
			if emailFilter == "" && !userMatchesQuery(user, query, exact) {
				continue
			}
			out = append(out, map[string]any{
				"id":             user.ID,
				"name":           user.Name,
				"email":          user.Email,
				"principal_type": user.PrincipalType,
				"is_bot":         user.IsBot,
			})
			s.cacheUser(user)
			if len(out) >= limit {
				break
			}
		}
		if page.NextCursor == "" || len(page.Items) == 0 {
			break
		}
		cursor = page.NextCursor
	}

	return marshalToolResult(map[string]any{"users": out})
}

func (s *Server) handleCreateDM(ctx context.Context, args map[string]any) (string, error) {
	auth, err := s.authContext(ctx)
	if err != nil {
		return "", err
	}

	targetUserID := strings.TrimSpace(stringArg(args, "user_id", ""))
	if targetUserID == "" {
		return "", fmt.Errorf("user_id is required")
	}

	conv, err := s.client.CreateConversation(ctx, domain.CreateConversationParams{
		Type:      domain.ConversationTypeIM,
		CreatorID: auth.UserID,
	})
	if err != nil {
		return "", err
	}
	if _, err := s.client.InviteUsers(ctx, conv.ID, []string{targetUserID}); err != nil {
		return "", err
	}

	setDefault := boolArg(args, "set_default", true)
	if setDefault {
		s.setDefaultConversation(conv.ID)
	}

	return marshalToolResult(map[string]any{
		"conversation_id": conv.ID,
		"channel_id":      conv.ID,
		"type":            conv.Type,
		"default_set":     setDefault,
		"user_ids":        []string{auth.UserID, targetUserID},
	})
}

func (s *Server) handleSetDefaultConversation(args map[string]any) (string, error) {
	conversationID := resolveConversationID(s.defaultConversation(), args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required")
	}
	s.setDefaultConversation(conversationID)
	return marshalToolResult(map[string]any{
		"status":          "ok",
		"conversation_id": conversationID,
		"channel_id":      conversationID,
	})
}

func (s *Server) handleSendMessage(ctx context.Context, args map[string]any) (string, error) {
	auth, err := s.authContext(ctx)
	if err != nil {
		return "", err
	}

	text := strings.TrimSpace(stringArg(args, "text", ""))
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	conversationID := resolveConversationID(s.defaultConversation(), args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required unless a default conversation is already set")
	}
	metadata := mapArg(args, "metadata")

	msg, err := s.client.PostMessage(ctx, conversationID, auth.UserID, text, metadata)
	if err != nil {
		return "", err
	}
	if s.defaultConversation() == "" {
		s.setDefaultConversation(conversationID)
	}

	s.logger.Info("sent teraslack stdio message", "conversation_id", conversationID, "user_id", auth.UserID, "text", text, "ts", msg.TS)
	return marshalToolResult(map[string]any{
		"status":          "sent",
		"conversation_id": conversationID,
		"channel_id":      conversationID,
		"message":         s.summarizeMessage(ctx, *msg),
	})
}

func (s *Server) handleListMessages(ctx context.Context, args map[string]any) (string, error) {
	conversationID := resolveConversationID(s.defaultConversation(), args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required unless a default conversation is already set")
	}
	limit := intArg(args, "limit", 20)

	msgs, err := s.client.ListMessages(ctx, conversationID, limit)
	if err != nil {
		return "", err
	}

	out := make([]messageSummary, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, s.summarizeMessage(ctx, msg))
	}

	return marshalToolResult(map[string]any{
		"conversation_id": conversationID,
		"channel_id":      conversationID,
		"messages":        out,
	})
}

func (s *Server) handleListEvents(ctx context.Context, args map[string]any) (string, error) {
	events, err := s.client.ListEvents(
		ctx,
		"",
		stringArg(args, "type", ""),
		stringArg(args, "resource_type", ""),
		stringArg(args, "resource_id", ""),
		intArg(args, "limit", 20),
	)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{"events": events})
}

func (s *Server) handleWaitForEvent(ctx context.Context, args map[string]any) (string, error) {
	cursor, err := s.currentEventCursor(ctx)
	if err != nil {
		return "", err
	}

	eventType := strings.TrimSpace(stringArg(args, "type", ""))
	resourceType := strings.TrimSpace(stringArg(args, "resource_type", ""))
	resourceID := strings.TrimSpace(stringArg(args, "resource_id", ""))
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond
	deadline := time.Now().Add(timeout)

	for {
		page, err := s.client.ListEventPage(ctx, cursor, eventType, resourceType, resourceID, 50)
		if err != nil {
			return "", err
		}
		if len(page.Items) > 0 {
			return marshalToolResult(map[string]any{
				"status": "received",
				"event":  page.Items[0],
			})
		}
		if page.NextCursor != "" {
			cursor = page.NextCursor
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for matching event after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (s *Server) handleWaitForMessage(ctx context.Context, args map[string]any) (string, error) {
	auth, err := s.authContext(ctx)
	if err != nil {
		return "", err
	}

	conversationID := resolveConversationID(s.defaultConversation(), args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required unless a default conversation is already set")
	}
	wantText := stringArg(args, "text", "")
	wantContains := stringArg(args, "contains_text", "")
	wantEmail := stringArg(args, "from_email", "")
	wantUserID := stringArg(args, "from_user_id", "")
	includeSelf := boolArg(args, "include_self", false)
	includeExisting := boolArg(args, "include_existing", false)
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond

	afterTS := ""
	if !includeExisting {
		afterTS, err = s.currentTopLevelMessageTS(ctx, conversationID)
		if err != nil {
			return "", err
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		msgs, err := s.client.ListMessages(ctx, conversationID, 50)
		if err != nil {
			return "", err
		}
		for _, msg := range msgs {
			summary := s.summarizeMessage(ctx, msg)
			if !includeExisting && compareMessageTS(summary.TS, afterTS) <= 0 {
				continue
			}
			if !includeSelf && summary.UserID == auth.UserID {
				continue
			}
			if msg.IsDeleted || msg.ThreadTS != nil {
				continue
			}
			if wantText != "" && summary.Text != wantText {
				continue
			}
			if wantContains != "" && !strings.Contains(summary.Text, wantContains) {
				continue
			}
			if wantEmail != "" && summary.SenderEmail != wantEmail {
				continue
			}
			if wantUserID != "" && summary.UserID != wantUserID {
				continue
			}
			return marshalToolResult(map[string]any{
				"status":          "received",
				"conversation_id": conversationID,
				"channel_id":      conversationID,
				"message":         summary,
			})
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for matching message after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (s *Server) handleAPIRequest(ctx context.Context, args map[string]any) (string, error) {
	method := strings.ToUpper(strings.TrimSpace(stringArg(args, "method", "")))
	if method == "" {
		return "", fmt.Errorf("method is required")
	}
	path := strings.TrimSpace(stringArg(args, "path", ""))
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	query := mapArg(args, "query")
	body, hasBody := args["body"]
	if !hasBody {
		body = nil
	}

	result, err := s.client.Request(ctx, method, path, query, body)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{
		"method": method,
		"path":   path,
		"result": result,
	})
}

func (s *Server) authContext(ctx context.Context) (*domain.AuthContext, error) {
	s.mu.RLock()
	if s.auth != nil {
		authCopy := *s.auth
		s.mu.RUnlock()
		return &authCopy, nil
	}
	s.mu.RUnlock()

	auth, err := s.client.AuthMe(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.auth = auth
	s.mu.Unlock()
	return auth, nil
}

func (s *Server) currentEventCursor(ctx context.Context) (string, error) {
	page, err := s.client.ListEventPage(ctx, "", "", "", "", 1)
	if err != nil {
		return "", err
	}
	return page.NextCursor, nil
}

func (s *Server) currentTopLevelMessageTS(ctx context.Context, conversationID string) (string, error) {
	msgs, err := s.client.ListMessages(ctx, conversationID, 50)
	if err != nil {
		return "", err
	}
	latest := ""
	for _, msg := range msgs {
		if msg.IsDeleted || msg.ThreadTS != nil {
			continue
		}
		if compareMessageTS(msg.TS, latest) > 0 {
			latest = msg.TS
		}
	}
	return latest, nil
}

func (s *Server) summarizeMessage(ctx context.Context, msg domain.Message) messageSummary {
	return messageSummary{
		TS:          msg.TS,
		Text:        msg.Text,
		UserID:      msg.UserID,
		SenderEmail: s.emailForUserID(ctx, msg.UserID),
	}
}

func (s *Server) emailForUserID(ctx context.Context, userID string) string {
	if userID == "" {
		return ""
	}
	user, err := s.userByID(ctx, userID)
	if err != nil {
		s.debugf("resolve user email failed user_id=%s err=%v", userID, err)
		return ""
	}
	return user.Email
}

func (s *Server) userByID(ctx context.Context, userID string) (domain.User, error) {
	s.mu.RLock()
	user, ok := s.userCache[userID]
	s.mu.RUnlock()
	if ok {
		return user, nil
	}

	found, err := s.client.GetUser(ctx, s.auth.WorkspaceID, userID)
	if err != nil {
		return domain.User{}, err
	}
	s.cacheUser(*found)
	return *found, nil
}

func (s *Server) cacheUser(user domain.User) {
	if user.ID == "" {
		return
	}
	s.mu.Lock()
	s.userCache[user.ID] = user
	s.mu.Unlock()
}

func (s *Server) defaultConversation() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defaultConversationID
}

func (s *Server) setDefaultConversation(conversationID string) {
	s.mu.Lock()
	s.defaultConversationID = strings.TrimSpace(conversationID)
	s.mu.Unlock()
}

func (s *Server) debugf(format string, args ...any) {
	if s.debug == nil {
		return
	}
	fmt.Fprintf(s.debug, "%s ", time.Now().Format(time.RFC3339Nano))
	fmt.Fprintf(s.debug, format, args...)
	fmt.Fprintln(s.debug)
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func marshalToolResult(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	return string(data), nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveConversationID(defaultConversationID string, args map[string]any) string {
	conversationID := strings.TrimSpace(stringArg(args, "conversation_id", ""))
	if conversationID != "" {
		return conversationID
	}
	channelID := strings.TrimSpace(stringArg(args, "channel_id", ""))
	if channelID != "" {
		return channelID
	}
	return strings.TrimSpace(defaultConversationID)
}

func userMatchesQuery(user domain.User, query string, exact bool) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	candidates := []string{user.ID, user.Name, user.Email, user.RealName, user.DisplayName}
	if exact {
		for _, candidate := range candidates {
			if strings.EqualFold(strings.TrimSpace(candidate), query) {
				return true
			}
		}
		return false
	}
	query = strings.ToLower(query)
	for _, candidate := range candidates {
		if strings.Contains(strings.ToLower(candidate), query) {
			return true
		}
	}
	return false
}

func stringArg(args map[string]any, key, fallback string) string {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func intArg(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return int(n)
		}
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func boolArg(args map[string]any, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func mapArg(args map[string]any, key string) map[string]any {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return values
}

func compareMessageTS(left, right string) int {
	if left == right {
		return 0
	}
	leftMajor, leftMinor, leftOK := splitMessageTS(left)
	rightMajor, rightMinor, rightOK := splitMessageTS(right)
	if leftOK && rightOK {
		switch {
		case leftMajor < rightMajor:
			return -1
		case leftMajor > rightMajor:
			return 1
		case leftMinor < rightMinor:
			return -1
		case leftMinor > rightMinor:
			return 1
		default:
			return 0
		}
	}
	if left < right {
		return -1
	}
	return 1
}

func splitMessageTS(value string) (int64, int64, bool) {
	if value == "" {
		return 0, 0, true
	}
	parts := strings.SplitN(value, ".", 2)
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if len(parts) == 1 {
		return major, 0, true
	}
	minor, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}
