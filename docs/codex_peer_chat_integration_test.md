# Codex Peer Chat Integration Test

## Overview

This document describes the compose-backed integration scenario where:

1. One Codex instance runs with a teraslack MCP server.
2. A second Codex instance runs with a different teraslack MCP server.
3. Both MCP servers connect to the same central teraslack server.
4. Both Codex instances operate as different agent users on the same teraslack workspace.
5. One Codex instance sends the exact message `hi`.
6. The other Codex instance receives that message and reports it.

This scenario is implemented in:

- `internal/e2e/codex_peer_chat_compose_test.go`
- `internal/teraslackmcp/client.go`
- `internal/teraslackmcp/server.go`

It runs as part of:

- `./integration_test`
- `scripts/integration_test.sh`

## What The Test Proves

The test verifies all of the following in one end-to-end flow:

1. The central teraslack server can host a shared workspace for multiple agent users.
2. Two independent Codex processes can be configured with separate MCP endpoints.
3. Each Codex process can use its MCP server as a different teraslack identity.
4. One Codex process can send a real teraslack message into a shared conversation.
5. The other Codex process can observe that real message through MCP.
6. The teraslack database and message history reflect the same message that Codex reported.

## High-Level Architecture

The test has five moving parts:

1. The compose stack.
   This brings up Postgres, the teraslack API server, the external event projector, the webhook producer, the webhook worker, and the indexer.

2. The central teraslack API.
   This is the shared backend both Codex instances talk to indirectly through MCP.

3. Two teraslack MCP servers.
   These are lightweight adapters that expose a small MCP tool surface on top of the teraslack HTTP API.

4. Two isolated Codex processes.
   Each Codex process gets its own temporary home directory and its own MCP registration so it behaves like an independent client.

5. The Go test harness.
   This bootstraps data, starts the MCP servers, launches Codex, and asserts the result.

## The Scenario We Test

The test scenario is:

1. Start the central teraslack stack with `docker compose`.
2. Wait for the API to become healthy.
3. Create one workspace owner directly against teraslack.
4. Create two agent users on the same workspace.
5. Create one API key for agent A.
6. Create one API key for agent B.
7. Create one IM conversation.
8. Add both agents to that conversation.
9. Start one MCP server configured as agent A.
10. Start one MCP server configured as agent B.
11. Start receiver Codex first.
12. Instruct receiver Codex to wait for the exact message `hi`.
13. Start sender Codex second.
14. Instruct sender Codex to send the exact message `hi`.
15. Wait for sender Codex to finish and report success.
16. Wait for receiver Codex to finish and report the received message.
17. Query teraslack message history directly.
18. Assert that exactly one top-level message exists in the conversation.
19. Assert that the stored message text is `hi`.
20. Assert that the stored sender is agent A.

## How The MCP Server Works In This Test

Each MCP server is configured with fixed identity and conversation context:

- `TERASLACK_BASE_URL`
- `TERASLACK_API_KEY`
- `TERASLACK_WORKSPACE_ID`
- `TERASLACK_USER_ID`
- `TERASLACK_USER_NAME`
- `TERASLACK_USER_EMAIL`
- `TERASLACK_PEER_USER_ID`
- `TERASLACK_PEER_USER_NAME`
- `TERASLACK_PEER_USER_EMAIL`
- `TERASLACK_CHANNEL_ID`

That means each MCP server already knows:

1. Which teraslack user it represents.
2. Which peer user the test is about.
3. Which conversation the message exchange should happen in.

The MCP server exposes only the tools needed for this scenario:

1. `whoami`
   Returns the configured current user, peer user, workspace, and conversation.

2. `send_message`
   Sends a message to the configured conversation as the configured current user.

3. `list_messages`
   Returns recent messages from the configured conversation.

4. `wait_for_message`
   Polls teraslack until a matching message appears or a timeout is hit.

## Why The Test Uses HTTP MCP Endpoints

The MCP implementation supports stdio and HTTP. The integration test uses HTTP MCP endpoints.

Reason:

1. Codex CLI can register a streamable HTTP MCP server directly with `codex mcp add --url`.
2. In practice, the local Codex CLI repeatedly timed out during stdio MCP startup for this server even though the raw stdio handshake itself was valid.
3. The HTTP path avoids that startup deadlock and gives deterministic integration behavior.

The test therefore creates two in-process HTTP MCP servers with `httptest.NewServer(...)` and registers those URLs with the two isolated Codex homes.

## How The Two Codex Instances Are Isolated

The test does not reuse the user's real Codex home directly.

Instead it:

1. Creates a temporary home directory for sender Codex.
2. Creates a temporary home directory for receiver Codex.
3. Copies `~/.codex/auth.json` into each temp home so both processes can authenticate with the Codex service.
4. Writes a minimal temp `config.toml` in each home.
5. Registers a different `teraslack` MCP endpoint in each temp home.

This makes the two Codex runs independent:

1. Sender Codex sees only sender MCP.
2. Receiver Codex sees only receiver MCP.

## Exact Prompt Behavior

The prompts are intentionally narrow so the test measures transport and integration, not agent creativity.

Receiver Codex is told to:

1. Call `whoami`.
2. Call `wait_for_message` with `{"text":"hi","timeout_seconds":60}`.
3. Return strict JSON only.

Sender Codex is told to:

1. Call `whoami`.
2. Call `send_message` exactly once with `{"text":"hi"}`.
3. Return strict JSON only.

## What The Test Asserts

After both Codex processes exit, the test checks:

1. Sender JSON reports:
   - `status = "sent"`
   - sender email matches agent A
   - `sent_text = "hi"`
   - channel matches the configured IM

2. Receiver JSON reports:
   - `status = "received"`
   - receiver email matches agent B
   - sender email matches agent A
   - `received_text = "hi"`
   - channel matches the configured IM

3. Direct teraslack history reports:
   - exactly one top-level message
   - text is `hi`
   - sender user ID is agent A

## Where It Runs

The top-level integration entrypoint now runs both compose-backed end-to-end scenarios:

1. `TestComposeE2E_AgentSessionFlow`
2. `TestComposeE2E_CodexPeerChat`

This is triggered by:

```bash
./integration_test
```

The script:

1. Generates per-run ports and prefixes.
2. Starts the compose stack.
3. Waits for health.
4. Runs the Go end-to-end tests.
5. Tears down containers, network, and volume.
6. Cleans remote S3 and Turbopuffer test data.

## Expected Outcome

If everything works, the scenario demonstrates a real cross-agent message exchange:

1. Codex instance A uses MCP to send `hi`.
2. The central teraslack server stores that message.
3. Codex instance B uses MCP to observe that same stored message.
4. The Go test confirms that Codex-reported results and teraslack-recorded state agree.
