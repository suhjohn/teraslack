package teraslackmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
)

const defaultProtocolVersion = "2025-06-18"

type Config struct {
	BaseURL        string
	APIKey         string
	BootstrapToken string
	TeamID         string
	UserID         string
	UserName       string
	UserEmail      string
	PeerUserID     string
	PeerUserName   string
	PeerUserEmail  string
	ChannelID      string
	DebugLogPath   string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:        strings.TrimSpace(os.Getenv("TERASLACK_BASE_URL")),
		APIKey:         strings.TrimSpace(os.Getenv("TERASLACK_API_KEY")),
		BootstrapToken: firstNonEmptyEnv("TERASLACK_BOOTSTRAP_TOKEN", "TERASLACK_BOOTSTRAP_API_KEY", "TERASLACK_SYSTEM_API_KEY"),
		TeamID:         strings.TrimSpace(os.Getenv("TERASLACK_TEAM_ID")),
		UserID:         strings.TrimSpace(os.Getenv("TERASLACK_USER_ID")),
		UserName:       strings.TrimSpace(os.Getenv("TERASLACK_USER_NAME")),
		UserEmail:      strings.TrimSpace(os.Getenv("TERASLACK_USER_EMAIL")),
		PeerUserID:     strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_ID")),
		PeerUserName:   strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_NAME")),
		PeerUserEmail:  strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_EMAIL")),
		ChannelID:      strings.TrimSpace(os.Getenv("TERASLACK_CHANNEL_ID")),
		DebugLogPath:   strings.TrimSpace(os.Getenv("TERASLACK_MCP_DEBUG_LOG")),
	}

	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("missing required environment variable: TERASLACK_BASE_URL")
	}
	if cfg.APIKey == "" && cfg.BootstrapToken == "" {
		return Config{}, fmt.Errorf("either TERASLACK_API_KEY or a bootstrap token must be configured")
	}
	return cfg, nil
}

type sessionState struct {
	Token         string
	TeamID        string
	UserID        string
	UserName      string
	UserEmail     string
	PeerUserID    string
	PeerUserName  string
	PeerUserEmail string
	ChannelID     string
	client        *Client
}

type Server struct {
	cfg             Config
	logger          *slog.Logger
	debug           io.Writer
	bootstrapClient *Client

	mu      sync.RWMutex
	current sessionState
}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(cfg.BootstrapToken) == "" {
		return nil, fmt.Errorf("an acting API key or bootstrap token is required")
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

	var current sessionState
	if cfg.APIKey != "" {
		client, err := NewClient(cfg.BaseURL, cfg.APIKey)
		if err != nil {
			return nil, err
		}
		current = sessionState{
			Token:         cfg.APIKey,
			TeamID:        cfg.TeamID,
			UserID:        cfg.UserID,
			UserName:      cfg.UserName,
			UserEmail:     cfg.UserEmail,
			PeerUserID:    cfg.PeerUserID,
			PeerUserName:  cfg.PeerUserName,
			PeerUserEmail: cfg.PeerUserEmail,
			ChannelID:     cfg.ChannelID,
			client:        client,
		}
	}

	bootstrapToken := strings.TrimSpace(cfg.BootstrapToken)
	if bootstrapToken == "" {
		bootstrapToken = strings.TrimSpace(cfg.APIKey)
	}

	var bootstrapClient *Client
	if bootstrapToken != "" {
		client, err := NewClient(cfg.BaseURL, bootstrapToken)
		if err != nil {
			return nil, err
		}
		bootstrapClient = client
	}

	return &Server{
		cfg:             cfg,
		logger:          logger,
		debug:           debug,
		bootstrapClient: bootstrapClient,
		current:         current,
	}, nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush()
	s.debugf("serve start")

	for {
		payload, err := readRPCMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.debugf("serve EOF")
				return nil
			}
			s.debugf("serve read error: %v", err)
			return err
		}
		s.debugf("serve received payload bytes=%d", len(payload))

		if len(payload) == 0 {
			continue
		}

		if payload[0] == '[' {
			var batch []json.RawMessage
			if err := json.Unmarshal(payload, &batch); err != nil {
				if err := writeRPCMessage(writer, rpcResponse{
					JSONRPC: "2.0",
					Error:   &rpcError{Code: -32700, Message: "invalid JSON batch"},
				}); err != nil {
					return err
				}
				continue
			}
			for _, msg := range batch {
				if err := s.handleOne(ctx, msg, writer); err != nil {
					return err
				}
			}
			continue
		}

		if err := s.handleOne(ctx, payload, writer); err != nil {
			return err
		}
	}
}

