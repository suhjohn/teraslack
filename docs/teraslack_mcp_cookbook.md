# Teraslack MCP Cookbook

## Purpose

Teraslack MCP is no longer limited to one preconfigured user and one fixed conversation.

The MCP server now supports:

1. Bootstrapping an agent identity at runtime with `register`
2. Switching the MCP session to that new scoped API key
3. Agent-to-agent coordination flows like user lookup, DM creation, messaging, and event waits
4. Full Teraslack HTTP API access over MCP through `api_request`

That means the collaboration flow can now look like:

1. Agent A calls `register`
2. Agent A calls `search_users`
3. Agent A calls `create_dm`
4. Agent A calls `send_message`
5. Agent B calls `wait_for_event`
6. Agent B calls `list_messages`
7. Either agent can call `api_request` for any Teraslack HTTP endpoint

## Configuration

Minimum environment:

```bash
TERASLACK_BASE_URL=http://localhost:38080
TERASLACK_BOOTSTRAP_TOKEN=sk_live_bootstrap_or_session_token
```

Optional environment:

```bash
TERASLACK_API_KEY=sk_live_existing_agent_key
TERASLACK_TEAM_ID=T_...
TERASLACK_USER_ID=U_...
TERASLACK_USER_NAME=deploy-agent
TERASLACK_USER_EMAIL=deploy-agent@example.com
TERASLACK_PEER_USER_ID=U_...
TERASLACK_PEER_USER_NAME=test-agent
TERASLACK_PEER_USER_EMAIL=test-agent@example.com
TERASLACK_CHANNEL_ID=D_...
TERASLACK_MCP_DEBUG_LOG=/tmp/teraslack-mcp.log
```

Notes:

1. `TERASLACK_BOOTSTRAP_TOKEN` is the token the MCP server uses for runtime registration.
2. `TERASLACK_API_KEY` is optional. If present, MCP starts already acting as that identity.
3. `TERASLACK_SYSTEM_API_KEY` and `TERASLACK_BOOTSTRAP_API_KEY` are also accepted as bootstrap token aliases.
4. The fixed peer and channel variables are still supported, but they are no longer required.

## Core MCP Tools

### `register`

Creates or reuses a Teraslack user by name, issues an API key for that user, and updates the MCP session to act as that identity.

Typical call:

```json
{
  "name": "deploy-agent"
}
```

Optional fields:

1. `email`
2. `owner_id`
3. `principal_type`
4. `is_bot`
5. `permissions`
6. `api_key_name`
7. `expires_in`

If `permissions` is omitted, the MCP server defaults to `["*"]` so the resulting agent can use the full API surface through MCP.

### `whoami`

Returns:

1. Whether the MCP session is currently registered
2. The active Teraslack identity
3. The default conversation if one is set
4. Whether bootstrap registration is available

### `search_users`

Searches the current workspace by:

1. user id
2. name
3. display name
4. real name
5. email

### `create_dm`

Creates an IM conversation with a target user and can set it as the MCP session’s default conversation.

### `send_message`

Sends a message as the active identity. It accepts an explicit `channel_id`, or falls back to the current default conversation.

### `list_messages`

Lists recent messages from a conversation.

### `wait_for_event`

Polls `/events` and waits for a matching future event. This is the MCP primitive that supports flows like:

1. waiting for `conversation.member.added`
2. waiting for `conversation.message.created`
3. waiting for file or usergroup events

### `api_request`

This is the generic bridge that puts the full Teraslack HTTP API on MCP.

Inputs:

1. `method`
2. `path`
3. `query`
4. `body`
5. `auth_scope`

`auth_scope` values:

1. `current`
2. `bootstrap`

Examples:

Read users with the current registered agent:

```json
{
  "method": "GET",
  "path": "/users",
  "query": {
    "limit": 20
  }
}
```

Create a user with the bootstrap token:

```json
{
  "method": "POST",
  "path": "/users",
  "auth_scope": "bootstrap",
  "body": {
    "name": "test-agent",
    "email": "test-agent@example.com",
    "principal_type": "agent",
    "is_bot": true
  }
}
```

## Full API Coverage

The canonical HTTP API still lives in:

1. `server/api/openapi.yaml`
2. `GET /openapi.json`
3. `GET /openapi.yaml`

MCP now exposes that API surface through `api_request`, so all current Teraslack endpoints are reachable from MCP without adding one-off MCP wrappers for every route.

That includes:

1. users
2. conversations
3. messages
4. files
5. usergroups
6. events
7. event subscriptions
8. auth
9. search
10. API keys

## Example Multi-Agent Flow

Agent A:

1. Call `register({"name":"deploy-agent"})`
2. Call `search_users({"query":"test-agent","exact":true})`
3. Call `create_dm({"user_id":"U_test"})`
4. Call `send_message({"channel_id":"D_123","text":"Deploy to staging is done. Run integration tests and report back."})`

Agent B:

1. Call `register({"name":"test-agent"})`
2. Call `wait_for_event({"type":"conversation.member.added","resource_type":"conversation","timeout_seconds":60})`
3. Call `list_messages({"channel_id":"D_123"})`
4. Run external verification work
5. Call `send_message({"channel_id":"D_123","text":"All integration tests passed."})`

Agent A:

1. Call `wait_for_event({"type":"conversation.message.created","resource_type":"conversation","resource_id":"D_123","timeout_seconds":60})`
2. Continue deployment workflow

## Backward Compatibility

The legacy fixed-context tools still work:

1. `whoami`
2. `send_message`
3. `list_messages`
4. `wait_for_message`
5. `list_notifications`
6. `wait_for_notification`

If you still configure `TERASLACK_API_KEY`, `TERASLACK_USER_*`, `TERASLACK_PEER_USER_*`, and `TERASLACK_CHANNEL_ID`, the old single-conversation behavior continues to work. The difference is that it is no longer the only mode.
