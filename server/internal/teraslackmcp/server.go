package teraslackmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/suhjohn/teraslack/internal/domain"
)

type Config struct {
	BaseURL         string
	APIKey          string
	BootstrapToken  string
	MCPBaseURL      string
	OAuthIssuer     string
	OAuthSigningKey string
	KeepAlive       time.Duration
	SSEHeartbeat    time.Duration
	WorkspaceID     string
	UserID          string
	UserName        string
	UserEmail       string
	Permissions     []string
	OAuthScopes     []string
	PeerUserID      string
	PeerUserName    string
	PeerUserEmail   string
	ChannelID       string
	DebugLogPath    string
}

func LoadConfigFromEnv() (Config, error) {
	keepAlive := time.Duration(0)
	if raw := strings.TrimSpace(os.Getenv("MCP_KEEPALIVE_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 0 {
			return Config{}, fmt.Errorf("invalid MCP_KEEPALIVE_SECONDS %q", raw)
		}
		keepAlive = time.Duration(seconds) * time.Second
	}

	sseHeartbeat := 25 * time.Second
	if raw := strings.TrimSpace(os.Getenv("MCP_SSE_HEARTBEAT_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 0 {
			return Config{}, fmt.Errorf("invalid MCP_SSE_HEARTBEAT_SECONDS %q", raw)
		}
		sseHeartbeat = time.Duration(seconds) * time.Second
	}

	cfg := Config{
		BaseURL:         strings.TrimSpace(os.Getenv("TERASLACK_BASE_URL")),
		APIKey:          strings.TrimSpace(os.Getenv("TERASLACK_API_KEY")),
		BootstrapToken:  strings.TrimSpace(os.Getenv("TERASLACK_SYSTEM_API_KEY")),
		MCPBaseURL:      strings.TrimSpace(os.Getenv("MCP_BASE_URL")),
		OAuthIssuer:     strings.TrimSpace(firstNonEmptyEnv("MCP_OAUTH_ISSUER", "TERASLACK_BASE_URL")),
		OAuthSigningKey: strings.TrimSpace(firstNonEmptyEnv("MCP_OAUTH_SIGNING_KEY", "ENCRYPTION_KEY")),
		KeepAlive:       keepAlive,
		SSEHeartbeat:    sseHeartbeat,
		WorkspaceID:     strings.TrimSpace(os.Getenv("TERASLACK_WORKSPACE_ID")),
		UserID:          strings.TrimSpace(os.Getenv("TERASLACK_USER_ID")),
		UserName:        strings.TrimSpace(os.Getenv("TERASLACK_USER_NAME")),
		UserEmail:       strings.TrimSpace(os.Getenv("TERASLACK_USER_EMAIL")),
		PeerUserID:      strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_ID")),
		PeerUserName:    strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_NAME")),
		PeerUserEmail:   strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_EMAIL")),
		ChannelID:       strings.TrimSpace(os.Getenv("TERASLACK_CHANNEL_ID")),
		DebugLogPath:    strings.TrimSpace(os.Getenv("TERASLACK_MCP_DEBUG_LOG")),
	}

	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("missing required environment variable: TERASLACK_BASE_URL")
	}
	return cfg, nil
}

type sessionState struct {
	Token         string
	WorkspaceID   string
	UserID        string
	UserName      string
	UserEmail     string
	Permissions   []string
	OAuthScopes   []string
	PeerUserID    string
	PeerUserName  string
	PeerUserEmail string
	ChannelID     string
	client        *Client
}

type sessionData struct {
	owner              sessionState
	current            sessionState
	sessionIdentity    sessionState
	clientSessionID    string
	provisionedSession string
	autoProvisionErr   error
	provisionMu        sync.Mutex
	nextSubscriptionID int64
	subscriptions      map[string]conversationSubscription
	lastAccess         time.Time
}

type Server struct {
	cfg             Config
	logger          *slog.Logger
	debug           io.Writer
	bootstrapClient *Client
	initial         sessionState
	sdkServer       *mcp.Server
	httpHandler     http.Handler
	sessionTTL      time.Duration
	channelCancel   context.CancelFunc

	mu       sync.RWMutex
	sessions map[string]*sessionData
}

type conversationSubscription struct {
	ID        string
	ChannelID string
	Cursor    string
	State     sessionState
}

type sessionContextKey struct{}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	var debug io.Writer = io.Discard
	if cfg.DebugLogPath != "" {
		f, err := os.OpenFile(cfg.DebugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			debug = f
		}
	}

	initial := sessionState{
		Token:         cfg.APIKey,
		WorkspaceID:   cfg.WorkspaceID,
		UserID:        cfg.UserID,
		UserName:      cfg.UserName,
		UserEmail:     cfg.UserEmail,
		Permissions:   append([]string(nil), cfg.Permissions...),
		OAuthScopes:   append([]string(nil), cfg.OAuthScopes...),
		PeerUserID:    cfg.PeerUserID,
		PeerUserName:  cfg.PeerUserName,
		PeerUserEmail: cfg.PeerUserEmail,
		ChannelID:     cfg.ChannelID,
	}
	if cfg.APIKey != "" {
		client, err := NewClient(cfg.BaseURL, cfg.APIKey)
		if err != nil {
			return nil, err
		}
		initial.client = client
	}

	var bootstrapClient *Client
	bootstrapToken := strings.TrimSpace(cfg.BootstrapToken)
	if bootstrapToken != "" {
		client, err := NewClient(cfg.BaseURL, bootstrapToken)
		if err != nil {
			return nil, err
		}
		bootstrapClient = client
	}

	srv := &Server{
		cfg:             cfg,
		logger:          logger,
		debug:           debug,
		bootstrapClient: bootstrapClient,
		initial:         initial,
		sessionTTL:      30 * time.Minute,
		sessions:        map[string]*sessionData{},
	}
	srv.sdkServer = srv.newMCPServer()
	baseHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv.sdkServer
	}, &mcp.StreamableHTTPOptions{
		Logger:         logger,
		EventStore:     streamableHTTPEventStore,
		SessionTimeout: srv.sessionTTL,
	})
	srv.httpHandler = withSSEHeartbeat(baseHandler, srv.cfg.SSEHeartbeat)

	// Start channel polling loop for HTTP transport (stdio starts it in Serve).
	ctx, cancel := context.WithCancel(context.Background())
	srv.channelCancel = cancel
	go srv.startChannelLoop(ctx)

	return srv, nil
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

	return s.sdkServer.Run(ctx, &mcp.IOTransport{
		Reader: reader,
		Writer: writer,
	})
}

