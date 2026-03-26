package teraslackmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"
	"unsafe"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/suhjohn/teraslack/internal/domain"
)

// channelNotifier sends arbitrary JSON-RPC notifications through a ServerSession.
//
// The Go MCP SDK (v1.4.1) does not expose a public method for sending custom
// notifications — only NotifyProgress and Log are available. Channel events
// require the method "notifications/claude/channel", so we access the session's
// underlying jsonrpc2.Connection (unexported) via reflect+unsafe and call its
// public Notify method.
type channelNotifier interface {
	Notify(ctx context.Context, method string, params any) error
}

func sessionNotifier(ss *mcp.ServerSession) (channelNotifier, error) {
	field, ok := reflect.TypeOf(ss).Elem().FieldByName("conn")
	if !ok {
		return nil, fmt.Errorf("ServerSession has no conn field")
	}
	ptr := unsafe.Add(unsafe.Pointer(ss), field.Offset)
	val := reflect.NewAt(field.Type, ptr).Elem()
	n, ok := val.Interface().(channelNotifier)
	if !ok {
		return nil, fmt.Errorf("conn does not implement channelNotifier")
	}
	return n, nil
}

// channelEvent is the JSON-RPC notification payload for "notifications/claude/channel".
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

	var cursor string

	// Grab initial cursor so we only see messages arriving after startup.
	if err := s.initChannelCursor(ctx, &cursor); err != nil {
		s.logger.Warn("channel: failed to get initial cursor", "error", err)
	}

	s.logger.Info("channel: polling started", "cursor", cursor)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollChannelEvents(ctx, &cursor)
		}
	}
}

func (s *Server) initChannelCursor(ctx context.Context, cursor *string) error {
	// Use the first available session's client to get the initial cursor.
	client, _, err := s.anySessionClient(ctx)
	if err != nil {
		return err
	}
	page, err := client.ListEventPage(ctx, "", domain.EventTypeConversationMessageCreated, "", "", 1)
	if err != nil {
		return err
	}
	if len(page.Items) > 0 {
		*cursor = strconv.FormatInt(page.Items[0].ID, 10)
	}
	return nil
}

func (s *Server) pollChannelEvents(ctx context.Context, cursor *string) {
	client, selfUserID, err := s.anySessionClient(ctx)
	if err != nil {
		return // no session ready yet
	}

	channelID := s.cfg.ChannelID

	page, err := client.ListEventPage(ctx, *cursor, domain.EventTypeConversationMessageCreated, domain.ResourceTypeConversation, channelID, 50)
	if err != nil {
		s.logger.Warn("channel: poll error", "error", err)
		return
	}

	for _, event := range page.Items {
		*cursor = strconv.FormatInt(event.ID, 10)

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

		s.pushChannelNotification(ctx, channelEvent{
			Content: payload.Text,
			Meta:    meta,
		})
	}

	if page.NextCursor != "" && page.NextCursor != *cursor {
		*cursor = page.NextCursor
	}
}

func (s *Server) pushChannelNotification(ctx context.Context, event channelEvent) {
	for session := range s.sdkServer.Sessions() {
		notifier, err := sessionNotifier(session)
		if err != nil {
			s.logger.Warn("channel: cannot get notifier", "error", err)
			continue
		}
		if err := notifier.Notify(ctx, "notifications/claude/channel", event); err != nil {
			s.logger.Warn("channel: notify error", "error", err)
		}
	}
}

// anySessionClient returns a teraslack API client and user ID from the first
// available session. Used by the channel loop to poll events without being
// tied to a specific tool call context.
func (s *Server) anySessionClient(ctx context.Context) (*Client, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, data := range s.sessions {
		if data.current.client != nil && data.current.UserID != "" {
			return data.current.client, data.current.UserID, nil
		}
		if data.sessionIdentity.client != nil && data.sessionIdentity.UserID != "" {
			return data.sessionIdentity.client, data.sessionIdentity.UserID, nil
		}
	}
	return nil, "", fmt.Errorf("no active session")
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
