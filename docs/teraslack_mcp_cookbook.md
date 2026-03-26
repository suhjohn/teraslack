# Teraslack MCP Cookbook

## Purpose

Teraslack MCP is a remote protected MCP server with OAuth-backed access.

The MCP server supports:

1. Remote Streamable HTTP transport on `/mcp`
2. OAuth authorization code + PKCE through the API service
3. Fresh session-agent provisioning when a new MCP session starts, owned by the approving human user
4. Full Teraslack HTTP API access over MCP through `api_request`
5. Optional bootstrap-only operations for local stdio or other explicit system-key flows

That means the collaboration flow can now look like:

1. Claude Code completes OAuth approval as the human owner
2. A new MCP session starts
3. Teraslack creates a fresh session agent owned by that user
4. The MCP session calls `search_users`
5. The MCP session calls `create_dm`
6. The MCP session calls `send_message`
7. Another MCP session can either use its own fresh session agent or explicitly switch to a shared durable identity

## Transport And Auth Notes

The Teraslack MCP server now uses the official Go MCP SDK.

That means:

1. stdio uses the SDK's standard newline-delimited MCP transport
2. HTTP uses the SDK's Streamable HTTP transport on `/mcp`
3. MCP session state such as `register`, default conversation, and conversation subscriptions is scoped to the MCP session, not shared process-wide
4. Remote HTTP clients should expect normal MCP session behavior, including `Mcp-Session-Id` headers and `GET`/`POST`/`DELETE` support on the MCP endpoint
5. Teraslack can push new incoming messages via standard MCP logging notifications (`notifications/message`). Streaming requires a conversation ID: set a default conversation for the MCP session (via `create_dm` or `send_message`) or set `TERASLACK_CHANNEL_ID` on the server. The server defaults the session log level to `info` so clients do not need to call `logging/setLevel` unless they want a different level. To reply, use `send_message` with the `conversation_id` from the notification metadata.
6. The MCP deployment is the protected resource server; the API deployment is the OAuth authorization server
7. Clients authenticate to `/mcp` with OAuth access tokens, not raw Teraslack API keys

## Configuration

Minimum environment for a hosted remote MCP server:

```bash
TERASLACK_BASE_URL=http://localhost:38080
MCP_BASE_URL=http://localhost:8090/mcp
```

In remote HTTP mode, clients obtain OAuth access tokens from the API service and send
`Authorization: Bearer <oauth-access-token>` to `/mcp`.

Optional environment:

```bash
MCP_OAUTH_SIGNING_KEY=replace_with_shared_signing_key
MCP_KEEPALIVE_SECONDS=0
MCP_SSE_HEARTBEAT_SECONDS=25
MCP_EVENT_STORE_MAX_BYTES=10485760
TERASLACK_API_KEY=sk_live_existing_agent_key
TERASLACK_WORKSPACE_ID=T_...
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

1. `MCP_BASE_URL` is the canonical protected-resource URL clients request tokens for.
2. `MCP_OAUTH_SIGNING_KEY` is optional if both the API and MCP services share the same `ENCRYPTION_KEY`.
3. Hosted remote HTTP MCP does not need `TERASLACK_SYSTEM_API_KEY` for default OAuth-backed session-agent provisioning.
4. `TERASLACK_SYSTEM_API_KEY` is optional. It is only used for explicit local/bootstrap-style flows such as `register` or `api_request` with `auth_scope=bootstrap`.
5. `TERASLACK_API_KEY` is optional. If present, stdio or fixed-identity deployments can start already acting as that identity.
6. The fixed peer and channel variables are still supported, but they are no longer required.

## OAuth Discovery

Authorization server metadata:

```text
GET https://api.example.com/.well-known/oauth-authorization-server
```

Protected resource metadata:

```text
GET https://mcp.example.com/.well-known/oauth-protected-resource
GET https://mcp.example.com/.well-known/oauth-protected-resource/mcp
```

Important details:

1. Authorization uses the authorization code flow with PKCE.
2. The auth server supports client ID metadata documents.
3. Dynamic client registration is not implemented.
4. Tokens must include the MCP resource URI in the OAuth `resource` parameter.
5. Tokens presented to `/mcp` must include the `mcp:tools` scope.
6. Bootstrap-only operations are separate from the hosted OAuth-backed HTTP flow.
7. Loopback redirect URIs for native clients are supported. For registered localhost callbacks such as `http://localhost/callback` or `http://127.0.0.1/callback`, the auth server also accepts the same host and path with a client-chosen loopback port.
8. Approving OAuth access authenticates the human owner. Clients should call `whoami` with a client-provided `session_id` (unique per Claude Code / Codex / agent run) to provision or reuse a per-client session agent owned by that human.

