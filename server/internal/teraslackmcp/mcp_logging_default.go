package teraslackmcp

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setServerSessionLogLevelIfEmpty sets the session's log level if it is not yet set.
//
// The MCP spec has the client call logging/setLevel, and the Go MCP SDK suppresses
// notifications/message until that happens. Teraslack uses notifications/message
// for "push" of incoming conversation messages, so we default the session log
// level to "info" to make this work out-of-the-box.
//
// This uses reflect+unsafe because the SDK does not expose a public setter for
// ServerSessionState.LogLevel.
func setServerSessionLogLevelIfEmpty(ss *mcp.ServerSession, level mcp.LoggingLevel) error {
	if ss == nil {
		return nil
	}

	ssType := reflect.TypeOf(ss).Elem()

	muField, ok := ssType.FieldByName("mu")
	if !ok {
		return fmt.Errorf("mcp.ServerSession has no mu field")
	}
	stateField, ok := ssType.FieldByName("state")
	if !ok {
		return fmt.Errorf("mcp.ServerSession has no state field")
	}

	if muField.Type != reflect.TypeOf(sync.Mutex{}) {
		return fmt.Errorf("mcp.ServerSession.mu is %v, want sync.Mutex", muField.Type)
	}
	if stateField.Type != reflect.TypeOf(mcp.ServerSessionState{}) {
		return fmt.Errorf("mcp.ServerSession.state is %v, want mcp.ServerSessionState", stateField.Type)
	}

	muPtr := unsafe.Add(unsafe.Pointer(ss), muField.Offset)
	mu := (*sync.Mutex)(muPtr)

	mu.Lock()
	defer mu.Unlock()

	statePtr := unsafe.Add(unsafe.Pointer(ss), stateField.Offset)
	state := (*mcp.ServerSessionState)(statePtr)
	if state.LogLevel == "" {
		state.LogLevel = level
	}
	return nil
}