func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		payload, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("MCP-Protocol-Version", defaultProtocolVersion)

		if len(payload) == 0 {
			http.Error(w, "empty request body", http.StatusBadRequest)
			return
		}

		if payload[0] == '[' {
			var batch []json.RawMessage
			if err := json.Unmarshal(payload, &batch); err != nil {
				_ = json.NewEncoder(w).Encode(rpcResponse{
					JSONRPC: "2.0",
					Error:   &rpcError{Code: -32700, Message: "invalid JSON batch"},
				})
				return
			}

			responses := make([]rpcResponse, 0, len(batch))
			for _, msg := range batch {
				resp, ok := s.decodeAndDispatch(r.Context(), msg)
				if ok {
					responses = append(responses, resp)
				}
			}
			if len(responses) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			_ = json.NewEncoder(w).Encode(responses)
			return
		}

		resp, ok := s.decodeAndDispatch(r.Context(), payload)
		if !ok {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func (s *Server) handleOne(ctx context.Context, payload []byte, writer *bufio.Writer) error {
	resp, ok := s.decodeAndDispatch(ctx, payload)
	if !ok {
		return nil
	}
	return writeRPCMessage(writer, resp)
}

func (s *Server) decodeAndDispatch(ctx context.Context, payload []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "invalid JSON"},
		}, true
	}

	return s.dispatch(ctx, req)
}

func (s *Server) dispatch(ctx context.Context, req rpcRequest) (rpcResponse, bool) {
	s.debugf("dispatch method=%s", req.Method)
	var id any
	if len(req.ID) > 0 {
		if err := json.Unmarshal(req.ID, &id); err != nil {
			id = string(req.ID)
		}
	}

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		protocolVersion := params.ProtocolVersion
		if protocolVersion == "" {
			protocolVersion = defaultProtocolVersion
		}

		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
				},
				"serverInfo": map[string]any{
					"name":    "teraslack-mcp",
					"version": "0.2.0",
				},
			},
		}, true
	case "notifications/initialized":
		return rpcResponse{}, false
	case "ping":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result:  map[string]any{},
		}, true
	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"tools": s.tools(),
			},
		}, true
	case "tools/call":
		result, err := s.callTool(ctx, req.Params)
		if err != nil {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      id,
				Result: map[string]any{
					"content": []map[string]any{{
						"type": "text",
						"text": err.Error(),
					}},
					"isError": true,
				},
			}, true
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"content": []map[string]any{{
					"type": "text",
					"text": result,
				}},
			},
		}, true
	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &rpcError{Code: -32601, Message: "method not found"},
		}, true
	}
}