## Core MCP Tools

### `register`

Creates or reuses a Teraslack user by name, issues an API key for that user, and updates the MCP session to act as that identity.

This creates or reuses an agent identity and switches the MCP session to act as that agent.

In OAuth-backed HTTP mode, the new agent is created as an agent owned by the approving human user. Clients can either call `register` directly to choose a durable agent name, or call `whoami` with `session_id` to provision a per-client session agent automatically.

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

In OAuth-backed HTTP mode, `whoami` accepts an optional `session_id`. If `session_id` is provided, the server provisions or reuses a session agent for that client session and switches the MCP session to operate as that agent. If `session_id` is omitted, `whoami` reports whatever identity the MCP session is currently using, and still includes the approving human owner separately.

Returns:

1. Whether the MCP session is currently registered
2. The active Teraslack identity
3. The default conversation if one is set
4. The human owner for the session, when present
5. The session-agent identity (provisioned via `whoami` with `session_id`), when present
6. Whether bootstrap registration is available

### `list_owned_identities`

Lists agent identities owned by the approving human for the current MCP session.

Useful when you want:

1. one terminal to keep its own session agent identity
2. another terminal to intentionally switch to a shared durable agent

### `switch_identity`

Switches the current MCP session to an existing owned agent identity.

You can identify the target by:

1. `user_id`
2. `name`
3. `email`

This lets two terminals intentionally converge on the same reusable identity.

### `reset_identity`

Resets the current MCP session back to its session agent identity (the one created via `whoami` with `session_id`).

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

Sends a message as the active identity. It accepts an explicit `conversation_id` (preferred; `channel_id` is accepted as an alias), or falls back to the current default conversation.

### `list_messages`

Lists recent messages from a conversation.

### `wait_for_message`

Waits for the next matching top-level message in a conversation.

By default it is future-only: existing history is ignored unless `include_existing` is set to `true`.

Useful filters:

1. `text`
2. `contains_text`
3. `from_email`
4. `from_user_id`
5. `include_existing`

### `wait_for_event`

Polls `/events` and waits for a matching future event. This is the MCP primitive that supports flows like:

1. waiting for `conversation.member.added`
2. waiting for `conversation.message.created`
3. waiting for file or usergroup events

### `subscribe_conversation`

Creates a future-only cursor for a conversation so an agent can consume the next matching event without rereading history.

Typical call:

```json
{
  "conversation_id": "D_123"
}
```

Returns:

1. `subscription_id`
2. `conversation_id`
3. `after_event_id`

### `next_event`

Consumes the next matching event from a prior `subscribe_conversation` cursor.

Useful filters:

1. `event_type`
2. `from_user_id`
3. `from_email`
4. `text`
5. `contains_text`
6. `include_self`
7. `timeout_seconds`
8. `poll_interval_ms`

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

Create a user with the bootstrap token in a local/bootstrap-style flow:

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
4. Call `send_message({"conversation_id":"D_123","text":"Deploy to staging is done. Run integration tests and report back."})`

Agent B:

1. Call `register({"name":"test-agent"})`
2. Call `wait_for_event({"type":"conversation.member.added","resource_type":"conversation","timeout_seconds":60})`
3. Call `subscribe_conversation({"conversation_id":"D_123"})`
4. Call `next_event({"subscription_id":"sub_001","event_type":"conversation.message.created","from_email":"deploy-agent@example.com","timeout_seconds":60})`
5. Run external verification work
6. Call `send_message({"conversation_id":"D_123","text":"All integration tests passed."})`

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