func (s *Server) HTTPHandler() http.Handler {
	return s.httpHandler
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func (s *Server) newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "teraslack-mcp",
		Version: "0.3.0",
	}, &mcp.ServerOptions{
		Logger:    s.logger,
		KeepAlive: s.cfg.KeepAlive,
		InitializedHandler: func(ctx context.Context, req *mcp.InitializedRequest) {
			if err := setServerSessionLogLevelIfEmpty(req.Session, "info"); err != nil {
				s.logger.Warn("mcp: failed to set default log level", "error", err)
			}
		},
		Capabilities: &mcp.ServerCapabilities{
			Logging: &mcp.LoggingCapabilities{},
		},
		Instructions: "Teraslack may stream incoming conversation messages as MCP logging notifications (notifications/message). " +
			"For OAuth-backed remote MCP, call whoami with a client session_id (unique per Claude/Codex run) to provision a per-client session agent. " +
			"Streaming requires a conversation ID: set a default conversation for this MCP session using create_dm or send_message, or set TERASLACK_CHANNEL_ID on the server. " +
			"To respond, use send_message with the conversation_id from the notification metadata.",
	})

	for _, spec := range s.tools() {
		name, _ := spec["name"].(string)
		if name == "" {
			continue
		}
		description, _ := spec["description"].(string)
		server.AddTool(&mcp.Tool{
			Name:        name,
			Description: description,
			InputSchema: spec["inputSchema"],
		}, s.mcpToolHandler(name))
	}

	return server
}

func (s *Server) mcpToolHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx = context.WithValue(ctx, sessionContextKey{}, req.Session)
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

		resultText, err := s.callTool(ctx, raw)
		if err != nil {
			result := &mcp.CallToolResult{}
			result.SetError(err)
			return result, nil
		}

		result := &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: resultText},
			},
		}

		var structured map[string]any
		if err := json.Unmarshal([]byte(resultText), &structured); err == nil {
			result.StructuredContent = structured
		}

		return result, nil
	}
}

func (s *Server) tools() []map[string]any {
	return []map[string]any{
		{
			"name":        "register",
			"description": "Create or reuse a Teraslack agent identity by name using the configured system API key, issue a scoped API key for it, and switch this MCP session to act as that identity.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
					"email": map[string]any{
						"type": "string",
					},
					"owner_id": map[string]any{
						"type": "string",
					},
					"principal_type": map[string]any{
						"type": "string",
						"enum": []string{"agent", "system", "human"},
					},
					"is_bot": map[string]any{
						"type": "boolean",
					},
					"permissions": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
					"api_key_name": map[string]any{
						"type": "string",
					},
					"expires_in": map[string]any{
						"type": "string",
					},
				},
				"required":             []string{"name"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "whoami",
			"description": "Return the active Teraslack identity. For OAuth-backed remote MCP, pass a client session_id to create or reuse a per-client session agent owned by the approving human (session_id should be unique per Claude/Codex session).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{
						"type":        "string",
						"description": "Client-provided session identifier (unique per Claude/Codex run). Required for OAuth-backed remote MCP to provision a session agent.",
					},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_owned_identities",
			"description": "List Teraslack agent identities owned by the approving human for this MCP session.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			"name":        "switch_identity",
			"description": "Switch this MCP session to an existing owned Teraslack agent identity by user_id, name, or email.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_id": map[string]any{
						"type": "string",
					},
					"name": map[string]any{
						"type": "string",
					},
					"email": map[string]any{
						"type": "string",
					},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "reset_identity",
			"description": "Reset this MCP session back to its session agent identity (the one provisioned via whoami with session_id).",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			"name":        "search_users",
			"description": "Search users in the current Teraslack workspace by id, name, display name, real name, or email.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type": "string",
					},
					"limit": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 100,
					},
					"exact": map[string]any{
						"type": "boolean",
					},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "create_dm",
			"description": "Create an IM conversation with another user, invite them, and optionally set it as the default conversation for later message tools.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_id": map[string]any{
						"type": "string",
					},
					"set_default": map[string]any{
						"type": "boolean",
					},
				},
				"required":             []string{"user_id"},
				"additionalProperties": false,
			},
		},
			{
				"name":        "send_message",
				"description": "Send a message to a Teraslack conversation as the active identity. Prefer conversation_id; channel_id is accepted as an alias.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"conversation_id": map[string]any{
							"type": "string",
						},
						"channel_id": map[string]any{
							"type": "string",
						},
						"text": map[string]any{
						"type": "string",
					},
					"metadata": map[string]any{
						"type": "object",
					},
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
						"conversation_id": map[string]any{
							"type": "string",
						},
						"channel_id": map[string]any{
							"type": "string",
						},
						"limit": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 100,
					},
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
					"type": map[string]any{
						"type": "string",
					},
					"resource_type": map[string]any{
						"type": "string",
					},
					"resource_id": map[string]any{
						"type": "string",
					},
					"timeout_seconds": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 300,
					},
					"poll_interval_ms": map[string]any{
						"type":    "integer",
						"minimum": 100,
						"maximum": 10000,
					},
				},
				"additionalProperties": false,
			},
		},
			{
				"name":        "subscribe_conversation",
				"description": "Create a future-only event subscription for a Teraslack conversation and return a cursor-backed subscription id. Prefer conversation_id; channel_id is accepted as an alias.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"conversation_id": map[string]any{
							"type": "string",
						},
						"channel_id": map[string]any{
							"type": "string",
						},
					},
					"additionalProperties": false,
			},
		},
		{
			"name":        "next_event",
			"description": "Wait for the next matching event on a previously subscribed Teraslack conversation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subscription_id": map[string]any{
						"type": "string",
					},
					"event_type": map[string]any{
						"type": "string",
					},
					"text": map[string]any{
						"type": "string",
					},
					"contains_text": map[string]any{
						"type": "string",
					},
					"from_email": map[string]any{
						"type": "string",
					},
					"from_user_id": map[string]any{
						"type": "string",
					},
					"include_self": map[string]any{
						"type": "boolean",
					},
					"timeout_seconds": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 300,
					},
					"poll_interval_ms": map[string]any{
						"type":    "integer",
						"minimum": 100,
						"maximum": 10000,
					},
				},
				"required":             []string{"subscription_id"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "api_request",
			"description": "Call any Teraslack HTTP API over MCP. By default it uses the active identity; set auth_scope to bootstrap to use the system API key instead.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{
						"type": "string",
						"enum": []string{"GET", "POST", "PATCH", "PUT", "DELETE"},
					},
					"path": map[string]any{
						"type": "string",
					},
					"query": map[string]any{
						"type": "object",
					},
					"body": map[string]any{},
					"auth_scope": map[string]any{
						"type": "string",
						"enum": []string{"current", "bootstrap"},
					},
				},
				"required":             []string{"method", "path"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_events",
			"description": "List recent external events from the Teraslack event stream.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type": map[string]any{
						"type": "string",
					},
					"resource_type": map[string]any{
						"type": "string",
					},
					"resource_id": map[string]any{
						"type": "string",
					},
					"limit": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 100,
					},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "wait_for_events",
			"description": "Wait until a matching external event appears from the Teraslack event stream.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type": map[string]any{
						"type": "string",
					},
					"resource_type": map[string]any{
						"type": "string",
					},
					"resource_id": map[string]any{
						"type": "string",
					},
					"timeout_seconds": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 300,
					},
					"poll_interval_ms": map[string]any{
						"type":    "integer",
						"minimum": 100,
						"maximum": 10000,
					},
				},
				"additionalProperties": false,
			},
		},
			{
				"name":        "wait_for_message",
				"description": "Wait until a matching message appears in a Teraslack conversation. Prefer conversation_id; channel_id is accepted as an alias.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"conversation_id": map[string]any{
							"type": "string",
						},
						"channel_id": map[string]any{
							"type": "string",
						},
						"text": map[string]any{
						"type": "string",
					},
					"contains_text": map[string]any{
						"type": "string",
					},
					"from_email": map[string]any{
						"type": "string",
					},
					"from_user_id": map[string]any{
						"type": "string",
					},
					"include_existing": map[string]any{
						"type": "boolean",
					},
					"timeout_seconds": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 300,
					},
					"poll_interval_ms": map[string]any{
						"type":    "integer",
						"minimum": 100,
						"maximum": 10000,
					},
				},
				"additionalProperties": false,
			},
		},
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
	case "register":
		return s.handleRegister(ctx, req.Arguments)
	case "whoami":
		return s.handleWhoAmI(ctx, req.Arguments)
	case "list_owned_identities":
		return s.handleListOwnedIdentities(ctx)
	case "switch_identity":
		return s.handleSwitchIdentity(ctx, req.Arguments)
	case "reset_identity":
		return s.handleResetIdentity(ctx)
	case "search_users":
		return s.handleSearchUsers(ctx, req.Arguments)
	case "create_dm":
		return s.handleCreateDM(ctx, req.Arguments)
	case "send_message":
		return s.handleSendMessage(ctx, req.Arguments)
	case "list_messages":
		return s.handleListMessages(ctx, req.Arguments)
	case "wait_for_event":
		return s.handleWaitForEvent(ctx, req.Arguments)
	case "subscribe_conversation":
		return s.handleSubscribeConversation(ctx, req.Arguments)
	case "next_event":
		return s.handleNextEvent(ctx, req.Arguments)
	case "api_request":
		return s.handleAPIRequest(ctx, req.Arguments)
	case "list_events":
		return s.handleListEvents(ctx, req.Arguments)
	case "wait_for_events":
		return s.handleWaitForEvents(ctx, req.Arguments)
	case "wait_for_message":
		return s.handleWaitForMessage(ctx, req.Arguments)
	default:
		return "", fmt.Errorf("unknown tool %q", req.Name)
	}
}