func (s *Server) tools() []map[string]any {
	return []map[string]any{
		{
			"name":        "register",
			"description": "Create or reuse a Teraslack agent identity by name using the configured bootstrap token, issue a scoped API key for it, and switch this MCP session to act as that identity.",
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
			"description": "Return the active Teraslack identity for this MCP session and whether bootstrap registration is available.",
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
			"description": "Send a message to a Teraslack conversation as the active identity.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id": map[string]any{
						"type": "string",
					},
					"text": map[string]any{
						"type": "string",
					},
					"notification_targets": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
					"notification_kind": map[string]any{
						"type": "string",
					},
					"notification_reason": map[string]any{
						"type": "string",
					},
					"notification_title": map[string]any{
						"type": "string",
					},
					"notification_body_preview": map[string]any{
						"type": "string",
					},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_messages",
			"description": "List recent messages in a Teraslack conversation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
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
			"name":        "api_request",
			"description": "Call any Teraslack HTTP API over MCP. By default it uses the active identity; set auth_scope to bootstrap to use the bootstrap token instead.",
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
			"name":        "list_notifications",
			"description": "List recent synthesized inbox-style summaries derived from external events.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
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
			"name":        "wait_for_notification",
			"description": "Wait until a matching synthesized notification appears from the external event stream.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type": "string",
					},
					"body_preview": map[string]any{
						"type": "string",
					},
					"from_email": map[string]any{
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
			"description": "Wait until a matching message appears in a Teraslack conversation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id": map[string]any{
						"type": "string",
					},
					"text": map[string]any{
						"type": "string",
					},
					"from_email": map[string]any{
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
		return s.handleWhoAmI(ctx)
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
	case "api_request":
		return s.handleAPIRequest(ctx, req.Arguments)
	case "list_notifications":
		return s.handleListNotifications(ctx, req.Arguments)
	case "wait_for_notification":
		return s.handleWaitForNotification(ctx, req.Arguments)
	case "wait_for_message":
		return s.handleWaitForMessage(ctx, req.Arguments)
	default:
		return "", fmt.Errorf("unknown tool %q", req.Name)
	}
}

func (s *Server) handleRegister(ctx context.Context, args map[string]any) (string, error) {
	bootstrap := s.bootstrap()
	if bootstrap == nil {
		return "", fmt.Errorf("register requires a configured bootstrap token")
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
		PrincipalID: user.ID,
		Type:        domain.APIKeyTypePersistent,
		Environment: domain.APIKeyEnvLive,
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

	current, _ := s.currentState(ctx)
	current.Token = secret
	current.TeamID = firstNonEmpty(key.TeamID, user.TeamID, current.TeamID, s.cfg.TeamID)
	current.UserID = user.ID
	current.UserName = user.Name
	current.UserEmail = user.Email
	current.ChannelID = ""
	current.client = client
	s.setCurrentState(current)

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
			"team_id":      key.TeamID,
			"principal_id": key.PrincipalID,
			"permissions":  key.Permissions,
		},
	})
}

