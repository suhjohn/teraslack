# Teraslack Stdio MCP

This is the local API-key based MCP server for Teraslack. It is separate from the hosted OAuth HTTP MCP server.

Use it when:

1. You want a simple stdio MCP server for Claude Code or Codex.
2. You already have a Teraslack API key.
3. You do not want the OAuth protected-resource flow.

## One-command install

For the hosted Teraslack deployment, the intended bootstrap path is:

```bash
curl https://teraslack.ai/install.sh | sh
```

That flow:

1. Opens the browser for Teraslack login and approval.
2. Lets the user approve the install for a workspace, with the current workspace preselected.
3. Mints a long-lived local human API key for the selected workspace.
4. Writes local config to `~/.teraslack/config.json`.
5. Downloads a prebuilt local stdio MCP binary for the current OS and architecture.
6. Registers the `teraslack` MCP server with Codex and Claude Code.

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

`api_request` is the generic escape hatch for the full HTTP API. The dedicated tools cover the common messaging flow without bringing along the OAuth-session behavior from the hosted MCP server.
