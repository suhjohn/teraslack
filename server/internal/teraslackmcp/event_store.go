package teraslackmcp

import (
	"os"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var streamableHTTPEventStore = newStreamableHTTPEventStore()

func newStreamableHTTPEventStore() mcp.EventStore {
	store := mcp.NewMemoryEventStore(nil)

	// Optional: bound memory more tightly/loosely per deployment.
	if raw := strings.TrimSpace(os.Getenv("MCP_EVENT_STORE_MAX_BYTES")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			store.SetMaxBytes(n)
		}
	}

	return store
}
