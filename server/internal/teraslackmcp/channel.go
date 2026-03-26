package teraslackmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/suhjohn/teraslack/internal/domain"
)

type channelEvent struct {
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// startChannelLoop begins polling for new messages and pushing them as channel
// notifications to the active MCP session. It runs until ctx is cancelled.
func (s *Server) startChannelLoop(ctx context.Context) {
	// Wait briefly for the session to be established before polling.
	ticker := time.NewTicker(time.Duration(s.channelPollIntervalMS()) * time.Millisecond)
	defer ticker.Stop()

	cursors := map[string]string{}
	channelIDs := map[string]string{}
	tokens := map[string]string{}

	if s.cfg.ChannelID != "" {
		s.logger.Info("channel: polling started", "fallback_channel_id", true, "channel_id", s.cfg.ChannelID)
	} else {
		s.logger.Info("channel: polling started (requires a default conversation per MCP session)", "fallback_channel_id", false)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollSessionChannelEvents(ctx, cursors, channelIDs, tokens)
		}
	}
}

func (s *Server) pollSessionChannelEvents(ctx context.Context, cursors map[string]string, channelIDs map[string]string, tokens map[string]string) {
	for session := range s.sdkServer.Sessions() {
		sessionKey := s.sessionKeyFromSession(session)
		state, ok := s.sessionStateForPush(sessionKey)
		if !ok {
			continue
		}

		// If the session switches identity, reset the cursor because /events cursors
		// are scoped to the authenticating principal.
		if tokens[sessionKey] != state.Token {
			tokens[sessionKey] = state.Token
			delete(cursors, sessionKey)
			delete(channelIDs, sessionKey)
		}

		channelID := firstNonEmpty(state.ChannelID, s.cfg.ChannelID)
		if channelID == "" {
			const disabled = "__disabled__"
			if channelIDs[sessionKey] != disabled {
				channelIDs[sessionKey] = disabled
				delete(cursors, sessionKey)
				s.logger.Info("channel: polling disabled for session (no default conversation configured)", "session_id", session.ID())
			}
			continue
		}

		filterKey := channelID

		if channelIDs[sessionKey] != filterKey {
			channelIDs[sessionKey] = filterKey
			cursor, err := s.initialCursorForMessages(ctx, state.client, channelID)
			if err != nil {
				s.logger.Warn("channel: failed to get initial cursor", "channel_id", channelID, "error", err)
				cursor = ""
			}
			cursors[sessionKey] = cursor
		}

		cursor := cursors[sessionKey]
		next, err := s.pollChannelEventsOnce(ctx, state.client, state.UserID, channelID, cursor, func(event channelEvent) {
			s.pushChannelNotificationToSession(ctx, session, event)
		})
		if err != nil {
			s.logger.Warn("channel: poll error", "channel_id", channelID, "error", err)
			// If we got a cursor-scoped error (common when the underlying identity
			// changes or the cursor becomes invalid), force a re-init next tick.
			delete(cursors, sessionKey)
			delete(channelIDs, sessionKey)
			continue
		}
		cursors[sessionKey] = next
	}
}

func (s *Server) pollChannelEventsOnce(
	ctx context.Context,
	client *Client,
	selfUserID string,
	channelID string,
	cursor string,
	push func(channelEvent),
) (string, error) {
	if channelID == "" {
		return cursor, fmt.Errorf("channel_id is required for message polling")
	}

	page, err := client.ListEventPage(ctx, cursor, domain.EventTypeConversationMessageCreated, domain.ResourceTypeConversation, channelID, 50)
	if err != nil {
		return cursor, err
	}

	next := cursor

	for _, event := range page.Items {
		var payload struct {
			TS        string `json:"ts"`
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
			Text      string `json:"text"`
			ThreadTS  string `json:"thread_ts,omitempty"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}

		// Skip own messages.
		if payload.UserID == selfUserID {
			continue
		}

		meta := map[string]string{
			"channel_id":  event.ResourceID,
			"sender_id":   payload.UserID,
			"sender_name": s.resolveUserName(client, payload.UserID),
			"message_ts":  payload.TS,
		}
		if payload.ThreadTS != "" {
			meta["thread_ts"] = payload.ThreadTS
		}

		push(channelEvent{
			Content: payload.Text,
			Meta:    meta,
		})
	}

	if page.NextCursor != "" {
		next = page.NextCursor
	}
	return next, nil
}

func (s *Server) pushChannelNotificationToSession(ctx context.Context, session *mcp.ServerSession, event channelEvent) {
	if err := setServerSessionLogLevelIfEmpty(session, "info"); err != nil {
		s.logger.Warn("channel: failed to set default log level", "error", err)
	}
	if err := session.Log(ctx, &mcp.LoggingMessageParams{
		Level:  "info",
		Logger: "teraslack/channel",
		Data: map[string]any{
			"type":    "teraslack.channel_message",
			"content": event.Content,
			"meta":    event.Meta,
		},
	}); err != nil {
		s.logger.Warn("channel: log notify error", "error", err)
		return
	}

	s.logger.Info(
		"channel: pushed notification",
		"session_id", session.ID(),
		"channel_id", event.Meta["channel_id"],
		"sender_id", event.Meta["sender_id"],
		"message_ts", event.Meta["message_ts"],
		"content_len", len(event.Content),
	)
}

func (s *Server) sessionKeyFromSession(session *mcp.ServerSession) string {
	if session == nil {
		return "default"
	}
	if id := strings.TrimSpace(session.ID()); id != "" {
		return id
	}
	return fmt.Sprintf("stdio:%p", session)
}

func (s *Server) sessionStateForPush(sessionKey string) (sessionState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sessions == nil {
		if s.initial.client != nil && s.initial.UserID != "" && s.cfg.ChannelID != "" {
			return s.initial, true
		}
		return sessionState{}, false
	}
	data := s.sessions[sessionKey]
	if data == nil {
		if s.initial.client != nil && s.initial.UserID != "" && s.cfg.ChannelID != "" {
			return s.initial, true
		}
		return sessionState{}, false
	}
	if data.current.client != nil && data.current.UserID != "" {
		return data.current, true
	}
	if data.sessionIdentity.client != nil && data.sessionIdentity.UserID != "" {
		return data.sessionIdentity, true
	}
	return sessionState{}, false
}

// resolveUserName looks up a display name for a user ID, falling back to the ID.
func (s *Server) resolveUserName(client *Client, userID string) string {
	user, err := client.GetUser(context.Background(), userID)
	if err != nil {
		return userID
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.Name != "" {
		return user.Name
	}
	return userID
}

func (s *Server) channelPollIntervalMS() int {
	return 1000
}

func (s *Server) initialCursorForMessages(ctx context.Context, client *Client, channelID string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("client is required")
	}
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required")
	}

	page, err := client.ListEventPage(ctx, "", domain.EventTypeConversationMessageCreated, domain.ResourceTypeConversation, channelID, 1)
	if err != nil {
		return "", err
	}
	if len(page.Items) == 0 {
		return "", nil
	}
	if page.NextCursor != "" {
		return page.NextCursor, nil
	}
	return "", nil
}