func (s *Server) handleRegister(ctx context.Context, args map[string]any) (string, error) {
	owner, err := s.ownerState(ctx)
	if err != nil {
		return "", err
	}
	if len(owner.OAuthScopes) > 0 {
		return "", fmt.Errorf("register is unavailable for OAuth-backed MCP sessions; a session identity is provisioned automatically")
	}
	bootstrap := s.bootstrap()
	if bootstrap == nil {
		return "", fmt.Errorf("register requires a configured system API key")
	}
	current, err := s.currentState(ctx)
	if err != nil {
		return "", err
	}

	name := strings.TrimSpace(stringArg(args, "name", ""))
	if name == "" {
		return "", fmt.Errorf("name is required")
	}

	email := strings.TrimSpace(stringArg(args, "email", ""))
	if email == "" {
		email = defaultAgentEmail(name)
	}

	principalType := domain.PrincipalType(strings.TrimSpace(stringArg(args, "principal_type", string(domain.PrincipalTypeAgent))))
	switch principalType {
	case domain.PrincipalTypeAgent, domain.PrincipalTypeSystem, domain.PrincipalTypeHuman:
	default:
		return "", fmt.Errorf("principal_type must be one of agent, system, or human")
	}

	user, found, err := s.findUserByIdentity(ctx, bootstrap, name, email)
	if err != nil {
		return "", err
	}
	if !found {
		user, err = bootstrap.CreateUser(ctx, domain.CreateUserParams{
			Name:          name,
			Email:         email,
			OwnerID:       strings.TrimSpace(stringArg(args, "owner_id", "")),
			PrincipalType: principalType,
			IsBot:         boolArg(args, "is_bot", principalType != domain.PrincipalTypeHuman),
		})
		if err != nil {
			return "", err
		}
	}

	permissions := stringSliceArg(args, "permissions")
	if len(permissions) == 0 {
		permissions = []string{"*"}
	}
	apiKeyName := strings.TrimSpace(stringArg(args, "api_key_name", ""))
	if apiKeyName == "" {
		apiKeyName = fmt.Sprintf("%s MCP key", user.Name)
	}

	key, secret, err := bootstrap.CreateAPIKey(ctx, domain.CreateAPIKeyParams{
		Name:        apiKeyName,
		UserID:      user.ID,
		Permissions: permissions,
		ExpiresIn:   strings.TrimSpace(stringArg(args, "expires_in", "")),
	})
	if err != nil {
		return "", err
	}

	client, err := NewClient(s.cfg.BaseURL, secret)
	if err != nil {
		return "", err
	}

	current.Token = secret
	current.WorkspaceID = firstNonEmpty(key.WorkspaceID, user.WorkspaceID, current.WorkspaceID, s.cfg.WorkspaceID)
	current.UserID = user.ID
	current.UserName = user.Name
	current.UserEmail = user.Email
	current.ChannelID = ""
	current.client = client
	s.setCurrentState(ctx, current)

	return marshalToolResult(map[string]any{
		"status": "registered",
		"user": map[string]any{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"principal_type": user.PrincipalType,
			"is_bot":         user.IsBot,
		},
		"api_key": map[string]any{
			"id":           key.ID,
			"workspace_id": key.WorkspaceID,
			"user_id":      key.UserID,
			"permissions":  key.Permissions,
		},
	})
}