func (s *Server) handleWhoAmI(ctx context.Context) (string, error) {
	current, err := s.currentState(ctx)
	if err != nil {
		return "", err
	}

	result := map[string]any{
		"registered":          current.client != nil,
		"bootstrap_available": s.bootstrap() != nil,
	}
	if current.TeamID != "" {
		result["team_id"] = current.TeamID
	}
	if current.client != nil {
		result["user"] = map[string]any{
			"id":    current.UserID,
			"name":  current.UserName,
			"email": current.UserEmail,
		}
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
	return marshalToolResult(result)
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

	var out []map[string]any
	cursor := ""
	for len(out) < limit {
		page, err := current.client.ListUsers(ctx, current.TeamID, cursor, 100)
		if err != nil {
			return "", err
		}
		for _, user := range page.Items {
			if !userMatchesQuery(user, query, exact) {
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
		s.setCurrentState(current)
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
	channelID := s.resolveChannelID(current, args)
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required unless a default conversation is already set")
	}

	msg, err := current.client.PostMessage(ctx, channelID, current.UserID, text, notificationMetadataFromArgs(args))
	if err != nil {
		return "", err
	}
	if current.ChannelID == "" {
		current.ChannelID = channelID
		s.setCurrentState(current)
	}

	s.logger.Info("sent teraslack message", "channel_id", channelID, "user_email", current.UserEmail, "text", text, "ts", msg.TS)
	return marshalToolResult(map[string]any{
		"status": "sent",
		"message": messageSummary{
			TS:          msg.TS,
			Text:        msg.Text,
			UserID:      msg.UserID,
			SenderEmail: s.emailForUserID(current, msg.UserID),
		},
		"channel_id": channelID,
	})
}

func (s *Server) handleListMessages(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	channelID := s.resolveChannelID(current, args)
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required unless a default conversation is already set")
	}
	limit := intArg(args, "limit", 20)
	msgs, err := current.client.ListMessages(ctx, channelID, limit)
	if err != nil {
		return "", err
	}
	out := make([]messageSummary, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, s.summarizeMessage(current, msg))
	}
	return marshalToolResult(map[string]any{
		"channel_id": channelID,
		"messages":   out,
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

func (s *Server) handleListNotifications(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	kind := stringArg(args, "kind", "")
	limit := intArg(args, "limit", 20)
	events, err := current.client.ListEvents(ctx, "", "", "", "", limit)
	if err != nil {
		return "", err
	}
	out := make([]notificationSummary, 0, len(events))
	for _, event := range events {
		summary, ok := s.summarizeEventNotification(current, event)
		if !ok {
			continue
		}
		if kind != "" && summary.Kind != kind {
			continue
		}
		out = append(out, summary)
	}
	return marshalToolResult(map[string]any{
		"notifications": out,
	})
}

func (s *Server) handleWaitForNotification(ctx context.Context, args map[string]any) (string, error) {
	current, err := s.requireCurrentState(ctx)
	if err != nil {
		return "", err
	}

	wantKind := stringArg(args, "kind", "")
	wantPreview := stringArg(args, "body_preview", "")
	wantEmail := stringArg(args, "from_email", current.PeerUserEmail)
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond

	cursor, err := s.currentEventCursor(ctx, current.client)
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(timeout)
	for {
		page, err := current.client.ListEventPage(ctx, cursor, "", "", "", 50)
		if err != nil {
			return "", err
		}
		for _, event := range page.Items {
			summary, ok := s.summarizeEventNotification(current, event)
			if !ok {
				continue
			}
			if wantKind != "" && summary.Kind != wantKind {
				continue
			}
			if wantPreview != "" && summary.BodyPreview != wantPreview {
				continue
			}
			if wantEmail != "" && summary.ActorEmail != wantEmail {
				continue
			}
			return marshalToolResult(map[string]any{
				"status":       "received",
				"notification": summary,
			})
		}
		if page.NextCursor != "" {
			cursor = page.NextCursor
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for matching notification after %s", timeout)
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

	channelID := s.resolveChannelID(current, args)
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required unless a default conversation is already set")
	}
	wantText := stringArg(args, "text", "")
	wantEmail := stringArg(args, "from_email", current.PeerUserEmail)
	timeout := time.Duration(intArg(args, "timeout_seconds", 30)) * time.Second
	pollInterval := time.Duration(intArg(args, "poll_interval_ms", 500)) * time.Millisecond

	deadline := time.Now().Add(timeout)
	for {
		msgs, err := current.client.ListMessages(ctx, channelID, 50)
		if err != nil {
			return "", err
		}
		for _, msg := range msgs {
			summary := s.summarizeMessage(current, msg)
			if summary.UserID == current.UserID || msg.IsDeleted || msg.ThreadTS != nil {
				continue
			}
			if wantText != "" && summary.Text != wantText {
				continue
			}
			if wantEmail != "" && summary.SenderEmail != wantEmail {
				continue
			}
			s.logger.Info("matched teraslack message", "channel_id", channelID, "sender_email", summary.SenderEmail, "text", summary.Text, "ts", summary.TS)
			return marshalToolResult(map[string]any{
				"status":     "received",
				"channel_id": channelID,
				"message":    summary,
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

type notificationSummary struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Reason      string `json:"reason"`
	ActorID     string `json:"actor_id"`
	ActorEmail  string `json:"actor_email"`
	ChannelID   string `json:"channel_id"`
	MessageTS   string `json:"message_ts"`
	Title       string `json:"title"`
	BodyPreview string `json:"body_preview"`
	State       string `json:"state"`
}

func (s *Server) bootstrap() *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bootstrapClient
}

func (s *Server) setCurrentState(state sessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = state
}

func (s *Server) currentState(ctx context.Context) (sessionState, error) {
	s.mu.RLock()
	state := s.current
	s.mu.RUnlock()
	if state.client == nil {
		return state, nil
	}
	if state.TeamID != "" && state.UserID != "" && state.UserEmail != "" {
		return state, nil
	}

	auth, err := state.client.AuthMe(ctx)
	if err != nil {
		return sessionState{}, err
	}
	if state.TeamID == "" {
		state.TeamID = auth.TeamID
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
	s.setCurrentState(state)
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
			return nil, "", fmt.Errorf("no bootstrap token is configured")
		}
		return bootstrap, "bootstrap", nil
	case "bootstrap":
		bootstrap := s.bootstrap()
		if bootstrap == nil {
			return nil, "", fmt.Errorf("no bootstrap token is configured")
		}
		return bootstrap, "bootstrap", nil
	default:
		return nil, "", fmt.Errorf("auth_scope must be current or bootstrap")
	}
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

func (s *Server) summarizeEventNotification(state sessionState, event domain.ExternalEvent) (notificationSummary, bool) {
	var summary notificationSummary

	switch event.Type {
	case domain.EventTypeConversationMessageCreated, domain.EventTypeConversationMessageUpdated:
		var payload struct {
			TS        string `json:"ts"`
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
			Text      string `json:"text"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return notificationSummary{}, false
		}
		summary = notificationSummary{
			ID:          event.ID,
			Kind:        "message",
			ActorID:     payload.UserID,
			ChannelID:   payload.ChannelID,
			MessageTS:   payload.TS,
			BodyPreview: payload.Text,
			State:       "new",
		}
		if state.ChannelID != "" && payload.ChannelID == state.ChannelID {
			summary.Kind = "direct_message"
			summary.Title = "New direct message"
			summary.Reason = "dm_recipient"
		}
	case domain.EventTypeFileShared:
		var payload struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return notificationSummary{}, false
		}
		summary = notificationSummary{
			ID:        event.ID,
			Kind:      "file_shared",
			Reason:    "resource_shared",
			ChannelID: payload.ChannelID,
			Title:     "A file was shared",
			State:     "new",
		}
	case domain.EventTypeConversationMemberAdded:
		var payload struct {
			ConversationID string `json:"conversation_id"`
			UserID         string `json:"user_id"`
			ActorID        string `json:"actor_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return notificationSummary{}, false
		}
		if payload.UserID != state.UserID {
			return notificationSummary{}, false
		}
		summary = notificationSummary{
			ID:        event.ID,
			Kind:      "conversation_invite",
			Reason:    "invited",
			ActorID:   payload.ActorID,
			ChannelID: payload.ConversationID,
			Title:     "You were added to a conversation",
			State:     "new",
		}
	default:
		return notificationSummary{}, false
	}

	summary.ActorEmail = s.emailForUserID(state, summary.ActorID)
	return summary, true
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

func (s *Server) resolveChannelID(state sessionState, args map[string]any) string {
	channelID := strings.TrimSpace(stringArg(args, "channel_id", ""))
	if channelID != "" {
		return channelID
	}
	return state.ChannelID
}

func (s *Server) findUserByIdentity(ctx context.Context, client *Client, name, email string) (*domain.User, bool, error) {
	cursor := ""
	for {
		page, err := client.ListUsers(ctx, s.cfg.TeamID, cursor, 100)
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

func notificationMetadataFromArgs(args map[string]any) map[string]any {
	targets := stringSliceArg(args, "notification_targets")
	if len(targets) == 0 {
		return nil
	}
	notification := map[string]any{
		"targets": targets,
	}
	if v := strings.TrimSpace(stringArg(args, "notification_kind", "")); v != "" {
		notification["kind"] = v
	}
	if v := strings.TrimSpace(stringArg(args, "notification_reason", "")); v != "" {
		notification["reason"] = v
	}
	if v := strings.TrimSpace(stringArg(args, "notification_title", "")); v != "" {
		notification["title"] = v
	}
	if v := strings.TrimSpace(stringArg(args, "notification_body_preview", "")); v != "" {
		notification["body_preview"] = v
	}
	return map[string]any{"notification": notification}
}

func readRPCMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])
		if key == "content-length" {
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid content-length %q: %w", value, err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing content-length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeRPCMessage(writer *bufio.Writer, msg rpcResponse) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal RPC message: %w", err)
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	return writer.Flush()
}

func (s *Server) debugf(format string, args ...any) {
	if s.debug == nil {
		return
	}
	fmt.Fprintf(s.debug, "%s ", time.Now().Format(time.RFC3339Nano))
	fmt.Fprintf(s.debug, format, args...)
	fmt.Fprintln(s.debug)
}
