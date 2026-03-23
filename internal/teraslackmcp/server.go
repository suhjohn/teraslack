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
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
)

const defaultProtocolVersion = "2025-06-18"

type Config struct {
	BaseURL       string
	APIKey        string
	TeamID        string
	UserID        string
	UserName      string
	UserEmail     string
	PeerUserID    string
	PeerUserName  string
	PeerUserEmail string
	ChannelID     string
	DebugLogPath  string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:       strings.TrimSpace(os.Getenv("TERASLACK_BASE_URL")),
		APIKey:        strings.TrimSpace(os.Getenv("TERASLACK_API_KEY")),
		TeamID:        strings.TrimSpace(os.Getenv("TERASLACK_TEAM_ID")),
		UserID:        strings.TrimSpace(os.Getenv("TERASLACK_USER_ID")),
		UserName:      strings.TrimSpace(os.Getenv("TERASLACK_USER_NAME")),
		UserEmail:     strings.TrimSpace(os.Getenv("TERASLACK_USER_EMAIL")),
		PeerUserID:    strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_ID")),
		PeerUserName:  strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_NAME")),
		PeerUserEmail: strings.TrimSpace(os.Getenv("TERASLACK_PEER_USER_EMAIL")),
		ChannelID:     strings.TrimSpace(os.Getenv("TERASLACK_CHANNEL_ID")),
		DebugLogPath:  strings.TrimSpace(os.Getenv("TERASLACK_MCP_DEBUG_LOG")),
	}

	missing := make([]string, 0, 5)
	for key, value := range map[string]string{
		"TERASLACK_BASE_URL":   cfg.BaseURL,
		"TERASLACK_API_KEY":    cfg.APIKey,
		"TERASLACK_USER_ID":    cfg.UserID,
		"TERASLACK_USER_EMAIL": cfg.UserEmail,
		"TERASLACK_CHANNEL_ID": cfg.ChannelID,
	} {
		if value == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

type Server struct {
	cfg    Config
	client *Client
	logger *slog.Logger
	debug  io.Writer
}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	client, err := NewClient(cfg.BaseURL, cfg.APIKey)
	if err != nil {
		return nil, err
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
	return &Server{
		cfg:    cfg,
		client: client,
		logger: logger,
		debug:  debug,
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
					"version": "0.1.0",
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
					"content": []map[string]any{
						{
							"type": "text",
							"text": err.Error(),
						},
					},
					"isError": true,
				},
			}, true
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": result,
					},
				},
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
			"name":        "whoami",
			"description": "Return the current teraslack identity and the configured peer conversation for this Codex instance.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			"name":        "send_message",
			"description": "Send a message to the configured teraslack peer conversation. Use this to send the exact chat text requested by the user.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Exact message text to send.",
					},
					"notification_targets": map[string]any{
						"type":        "array",
						"description": "Optional list of explicit teraslack user IDs that should receive a notification for this message.",
						"items": map[string]any{
							"type": "string",
						},
					},
					"notification_kind": map[string]any{
						"type":        "string",
						"description": "Optional notification kind for explicit notification targets.",
					},
					"notification_reason": map[string]any{
						"type":        "string",
						"description": "Optional notification reason for explicit notification targets.",
					},
					"notification_title": map[string]any{
						"type":        "string",
						"description": "Optional notification title for explicit notification targets.",
					},
					"notification_body_preview": map[string]any{
						"type":        "string",
						"description": "Optional notification preview text for explicit notification targets.",
					},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_messages",
			"description": "List recent messages in the configured teraslack peer conversation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     100,
						"description": "Maximum number of messages to return. Defaults to 20.",
					},
				},
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
						"type":        "string",
						"description": "Optional synthesized notification kind filter.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     100,
						"description": "Maximum number of notifications to return. Defaults to 20.",
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
						"type":        "string",
						"description": "If set, require an exact notification kind.",
					},
					"body_preview": map[string]any{
						"type":        "string",
						"description": "If set, require the notification body preview to match exactly.",
					},
					"from_email": map[string]any{
						"type":        "string",
						"description": "If set, require the notification actor email to match exactly.",
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     300,
						"description": "Maximum time to wait before failing. Defaults to 30.",
					},
					"poll_interval_ms": map[string]any{
						"type":        "integer",
						"minimum":     100,
						"maximum":     10000,
						"description": "Polling interval in milliseconds. Defaults to 500.",
					},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "wait_for_message",
			"description": "Wait until a matching message appears in the configured teraslack peer conversation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "If set, require the message text to match exactly.",
					},
					"from_email": map[string]any{
						"type":        "string",
						"description": "If set, require the sender email to match exactly.",
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     300,
						"description": "Maximum time to wait before failing. Defaults to 30.",
					},
					"poll_interval_ms": map[string]any{
						"type":        "integer",
						"minimum":     100,
						"maximum":     10000,
						"description": "Polling interval in milliseconds. Defaults to 500.",
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
	case "whoami":
		return marshalToolResult(map[string]any{
			"team_id": s.cfg.TeamID,
			"user": map[string]any{
				"id":    s.cfg.UserID,
				"name":  s.cfg.UserName,
				"email": s.cfg.UserEmail,
			},
			"peer": map[string]any{
				"id":    s.cfg.PeerUserID,
				"name":  s.cfg.PeerUserName,
				"email": s.cfg.PeerUserEmail,
			},
			"conversation": map[string]any{
				"id": s.cfg.ChannelID,
			},
		})
	case "send_message":
		text := strings.TrimSpace(stringArg(req.Arguments, "text", ""))
		if text == "" {
			return "", fmt.Errorf("text is required")
		}
		msg, err := s.client.PostMessage(ctx, s.cfg.ChannelID, s.cfg.UserID, text, notificationMetadataFromArgs(req.Arguments))
		if err != nil {
			return "", err
		}
		s.logger.Info("sent teraslack message", "channel_id", s.cfg.ChannelID, "user_email", s.cfg.UserEmail, "text", text, "ts", msg.TS)
		return marshalToolResult(map[string]any{
			"status": "sent",
			"message": messageSummary{
				TS:          msg.TS,
				Text:        msg.Text,
				UserID:      msg.UserID,
				SenderEmail: s.cfg.UserEmail,
			},
			"channel_id": s.cfg.ChannelID,
		})
	case "list_messages":
		limit := intArg(req.Arguments, "limit", 20)
		msgs, err := s.client.ListMessages(ctx, s.cfg.ChannelID, limit)
		if err != nil {
			return "", err
		}
		out := make([]messageSummary, 0, len(msgs))
		for _, msg := range msgs {
			out = append(out, s.summarizeMessage(msg))
		}
		return marshalToolResult(map[string]any{
			"channel_id": s.cfg.ChannelID,
			"messages":   out,
		})
	case "list_notifications":
		kind := stringArg(req.Arguments, "kind", "")
		limit := intArg(req.Arguments, "limit", 20)
		events, err := s.client.ListEvents(ctx, "", "", "", "", limit)
		if err != nil {
			return "", err
		}
		out := make([]notificationSummary, 0, len(events))
		for _, event := range events {
			summary, ok := s.summarizeEventNotification(event)
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
	case "wait_for_notification":
		wantKind := stringArg(req.Arguments, "kind", "")
		wantPreview := stringArg(req.Arguments, "body_preview", "")
		wantEmail := stringArg(req.Arguments, "from_email", s.cfg.PeerUserEmail)
		timeout := time.Duration(intArg(req.Arguments, "timeout_seconds", 30)) * time.Second
		pollInterval := time.Duration(intArg(req.Arguments, "poll_interval_ms", 500)) * time.Millisecond

		cursor, err := s.currentEventCursor(ctx)
		if err != nil {
			return "", err
		}
		deadline := time.Now().Add(timeout)
		for {
			page, err := s.client.ListEventPage(ctx, cursor, "", "", "", 50)
			if err != nil {
				return "", err
			}
			for _, event := range page.Items {
				summary, ok := s.summarizeEventNotification(event)
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
	case "wait_for_message":
		wantText := stringArg(req.Arguments, "text", "")
		wantEmail := stringArg(req.Arguments, "from_email", s.cfg.PeerUserEmail)
		timeout := time.Duration(intArg(req.Arguments, "timeout_seconds", 30)) * time.Second
		pollInterval := time.Duration(intArg(req.Arguments, "poll_interval_ms", 500)) * time.Millisecond

		deadline := time.Now().Add(timeout)
		for {
			msgs, err := s.client.ListMessages(ctx, s.cfg.ChannelID, 50)
			if err != nil {
				return "", err
			}
			for _, msg := range msgs {
				summary := s.summarizeMessage(msg)
				if summary.UserID == s.cfg.UserID || msg.IsDeleted || msg.ThreadTS != nil {
					continue
				}
				if wantText != "" && summary.Text != wantText {
					continue
				}
				if wantEmail != "" && summary.SenderEmail != wantEmail {
					continue
				}
				s.logger.Info("matched teraslack message", "channel_id", s.cfg.ChannelID, "sender_email", summary.SenderEmail, "text", summary.Text, "ts", summary.TS)
				return marshalToolResult(map[string]any{
					"status":     "received",
					"channel_id": s.cfg.ChannelID,
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
	default:
		return "", fmt.Errorf("unknown tool %q", req.Name)
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

func (s *Server) currentEventCursor(ctx context.Context) (string, error) {
	cursor := ""
	for {
		page, err := s.client.ListEventPage(ctx, cursor, "", "", "", 100)
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

func (s *Server) summarizeMessage(msg domain.Message) messageSummary {
	email := msg.UserID
	switch msg.UserID {
	case s.cfg.UserID:
		email = s.cfg.UserEmail
	case s.cfg.PeerUserID:
		if s.cfg.PeerUserEmail != "" {
			email = s.cfg.PeerUserEmail
		}
	}
	return messageSummary{
		TS:          msg.TS,
		Text:        msg.Text,
		UserID:      msg.UserID,
		SenderEmail: email,
	}
}

func (s *Server) summarizeEventNotification(event domain.ExternalEvent) (notificationSummary, bool) {
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
		if payload.ChannelID == s.cfg.ChannelID {
			summary.Kind = "direct_message"
			summary.Title = "New direct message"
			summary.Reason = "dm_recipient"
		}
	case domain.EventTypeFileShared:
		var payload struct {
			FileID    string `json:"file_id"`
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
		if payload.UserID != s.cfg.UserID {
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

	email := summary.ActorID
	switch summary.ActorID {
	case s.cfg.UserID:
		email = s.cfg.UserEmail
	case s.cfg.PeerUserID:
		if s.cfg.PeerUserEmail != "" {
			email = s.cfg.PeerUserEmail
		}
	}
	summary.ActorEmail = email
	return summary, true
}

func marshalToolResult(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	return string(data), nil
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
