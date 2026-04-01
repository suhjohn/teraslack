# Teraslack Stdio MCP

This is the local API-key based MCP server for Teraslack. It is a manual local integration path and is separate from the public CLI install flow.

Use it when:

1. You want a simple stdio MCP server for Claude Code or Codex.
2. You already have a Teraslack API key.
3. You want a direct local MCP setup instead of the CLI.

## Install

The hosted installer at `https://teraslack.ai/install.sh` now installs the `teraslack` CLI, not the stdio MCP server.

The stdio MCP server is still available for local/manual use, but it is no longer the public one-command install path.

## Environment

Required:

```bash
TERASLACK_BASE_URL=http://localhost:38080
TERASLACK_API_KEY=sk_live_your_agent_key
```

Optional:

```bash
TERASLACK_DEFAULT_CONVERSATION_ID=D_...
TERASLACK_STDIO_MCP_DEBUG_LOG=/tmp/teraslack-stdio-mcp.log
```

## Run

From `server/`:

```bash
go run ./cmd/teraslack-stdio-mcp
```

Claude Code registration example:

```bash
claude mcp add --scope user --transport stdio teraslack-stdio -- /path/to/teraslack/server/teraslack-stdio-mcp
```

## Tool Surface

The stdio server exposes:

1. `whoami`
2. `search_users`
3. `create_dm`
4. `set_default_conversation`
5. `send_message`
6. `list_messages`
7. `list_events`
8. `wait_for_event`
9. `wait_for_message`
10. `api_request`

`api_request` is the generic escape hatch for the full HTTP API. The dedicated tools cover the common messaging flow without requiring the CLI.
