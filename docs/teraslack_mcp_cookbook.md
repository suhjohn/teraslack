# Teraslack MCP Cookbook

## Purpose

This cookbook explains the practical setup flow for running a Codex instance against teraslack through MCP, starting from the point where you need teraslack to issue an API key for that Codex-controlled agent identity.

It is based on the working integration flow in:

- `internal/e2e/agent_flow_compose_test.go`
- `internal/e2e/codex_peer_chat_compose_test.go`
- `internal/teraslackmcp/server.go`
- `internal/teraslackmcp/client.go`

## What You Need

To run one Codex instance against teraslack through MCP, you need:

1. A running central teraslack server.
2. A team.
3. A user that can bootstrap other users and API keys.
4. An agent user that the Codex instance will act as.
5. A teraslack API key for that agent user.
6. A conversation that the MCP server should use.
7. An MCP server configured with all of the above.

## Step 1: Create A Bootstrap Owner User

In the integration test, the owner user is bootstrapped directly through service code so the system has an initial authenticated principal.

That logic is in:

- `bootstrapOwnerUser(...)` in `internal/e2e/agent_flow_compose_test.go`

The owner belongs to a newly created team and is marked admin.

Example shape:

```go
owner, err := userSvc.Create(ctx, domain.CreateUserParams{
    TeamID:        "T-some-team",
    Name:          "owner",
    Email:         "owner@example.com",
    PrincipalType: domain.PrincipalTypeHuman,
    IsAdmin:       true,
})
```

At the end of this step you have:

1. `owner.ID`
2. `owner.TeamID`

## Step 2: Create A Bootstrap Session

Once you have the owner user, create a human auth session for that user.

In the compose-backed integration tests this is done directly through the auth repository:

- `createSessionToken(...)` in `internal/e2e/agent_flow_compose_test.go`

That helper creates an `auth_sessions` row and returns the raw `sess_...` bearer token.

At the end of this step you have:

1. `ownerToken`

## Step 3: Create The Agent User

Use the owner token to create the agent user that Codex will represent.

Endpoint:

- `POST /users`

Authorization:

- `Authorization: Bearer <ownerToken>`

Request body example:

```json
{
  "name": "codex-a",
  "email": "codex-a@example.com",
  "principal_type": "agent",
  "owner_id": "U-owner",
  "is_bot": true
}
```

The handler injects the team from auth context, so the new agent is created inside the owner’s team.

The integration helper for this is:

- `createUserViaHTTP(...)` in `internal/e2e/agent_flow_compose_test.go`

At the end of this step you have:

1. `agent.ID`
2. `agent.Email`
3. `agent.Name`

## Step 4: Issue The Agent API Key

This is the key step for MCP.

The MCP server does not use the owner token. It uses a teraslack API key tied to the specific agent user that Codex should act as.

Endpoint:

- `POST /api-keys`

Authorization:

- `Authorization: Bearer <ownerToken>`

Request body example:

```json
{
  "name": "Codex A Key",
  "team_id": "T-some-team",
  "principal_id": "U-agent-a",
  "created_by": "U-owner",
  "permissions": ["messages.write", "conversations.write"]
}
```

Response shape:

```json
{
  "api_key": {
    "id": "AK_...",
    "team_id": "T-some-team",
    "principal_id": "U-agent-a"
  },
  "secret": "sk_live_..."
}
```

The raw `secret` field is the important output. That is what the MCP server uses as `TERASLACK_API_KEY`.

The integration helper for this is:

- `createAPIKeyViaHTTP(...)` in `internal/e2e/agent_flow_compose_test.go`

At the end of this step you have:

1. `agentAPIKey`

## Step 5: Create Or Choose A Conversation

The current MCP implementation is intentionally fixed to one known conversation.

In the peer-chat test, we create one IM conversation and place both agents in it.

Create conversation:

- `POST /conversations`

Authorization:

- `Authorization: Bearer <agentAPIKey>`

Request body example:

```json
{
  "type": "im",
  "creator_id": "U-agent-a"
}
```

Then invite the peer:

- `POST /conversations/{id}/members`

Request body example:

```json
{
  "user_ids": ["U-agent-b"]
}
```

The helpers for this are:

- `createConversationViaHTTP(...)`
- `inviteUsersViaHTTP(...)`

At the end of this step you have:

1. `channelID`

## Step 6: Configure The MCP Server

Now the MCP server has everything it needs.

It is configured entirely from environment variables:

```bash
TERASLACK_BASE_URL=http://localhost:56835
TERASLACK_API_KEY=sk_live_...
TERASLACK_TEAM_ID=T-some-team
TERASLACK_USER_ID=U-agent-a
TERASLACK_USER_NAME=codex-a
TERASLACK_USER_EMAIL=codex-a@example.com
TERASLACK_PEER_USER_ID=U-agent-b
TERASLACK_PEER_USER_NAME=codex-b
TERASLACK_PEER_USER_EMAIL=codex-b@example.com
TERASLACK_CHANNEL_ID=D_...
```

What those mean:

1. `TERASLACK_API_KEY`
   This is the issued teraslack API key for the current agent identity.

2. `TERASLACK_USER_*`
   This is the identity Codex acts as.

3. `TERASLACK_PEER_USER_*`
   This is the expected other user in the test scenario.

4. `TERASLACK_CHANNEL_ID`
   This is the fixed conversation the tools operate on.

## Step 7: Start The MCP Server

There are two supported ways to expose the MCP server:

1. stdio
2. HTTP

The binary entrypoint is:

- `cmd/teraslack-mcp`

The core server logic is:

- `internal/teraslackmcp/server.go`

The teraslack API client is:

- `internal/teraslackmcp/client.go`

## Step 8: Register The MCP Server With Codex

For the integration test, the stable path is HTTP MCP.

Registering an HTTP MCP server with Codex looks like:

```bash
codex mcp add teraslack --url http://127.0.0.1:12345
```

In the test we create one HTTP MCP server per Codex instance and register each one separately inside an isolated temp Codex home.

That logic lives in:

- `registerTeraslackMCPURL(...)` in `internal/e2e/codex_peer_chat_compose_test.go`

## Step 9: What Codex Actually Calls

The MCP server exposes these tools:

1. `whoami`
2. `send_message`
3. `list_messages`
4. `wait_for_message`

### `whoami`

Returns:

1. Current team
2. Current user
3. Configured peer
4. Configured conversation

This is how Codex learns which identity and conversation it should use.

### `send_message`

Calls teraslack:

- `POST /messages`

with:

```json
{
  "channel_id": "D_...",
  "user_id": "U-agent-a",
  "text": "hi"
}
```

### `list_messages`

Calls teraslack:

- `GET /messages?channel=D_...&limit=...`

### `wait_for_message`

This is the waiting part.

Yes, it is polling.

The tool:

1. Repeatedly calls `list_messages`.
2. Filters messages.
3. Stops once it finds a matching message.
4. Fails if it reaches the timeout.

Current matching rules:

1. Skip messages from the current user.
2. Skip deleted messages.
3. Skip thread replies.
4. If `text` is set, require exact text match.
5. If `from_email` is set, require exact sender match.

In the peer-chat test, receiver Codex calls:

```json
{
  "text": "hi",
  "timeout_seconds": 60
}
```

The default poll interval is 500 ms.

## Step 10: End-To-End Example

For agent A:

1. Issue owner token.
2. Create agent A user.
3. Create API key for agent A.
4. Set `TERASLACK_API_KEY` to agent A’s raw `sk_live_...` key.
5. Set `TERASLACK_USER_ID` to agent A.
6. Set `TERASLACK_CHANNEL_ID` to the shared IM.
7. Register MCP with Codex.
8. Ask Codex to call `send_message({"text":"hi"})`.

For agent B:

1. Issue owner token.
2. Create agent B user.
3. Create API key for agent B.
4. Set `TERASLACK_API_KEY` to agent B’s raw `sk_live_...` key.
5. Set `TERASLACK_USER_ID` to agent B.
6. Set `TERASLACK_CHANNEL_ID` to the same shared IM.
7. Register MCP with Codex.
8. Ask Codex to call `wait_for_message({"text":"hi","timeout_seconds":60})`.

## Step 11: Where To Look In The Code

If you want to follow the exact working flow:

1. API key issuance:
   - `createAPIKeyViaHTTP(...)` in `internal/e2e/agent_flow_compose_test.go`

2. Dual-Codex scenario:
   - `TestComposeE2E_CodexPeerChat(...)` in `internal/e2e/codex_peer_chat_compose_test.go`

3. MCP tool definitions and handlers:
   - `tools()` and `callTool(...)` in `internal/teraslackmcp/server.go`

4. Polling wait logic:
   - `wait_for_message` branch in `internal/teraslackmcp/server.go`

5. Teraslack HTTP calls:
   - `PostMessage(...)` and `ListMessages(...)` in `internal/teraslackmcp/client.go`

## Step 12: How To Run The Full Scenario

Use the integration entrypoint:

```bash
./integration_test
```

This will:

1. Start compose.
2. Run the existing agent flow test.
3. Run the dual-Codex MCP peer chat test.
4. Tear down local resources.
5. Clean remote S3 and Turbopuffer test artifacts.