func (s *Server) handleWhoAmI(ctx context.Context, args map[string]any) (string, error) {
	owner, err := s.ownerState(ctx)
	if err != nil {
		return "", err
	}
	data := s.sessionDataForContext(ctx)

	clientSessionID := strings.TrimSpace(stringArg(args, "session_id", ""))
	mcpSessionID := strings.TrimSpace(s.sessionKeyFromContext(ctx))

	// For OAuth-backed MCP sessions, the owner identity is the approving human.
	// We only provision/use a session agent once the client supplies a session_id.
	if len(owner.OAuthScopes) > 0 && clientSessionID == "" {
		result := map[string]any{
			"registered":          owner.client != nil,
			"bootstrap_available": false,
			"identity_mode":       "owner",
			"mcp_session_id":      mcpSessionID,
			"session_agent_ready": false,
		}
		if owner.WorkspaceID != "" {
			result["workspace_id"] = owner.WorkspaceID
		}
		if owner.UserID != "" {
			result["user"] = map[string]any{
				"id":    owner.UserID,
				"name":  owner.UserName,
				"email": owner.UserEmail,
			}
			result["owner"] = map[string]any{
				"id":    owner.UserID,
				"name":  owner.UserName,
				"email": owner.UserEmail,
			}
		}
		if len(owner.Permissions) > 0 {
			result["permissions"] = owner.Permissions
		}
		if len(owner.OAuthScopes) > 0 {
			result["scopes"] = owner.OAuthScopes
		}
		return marshalToolResult(result)
	}

	if len(owner.OAuthScopes) > 0 && clientSessionID != "" {
		s.setClientSessionID(ctx, clientSessionID)
	}

	current, err := s.currentState(ctx)
	if err != nil {
		return "", err
	}

	result := map[string]any{
		"registered":          current.client != nil,
		"bootstrap_available": s.bootstrapAvailable(owner),
		"mcp_session_id":      mcpSessionID,
	}
	if clientSessionID != "" {
		result["client_session_id"] = clientSessionID
	}
	if current.WorkspaceID != "" {
		result["workspace_id"] = current.WorkspaceID
	}
	if current.client != nil {
		result["user"] = map[string]any{
			"id":    current.UserID,
			"name":  current.UserName,
			"email": current.UserEmail,
		}
	}
	if owner.UserID != "" {
		result["owner"] = map[string]any{
			"id":    owner.UserID,
			"name":  owner.UserName,
			"email": owner.UserEmail,
		}
	}
	if data.sessionIdentity.UserID != "" {
		result["session_identity"] = map[string]any{
			"id":    data.sessionIdentity.UserID,
			"name":  data.sessionIdentity.UserName,
			"email": data.sessionIdentity.UserEmail,
		}
	}
	switch {
	case current.UserID != "" && current.UserID == data.sessionIdentity.UserID:
		result["identity_mode"] = "session"
	case current.UserID != "" && current.UserID == owner.UserID:
		result["identity_mode"] = "owner"
	default:
		result["identity_mode"] = "switched"
	}
	if len(current.Permissions) > 0 {
		result["permissions"] = current.Permissions
	}
	if len(current.OAuthScopes) > 0 {
		result["scopes"] = current.OAuthScopes
	}
	if current.PeerUserID != "" || current.PeerUserName != "" || current.PeerUserEmail != "" {
		result["peer"] = map[string]any{
			"id":    current.PeerUserID,
			"name":  current.PeerUserName,
			"email": current.PeerUserEmail,
		}
	}
	if current.ChannelID != "" {
		result["conversation"] = map[string]any{
			"id": current.ChannelID,
		}
	}

	if len(owner.OAuthScopes) > 0 {
		result["session_agent_ready"] = data.sessionIdentity.client != nil && data.sessionIdentity.UserID != ""
	}
	return marshalToolResult(result)
}

func (s *Server) handleListOwnedIdentities(ctx context.Context) (string, error) {
	owner, err := s.ownerState(ctx)
	if err != nil {
		return "", err
	}
	users, err := s.listOwnedAgentUsers(ctx, owner)
	if err != nil {
		return "", err
	}
	data := s.sessionDataForContext(ctx)
	out := make([]map[string]any, 0, len(users))
	for _, user := range users {
		out = append(out, map[string]any{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"owner_id":       user.OwnerID,
			"principal_type": user.PrincipalType,
			"is_bot":         user.IsBot,
			"current":        user.ID == data.current.UserID,
			"session":        user.ID == data.sessionIdentity.UserID,
		})
	}
	return marshalToolResult(map[string]any{"identities": out})
}

func (s *Server) handleSwitchIdentity(ctx context.Context, args map[string]any) (string, error) {
	owner, err := s.ownerState(ctx)
	if err != nil {
		return "", err
	}
	user, err := s.resolveOwnedAgentUser(ctx, owner, args)
	if err != nil {
		return "", err
	}
	state, err := s.issueSessionStateForUser(ctx, owner, user, fmt.Sprintf("%s shared MCP key", user.Name))
	if err != nil {
		return "", err
	}
	s.setCurrentState(ctx, state)
	return marshalToolResult(map[string]any{
		"status": "switched",
		"user": map[string]any{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"principal_type": user.PrincipalType,
			"is_bot":         user.IsBot,
		},
	})
}

func (s *Server) handleResetIdentity(ctx context.Context) (string, error) {
	data := s.sessionDataForContext(ctx)
	if err := s.ensureSessionIdentity(ctx, data); err != nil {
		return "", err
	}
	if data.sessionIdentity.client == nil || data.sessionIdentity.UserID == "" {
		return "", fmt.Errorf("no session identity is configured for this MCP session")
	}
	s.setCurrentState(ctx, data.sessionIdentity)
	return marshalToolResult(map[string]any{
		"status": "reset",
		"user": map[string]any{
			"id":    data.sessionIdentity.UserID,
			"name":  data.sessionIdentity.UserName,
			"email": data.sessionIdentity.UserEmail,
		},
	})
}

func (s *Server) handleSearchUsers(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := intArg(args, "limit", 20)
	exact := boolArg(args, "exact", false)

	// Use API email filter when the query looks like an email and exact match is requested.
	emailFilter := ""
	if exact && strings.Contains(query, "@") {
		emailFilter = query
	}

	var out []map[string]any
	cursor := ""
	for len(out) < limit {
		page, err := current.client.ListUsers(ctx, current.WorkspaceID, cursor, emailFilter, 100)
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
			if len(out) >= limit {
				break
			}
		}
		if page.NextCursor == "" || len(page.Items) == 0 {
			break
		}
		cursor = page.NextCursor
	}

	return marshalToolResult(map[string]any{
		"users": out,
	})
}

func (s *Server) handleCreateDM(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	targetUserID := strings.TrimSpace(stringArg(args, "user_id", ""))
	if targetUserID == "" {
		return "", fmt.Errorf("user_id is required")
	}

	conv, err := current.client.CreateConversation(ctx, domain.CreateConversationParams{
		Type:      domain.ConversationTypeIM,
		CreatorID: current.UserID,
	})
	if err != nil {
		return "", err
	}
	if _, err := current.client.InviteUsers(ctx, conv.ID, []string{targetUserID}); err != nil {
		return "", err
	}

	setDefault := boolArg(args, "set_default", true)
	if setDefault {
		current.ChannelID = conv.ID
		s.setCurrentState(ctx, current)
	}

	return marshalToolResult(map[string]any{
		"conversation_id": conv.ID,
		"type":            conv.Type,
		"default_set":     setDefault,
		"user_ids":        []string{current.UserID, targetUserID},
	})
}

func (s *Server) handleSendMessage(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	text := strings.TrimSpace(stringArg(args, "text", ""))
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	conversationID := s.resolveConversationID(current, args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required unless a default conversation is already set")
	}

	metadata := mapArg(args, "metadata")
	msg, err := current.client.PostMessage(ctx, conversationID, current.UserID, text, metadata)
	if err != nil {
		return "", err
	}
	if current.ChannelID == "" {
		current.ChannelID = conversationID
		s.setCurrentState(ctx, current)
	}

	s.logger.Info("sent teraslack message", "conversation_id", conversationID, "user_email", current.UserEmail, "text", text, "ts", msg.TS)
	return marshalToolResult(map[string]any{
		"status": "sent",
		"message": messageSummary{
			TS:          msg.TS,
			Text:        msg.Text,
			UserID:      msg.UserID,
			SenderEmail: s.emailForUserID(current, msg.UserID),
		},
		"conversation_id": conversationID,
		"channel_id":      conversationID,
	})
}

func (s *Server) handleListMessages(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	conversationID := s.resolveConversationID(current, args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required unless a default conversation is already set")
	}
	limit := intArg(args, "limit", 20)
	msgs, err := current.client.ListMessages(ctx, conversationID, limit)
	if err != nil {
		return "", err
	}
	out := make([]messageSummary, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, s.summarizeMessage(current, msg))
	}
	return marshalToolResult(map[string]any{
		"conversation_id": conversationID,
		"channel_id":      conversationID,
		"messages":        out,
	})
}

func (s *Server) handleWaitForEvent(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	eventType := strings.TrimSpace(stringArg(args, "type", ""))
	resourceType := strings.TrimSpace(stringArg(args, "resource_type", ""))
	resourceID := strings.TrimSpace(stringArg(args, "resource_id", ""))
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond

	cursor, err := s.currentEventCursor(ctx, current.client)
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(timeout)
	for {
		page, err := current.client.ListEventPage(ctx, cursor, eventType, resourceType, resourceID, 50)
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

func (s *Server) handleSubscribeConversation(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	conversationID := s.resolveConversationID(current, args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required unless a default conversation is already set")
	}

	cursor, err := s.currentEventCursor(ctx, current.client)
	if err != nil {
		return "", err
	}

	subscriptionID := s.createConversationSubscription(ctx, current, conversationID, cursor)
	return marshalToolResult(map[string]any{
		"status":          "subscribed",
		"subscription_id": subscriptionID,
		"conversation_id": conversationID,
		"channel_id":      conversationID,
		"after_event_id":  cursor,
	})
}

func (s *Server) handleNextEvent(ctx context.Context, args map[string]any) (string, error) {
	subscriptionID := strings.TrimSpace(stringArg(args, "subscription_id", ""))
	if subscriptionID == "" {
		return "", fmt.Errorf("subscription_id is required")
	}

	subscription, ok := s.conversationSubscription(ctx, subscriptionID)
	if !ok {
		return "", fmt.Errorf("unknown subscription %q", subscriptionID)
	}

	eventType := strings.TrimSpace(stringArg(args, "event_type", ""))
	wantText := stringArg(args, "text", "")
	wantContains := stringArg(args, "contains_text", "")
	wantEmail := stringArg(args, "from_email", "")
	wantUserID := stringArg(args, "from_user_id", "")
	includeSelf := boolArg(args, "include_self", false)
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond

	deadline := time.Now().Add(timeout)
	cursor := subscription.Cursor

	for {
		page, err := subscription.State.client.ListEventPage(ctx, cursor, "", domain.ResourceTypeConversation, subscription.ChannelID, 1)
		if err != nil {
			return "", err
		}
		for _, event := range page.Items {
			if page.NextCursor != "" {
				cursor = page.NextCursor
				s.updateConversationSubscriptionCursor(ctx, subscriptionID, cursor)
			}

			if !s.conversationEventMatches(subscription.State, event, eventType, wantUserID, wantEmail, wantText, wantContains, includeSelf) {
				continue
			}

				result := map[string]any{
					"status":          "received",
					"subscription_id": subscriptionID,
					"conversation_id": subscription.ChannelID,
					"channel_id":      subscription.ChannelID,
					"cursor":          cursor,
					"event":           event,
				}
			if summary, ok := s.messageSummaryFromEvent(subscription.State, event); ok {
				result["message"] = summary
			}
			return marshalToolResult(result)
		}
		if page.NextCursor != "" && page.NextCursor != cursor {
			cursor = page.NextCursor
			s.updateConversationSubscriptionCursor(ctx, subscriptionID, cursor)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for next matching event after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (s *Server) handleAPIRequest(ctx context.Context, args map[string]any) (string, error) {
	scope := strings.TrimSpace(stringArg(args, "auth_scope", ""))
	client, resolvedScope, err := s.clientForScope(ctx, scope)
	if err != nil {
		return "", err
	}

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

	result, err := client.Request(ctx, method, path, query, body)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{
		"auth_scope": resolvedScope,
		"method":     method,
		"path":       path,
		"result":     result,
	})
}

func (s *Server) handleListEvents(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	eventType := stringArg(args, "type", "")
	resourceType := stringArg(args, "resource_type", "")
	resourceID := stringArg(args, "resource_id", "")
	limit := intArg(args, "limit", 20)
	events, err := current.client.ListEvents(ctx, "", eventType, resourceType, resourceID, limit)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{
		"events": events,
	})
}

func (s *Server) handleWaitForEvents(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	eventType := stringArg(args, "type", "")
	resourceType := stringArg(args, "resource_type", "")
	resourceID := stringArg(args, "resource_id", "")
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond

	cursor, err := s.currentEventCursor(ctx, current.client)
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(timeout)
	for {
		page, err := current.client.ListEventPage(ctx, cursor, eventType, resourceType, resourceID, 50)
		if err != nil {
			return "", err
		}
		for _, event := range page.Items {
			return marshalToolResult(map[string]any{
				"status": "received",
				"event":  event,
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
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	conversationID := s.resolveConversationID(current, args)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id is required unless a default conversation is already set")
	}
	wantText := stringArg(args, "text", "")
	wantContains := stringArg(args, "contains_text", "")
	wantEmail := stringArg(args, "from_email", current.PeerUserEmail)
	wantUserID := stringArg(args, "from_user_id", current.PeerUserID)
	includeExisting := boolArg(args, "include_existing", false)
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond

	afterTS := ""
	if !includeExisting {
		afterTS, err = s.currentTopLevelMessageTS(ctx, current.client, conversationID)
		if err != nil {
			return "", err
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		msgs, err := current.client.ListMessages(ctx, conversationID, 50)
		if err != nil {
			return "", err
		}
		for _, msg := range msgs {
			summary := s.summarizeMessage(current, msg)
			if !includeExisting && compareMessageTS(summary.TS, afterTS) <= 0 {
				continue
			}
			if summary.UserID == current.UserID || msg.IsDeleted || msg.ThreadTS != nil {
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
			s.logger.Info("matched teraslack message", "conversation_id", conversationID, "sender_email", summary.SenderEmail, "text", summary.Text, "ts", summary.TS)
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

type messageSummary struct {
	TS          string `json:"ts"`
	Text        string `json:"text"`
	UserID      string `json:"user_id"`
	SenderEmail string `json:"sender_email"`
}

func (s *Server) bootstrap() *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bootstrapClient
}

func (s *Server) sessionDataForContext(ctx context.Context) *sessionData {
	key := s.sessionKeyFromContext(ctx)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessions == nil {
		s.sessions = make(map[string]*sessionData)
	}
	if s.sessionTTL > 0 {
		for sessionKey, data := range s.sessions {
			if now.Sub(data.lastAccess) > s.sessionTTL {
				delete(s.sessions, sessionKey)
			}
		}
	}

	data, ok := s.sessions[key]
	if !ok {
		data = &sessionData{
			owner:              s.initial,
			current:            s.initial,
			nextSubscriptionID: 1,
			subscriptions:      map[string]conversationSubscription{},
			lastAccess:         now,
		}
		s.sessions[key] = data
	} else {
		data.lastAccess = now
	}

	return data
}

func (s *Server) sessionKeyFromContext(ctx context.Context) string {
	if ctx != nil {
		if session, ok := ctx.Value(sessionContextKey{}).(*mcp.ServerSession); ok && session != nil {
			if id := strings.TrimSpace(session.ID()); id != "" {
				return id
			}
			return fmt.Sprintf("stdio:%p", session)
		}
	}
	return "default"
}

func (s *Server) setCurrentState(ctx context.Context, state sessionState) {
	data := s.sessionDataForContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	data.current = state
}

func (s *Server) currentState(ctx context.Context) (sessionState, error) {
	data := s.sessionDataForContext(ctx)
	if err := s.ensureSessionIdentity(ctx, data); err != nil {
		return sessionState{}, err
	}
	state := data.current
	return s.hydrateState(ctx, state, func(next sessionState) {
		s.mu.Lock()
		defer s.mu.Unlock()
		data.current = next
	})
}

func (s *Server) ownerState(ctx context.Context) (sessionState, error) {
	data := s.sessionDataForContext(ctx)
	state := data.owner
	return s.hydrateState(ctx, state, func(next sessionState) {
		s.mu.Lock()
		defer s.mu.Unlock()
		data.owner = next
	})
}

func (s *Server) hydrateState(ctx context.Context, state sessionState, persist func(sessionState)) (sessionState, error) {
	if state.client == nil {
		return state, nil
	}
	if state.WorkspaceID != "" && state.UserID != "" && state.UserEmail != "" && len(state.Permissions) > 0 && len(state.OAuthScopes) > 0 {
		return state, nil
	}

	auth, err := state.client.AuthMe(ctx)
	if err != nil {
		return sessionState{}, err
	}
	if state.WorkspaceID == "" {
		state.WorkspaceID = auth.WorkspaceID
	}
	if state.UserID == "" {
		state.UserID = auth.UserID
	}
	if state.UserID != "" && (state.UserName == "" || state.UserEmail == "") {
		user, err := state.client.GetUser(ctx, state.UserID)
		if err == nil {
			if state.UserName == "" {
				state.UserName = user.Name
			}
			if state.UserEmail == "" {
				state.UserEmail = user.Email
			}
		}
	}
	if len(state.Permissions) == 0 {
		state.Permissions = append([]string(nil), auth.Permissions...)
	}
	if len(state.OAuthScopes) == 0 {
		state.OAuthScopes = append([]string(nil), auth.Scopes...)
	}
	if persist != nil {
		persist(state)
	}
	return state, nil
}

func (s *Server) requireCurrentState(ctx context.Context) (sessionState, error) {
	state, err := s.currentState(ctx)
	if err != nil {
		return sessionState{}, err
	}
	if state.client == nil {
		return sessionState{}, fmt.Errorf("no active Teraslack identity; call register first or configure TERASLACK_API_KEY")
	}
	if state.UserID == "" {
		return sessionState{}, fmt.Errorf("active identity is missing user metadata")
	}
	if owner, err := s.ownerState(ctx); err == nil && len(owner.OAuthScopes) > 0 {
		// In OAuth-backed remote MCP, the bearer token represents the approving human.
		// Require clients to provision a session agent (via whoami with session_id)
		// before using stateful tools, to avoid acting directly as the owner.
		if state.UserID == owner.UserID {
			return sessionState{}, fmt.Errorf("OAuth-backed MCP requires a session agent; call whoami with session_id to provision one for this client session")
		}
	}
	return state, nil
}

func (s *Server) clientForScope(ctx context.Context, scope string) (*Client, string, error) {
	scope = strings.TrimSpace(scope)
	switch scope {
	case "", "current":
		if current, err := s.currentState(ctx); err == nil && current.client != nil {
			return current.client, "current", nil
		}
		if scope == "current" {
			return nil, "", fmt.Errorf("no active Teraslack identity is configured")
		}
		bootstrap := s.bootstrap()
		if bootstrap == nil {
			return nil, "", fmt.Errorf("no system API key is configured")
		}
		if owner, err := s.ownerState(ctx); err == nil {
			if len(owner.OAuthScopes) > 0 {
				return nil, "", fmt.Errorf("bootstrap operations are unavailable for OAuth-backed MCP sessions")
			}
		}
		return bootstrap, "bootstrap", nil
	case "bootstrap":
		bootstrap := s.bootstrap()
		if bootstrap == nil {
			return nil, "", fmt.Errorf("no system API key is configured")
		}
		if owner, err := s.ownerState(ctx); err == nil {
			if len(owner.OAuthScopes) > 0 {
				return nil, "", fmt.Errorf("bootstrap operations are unavailable for OAuth-backed MCP sessions")
			}
		}
		return bootstrap, "bootstrap", nil
	default:
		return nil, "", fmt.Errorf("auth_scope must be current or bootstrap")
	}
}

func (s *Server) ensureSessionIdentity(ctx context.Context, data *sessionData) error {
	if data == nil {
		return nil
	}

	owner, err := s.ownerState(ctx)
	if err != nil {
		return err
	}
	if len(owner.OAuthScopes) == 0 {
		return nil
	}

	data.provisionMu.Lock()
	defer data.provisionMu.Unlock()

	s.mu.RLock()
	clientSessionID := strings.TrimSpace(data.clientSessionID)
	ready := data.sessionIdentity.client != nil && data.sessionIdentity.UserID != "" && data.provisionedSession == clientSessionID
	s.mu.RUnlock()

	if clientSessionID == "" {
		// OAuth-backed MCP: do not auto-provision until the client supplies a session_id.
		return nil
	}
	if ready {
		return nil
	}

	provisioner, err := s.sessionProvisioningClient(owner)
	if err != nil {
		s.mu.Lock()
		data.autoProvisionErr = err
		s.mu.Unlock()
		return err
	}

	name := buildClientSessionAgentName(clientSessionID)
	email := defaultAgentEmail(name)
	user, found, err := s.findUserByIdentity(ctx, provisioner, name, email)
	if err != nil {
		s.mu.Lock()
		data.autoProvisionErr = err
		s.mu.Unlock()
		return err
	}
	if !found {
		user, err = provisioner.CreateUser(ctx, domain.CreateUserParams{
			Name:          name,
			Email:         email,
			OwnerID:       owner.UserID,
			PrincipalType: domain.PrincipalTypeAgent,
			IsBot:         true,
		})
		if err != nil {
			s.mu.Lock()
			data.autoProvisionErr = err
			s.mu.Unlock()
			return err
		}
	}

	state, err := s.issueSessionStateForUser(ctx, owner, user, fmt.Sprintf("%s session MCP key", user.Name))
	if err != nil {
		s.mu.Lock()
		data.autoProvisionErr = err
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	data.sessionIdentity = state
	data.current = state
	data.provisionedSession = clientSessionID
	data.autoProvisionErr = nil
	s.mu.Unlock()

	return nil
}

func (s *Server) setClientSessionID(ctx context.Context, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	data := s.sessionDataForContext(ctx)
	data.provisionMu.Lock()
	defer data.provisionMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if data.clientSessionID == sessionID {
		return
	}
	data.clientSessionID = sessionID
	data.autoProvisionErr = nil
	data.sessionIdentity = sessionState{}
	data.provisionedSession = ""
}

func (s *Server) issueSessionStateForUser(ctx context.Context, owner sessionState, user *domain.User, keyName string) (sessionState, error) {
	if user == nil {
		return sessionState{}, fmt.Errorf("user is required")
	}
	provisioner, err := s.sessionProvisioningClient(owner)
	if err != nil {
		return sessionState{}, err
	}

	key, secret, err := provisioner.CreateAPIKey(ctx, domain.CreateAPIKeyParams{
		Name:        keyName,
		UserID:      user.ID,
		Permissions: []string{"*"},
	})
	if err != nil {
		return sessionState{}, err
	}

	client, err := NewClient(s.cfg.BaseURL, secret)
	if err != nil {
		return sessionState{}, err
	}

	return sessionState{
		Token:         secret,
		WorkspaceID:   firstNonEmpty(key.WorkspaceID, user.WorkspaceID, owner.WorkspaceID, s.cfg.WorkspaceID),
		UserID:        user.ID,
		UserName:      user.Name,
		UserEmail:     user.Email,
		Permissions:   append([]string(nil), key.Permissions...),
		OAuthScopes:   append([]string(nil), owner.OAuthScopes...),
		ChannelID:     "",
		PeerUserID:    "",
		PeerUserName:  "",
		PeerUserEmail: "",
		client:        client,
	}, nil
}

func (s *Server) sessionProvisioningClient(owner sessionState) (*Client, error) {
	if len(owner.OAuthScopes) > 0 {
		if owner.client == nil {
			return nil, fmt.Errorf("no active owner identity is configured for session provisioning")
		}
		return owner.client, nil
	}
	bootstrap := s.bootstrap()
	if bootstrap == nil {
		return nil, fmt.Errorf("no system API key is configured")
	}
	return bootstrap, nil
}

func (s *Server) bootstrapAvailable(owner sessionState) bool {
	return len(owner.OAuthScopes) == 0 && s.bootstrap() != nil
}

func hasScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

func (s *Server) currentEventCursor(ctx context.Context, client *Client) (string, error) {
	cursor := ""
	for {
		page, err := client.ListEventPage(ctx, cursor, "", "", "", 100)
		if err != nil {
			return "", err
		}
		if page.NextCursor != "" {
			cursor = page.NextCursor
		}
		if !page.HasMore {
			return cursor, nil
		}
	}
}

func (s *Server) summarizeMessage(state sessionState, msg domain.Message) messageSummary {
	return messageSummary{
		TS:          msg.TS,
		Text:        msg.Text,
		UserID:      msg.UserID,
		SenderEmail: s.emailForUserID(state, msg.UserID),
	}
}

func (s *Server) messageSummaryFromEvent(state sessionState, event domain.ExternalEvent) (messageSummary, bool) {
	switch event.Type {
	case domain.EventTypeConversationMessageCreated, domain.EventTypeConversationMessageUpdated:
		var payload struct {
			TS        string `json:"ts"`
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
			Text      string `json:"text"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return messageSummary{}, false
		}
		return messageSummary{
			TS:          payload.TS,
			Text:        payload.Text,
			UserID:      payload.UserID,
			SenderEmail: s.emailForUserID(state, payload.UserID),
		}, true
	default:
		return messageSummary{}, false
	}
}

func (s *Server) conversationEventMatches(state sessionState, event domain.ExternalEvent, eventType, wantUserID, wantEmail, wantText, wantContains string, includeSelf bool) bool {
	if eventType != "" && event.Type != eventType {
		return false
	}
	if wantUserID == "" && wantEmail == "" && wantText == "" && wantContains == "" && includeSelf {
		return true
	}

	summary, ok := s.messageSummaryFromEvent(state, event)
	if !ok {
		return wantUserID == "" && wantEmail == "" && wantText == "" && wantContains == ""
	}
	if !includeSelf && summary.UserID == state.UserID {
		return false
	}
	if wantUserID != "" && summary.UserID != wantUserID {
		return false
	}
	if wantEmail != "" && summary.SenderEmail != wantEmail {
		return false
	}
	if wantText != "" && summary.Text != wantText {
		return false
	}
	if wantContains != "" && !strings.Contains(summary.Text, wantContains) {
		return false
	}
	return true
}

func (s *Server) emailForUserID(state sessionState, userID string) string {
	switch userID {
	case "":
		return ""
	case state.UserID:
		if state.UserEmail != "" {
			return state.UserEmail
		}
	case state.PeerUserID:
		if state.PeerUserEmail != "" {
			return state.PeerUserEmail
		}
	}
	return userID
}

func (s *Server) resolveConversationID(state sessionState, args map[string]any) string {
	conversationID := strings.TrimSpace(stringArg(args, "conversation_id", ""))
	if conversationID != "" {
		return conversationID
	}
	conversationID = strings.TrimSpace(stringArg(args, "channel_id", ""))
	if conversationID != "" {
		return conversationID
	}
	return state.ChannelID
}

func (s *Server) createConversationSubscription(ctx context.Context, state sessionState, channelID, cursor string) string {
	data := s.sessionDataForContext(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("sub_%03d", data.nextSubscriptionID)
	data.nextSubscriptionID++
	data.subscriptions[id] = conversationSubscription{
		ID:        id,
		ChannelID: channelID,
		Cursor:    cursor,
		State:     state,
	}
	return id
}

func (s *Server) conversationSubscription(ctx context.Context, id string) (conversationSubscription, bool) {
	data := s.sessionDataForContext(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()

	subscription, ok := data.subscriptions[id]
	return subscription, ok
}

func (s *Server) updateConversationSubscriptionCursor(ctx context.Context, id, cursor string) {
	data := s.sessionDataForContext(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	subscription, ok := data.subscriptions[id]
	if !ok {
		return
	}
	subscription.Cursor = cursor
	data.subscriptions[id] = subscription
}

func (s *Server) findUserByIdentity(ctx context.Context, client *Client, name, email string) (*domain.User, bool, error) {
	cursor := ""
	for {
		page, err := client.ListUsers(ctx, s.cfg.WorkspaceID, cursor, email, 100)
		if err != nil {
			return nil, false, err
		}
		for _, user := range page.Items {
			if strings.EqualFold(strings.TrimSpace(user.Name), name) {
				u := user
				return &u, true, nil
			}
			if email != "" && strings.EqualFold(strings.TrimSpace(user.Email), email) {
				u := user
				return &u, true, nil
			}
		}
		if page.NextCursor == "" || len(page.Items) == 0 {
			return nil, false, nil
		}
		cursor = page.NextCursor
	}
}

func (s *Server) listOwnedAgentUsers(ctx context.Context, owner sessionState) ([]domain.User, error) {
	client, err := s.sessionProvisioningClient(owner)
	if err != nil {
		return nil, err
	}
	if owner.UserID == "" {
		return nil, fmt.Errorf("session owner is missing user metadata")
	}

	cursor := ""
	var users []domain.User
	for {
		page, err := client.ListUsers(ctx, firstNonEmpty(owner.WorkspaceID, s.cfg.WorkspaceID), cursor, "", 100)
		if err != nil {
			return nil, err
		}
		for _, user := range page.Items {
			if user.PrincipalType != domain.PrincipalTypeAgent || user.OwnerID != owner.UserID {
				continue
			}
			users = append(users, user)
		}
		if page.NextCursor == "" || len(page.Items) == 0 {
			break
		}
		cursor = page.NextCursor
	}
	return users, nil
}

func (s *Server) resolveOwnedAgentUser(ctx context.Context, owner sessionState, args map[string]any) (*domain.User, error) {
	userID := strings.TrimSpace(stringArg(args, "user_id", ""))
	name := strings.TrimSpace(stringArg(args, "name", ""))
	email := strings.TrimSpace(stringArg(args, "email", ""))
	if userID == "" && name == "" && email == "" {
		return nil, fmt.Errorf("switch_identity requires user_id, name, or email")
	}

	users, err := s.listOwnedAgentUsers(ctx, owner)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		switch {
		case userID != "" && user.ID == userID:
			u := user
			return &u, nil
		case name != "" && strings.EqualFold(strings.TrimSpace(user.Name), name):
			u := user
			return &u, nil
		case email != "" && strings.EqualFold(strings.TrimSpace(user.Email), email):
			u := user
			return &u, nil
		}
	}
	return nil, fmt.Errorf("owned agent identity not found")
}

func marshalToolResult(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	return string(data), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func defaultAgentEmail(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	replacer := strings.NewReplacer(" ", "-", "_", "-", "/", "-", "@", "-")
	slug = replacer.Replace(slug)
	if slug == "" {
		slug = "agent"
	}
	return slug + "@mcp.teraslack.local"
}

func buildSessionAgentName(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "mcp-session-agent"
	}
	if len(sessionKey) > 8 {
		sessionKey = sessionKey[len(sessionKey)-8:]
	}
	replacer := strings.NewReplacer(":", "-", "/", "-", "_", "-", "@", "-")
	sessionKey = replacer.Replace(sessionKey)
	return "mcp-session-" + sessionKey
}

func buildClientSessionAgentName(clientSessionID string) string {
	clientSessionID = strings.TrimSpace(clientSessionID)
	if clientSessionID == "" {
		return "mcp-session-agent"
	}
	// Keep this readable but bounded: clients may use long random IDs.
	if len(clientSessionID) > 16 {
		clientSessionID = clientSessionID[len(clientSessionID)-16:]
	}
	replacer := strings.NewReplacer(":", "-", "/", "-", "_", "-", "@", "-", " ", "-")
	clientSessionID = replacer.Replace(clientSessionID)
	return "mcp-session-" + clientSessionID
}

func userMatchesQuery(user domain.User, query string, exact bool) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	candidates := []string{
		user.ID,
		user.Name,
		user.Email,
		user.RealName,
		user.DisplayName,
	}
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

func stringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		str, ok := value.(string)
		if !ok {
			continue
		}
		str = strings.TrimSpace(str)
		if str != "" {
			out = append(out, str)
		}
	}
	return out
}

func (s *Server) currentTopLevelMessageTS(ctx context.Context, client *Client, channelID string) (string, error) {
	msgs, err := client.ListMessages(ctx, channelID, 50)
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

func (s *Server) debugf(format string, args ...any) {
	if s.debug == nil {
		return
	}
	fmt.Fprintf(s.debug, "%s ", time.Now().Format(time.RFC3339Nano))
	fmt.Fprintf(s.debug, format, args...)
	fmt.Fprintln(s.debug)
}
