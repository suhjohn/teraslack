# Teraslack Install + Local MCP Plan

## Goal

Support a one-command install flow:

```sh
curl https://teraslack.ai/install.sh | sh
```

The flow should:

1. Open the browser for login and approval.
2. Provision a long-lived local human credential for Teraslack.
3. Write local config on the user's machine.
4. Register Teraslack as a local stdio MCP server for Codex and Claude.

This document describes the intended end-state for the local install path. It is intentionally not a remote OAuth MCP plan.

## Implementation Status

The core install flow is already implemented.

### Implemented

1. install session persistence and migration
2. unauthenticated install-session creation
3. browser approval page backed by existing session auth
4. human API key issuance on approval
5. one-time polling flow that returns the raw credential once and then consumes it
6. local installer script at `frontend/public/install.sh`
7. local config writing to `~/.teraslack/config.json`
8. local launcher generation for the stdio MCP
9. Codex stdio MCP registration
10. Claude stdio MCP registration
11. minimal Codex and Claude instruction injection

### Left To Do

1. decide whether the approval page should support workspace switching or explicit workspace selection instead of always using the current browser-session workspace
2. improve the approval page styling and copy
3. decide whether building the stdio binary from source is acceptable long term, or add prebuilt binary distribution
4. add better installer diagnostics for partial post-approval failures
5. add uninstall support
6. add revoke support for the installed API key
7. add rotation support for the installed API key
8. decide whether to keep approval at `POST /cli/install/{id}` or move it to `POST /cli/install/{id}/approve`
9. add stronger install-session test coverage for approval, consumption, and expiry behavior
10. ensure production serves `https://teraslack.ai/install.sh` and exposes the `/cli/install/...` endpoints on the intended host
11. expand user-facing docs around lifecycle operations such as uninstall and revoke

## Product Decision

The installed local credential should be:

1. A long-lived human-scoped Teraslack credential.
2. Bound to the approved workspace.
3. Stored locally on disk with restrictive permissions.

The installed local credential should not be:

1. A browser session cookie.
2. A temporary login nonce.
3. A browser-only credential that cannot be reused by the local MCP.

This keeps the architecture aligned with how Codex CLI and OpenCode appear to work in practice today:

1. browser login and approval
2. durable local credential written to disk
3. CLI or MCP reuses that credential directly

## Existing Primitives To Reuse

The current backend already has most of the needed building blocks:

1. Browser sign-in and session cookies via the existing OAuth login flow.
2. Session-authenticated access to the normal HTTP API.
3. API key validation on normal HTTP endpoints.
4. A local stdio MCP server that runs from `TERASLACK_BASE_URL` and `TERASLACK_API_KEY`.

The missing piece is a dedicated installer bootstrap flow that turns browser approval into a local long-lived credential and automatic MCP registration.

## Desired User Flow

1. User runs:

   ```sh
   curl https://teraslack.ai/install.sh | sh
   ```

2. The installer creates a backend install session.
3. The installer opens the browser to an approval URL.
4. If the user is not signed in, the browser goes through the existing Teraslack login flow.
5. After sign-in, the browser shows an install approval page.
6. The user chooses the target workspace if needed.
7. The user approves installation.
8. The backend mints a long-lived human credential for local CLI/MCP use.
9. The installer polls until approval completes.
10. The installer receives the credential once.
11. The installer writes local config and registers MCP with Codex and Claude.

## Security Model

Rules:

1. Only browser approval can mint the installed credential.
2. The shell installer never receives browser cookies.
3. The browser never renders the raw credential into the page.
4. The raw credential is returned only to the polling installer and only once.
5. Install sessions expire quickly if abandoned.
6. Local config files must be written with `0600` permissions.
7. The installed credential must be revocable independently from the browser session.

This is simpler than a device-token or refresh-token architecture, but that tradeoff is accepted.

## Backend Design

### New Install Session Resource

Add a small backend resource for installer bootstrap sessions.

Suggested fields:

1. `id`
2. `poll_token_hash`
3. `status`
   - `pending`
   - `approved`
   - `consumed`
   - `expired`
   - `cancelled`
4. `workspace_id`
5. `approved_by_user_id`
6. `credential_id`
7. `raw_credential_encrypted`
8. `device_name`
9. `client_kind`
10. `expires_at`
11. `approved_at`
12. `consumed_at`

### Credential Type

The simplest path is to mint a long-lived human API key.

Reasons:

1. The stdio MCP already expects `TERASLACK_API_KEY`.
2. The existing API already authenticates API keys.
3. This avoids inventing a new credential family just for install.

If a different credential type is introduced later, the installer protocol can stay mostly the same.

### New Endpoints

#### `POST /cli/install/sessions`

Purpose:

1. Create a new pending install session.
2. Return browser and polling bootstrap info.

Request:

```json
{
  "client_kind": "local_mcp",
  "device_name": "johns-macbook-pro"
}
```

Response:

```json
{
  "install_id": "...",
  "approval_url": "https://teraslack.ai/cli/install/...",
  "poll_token": "...",
  "expires_at": "..."
}
```

Notes:

1. This endpoint is unauthenticated.
2. `poll_token` must be random and high-entropy.
3. The response must not include a Teraslack credential.

#### `GET /cli/install/{install_id}`

Purpose:

1. Render the install approval page in the browser.
2. Reuse existing browser session auth.

Behavior:

1. If no session cookie is present, redirect into existing login flow.
2. After login, return to this page.
3. Show workspace and install details before approval.

#### `POST /cli/install/{install_id}`

Purpose:

1. Finalize approval from the browser.
2. Mint the long-lived human credential.

Behavior:

1. Require existing browser session auth.
2. Validate install session is still pending.
3. Resolve the selected workspace.
4. Mint a human-scoped API key for the signed-in user in that workspace.
5. Store the raw key server-side in encrypted form.
6. Mark the install session as approved.

Note:

1. The current implementation uses `POST /cli/install/{install_id}` rather than a separate `/approve` suffix.

#### `POST /cli/install/{install_id}/poll`

Purpose:

1. Let the shell installer wait for approval.
2. Deliver the raw local credential once.

Request:

```json
{
  "poll_token": "..."
}
```

Response while pending:

```json
{
  "status": "pending"
}
```

Response when approved:

```json
{
  "status": "approved",
  "base_url": "https://api.teraslack.ai",
  "workspace_id": "T_...",
  "user_id": "U_...",
  "api_key": "sk_live_..."
}
```

After successful delivery, mark the session as `consumed` so the raw key is not returned again.

### Optional Future Endpoints

Not required for first ship:

1. `DELETE /cli/install/{install_id}`
2. `POST /cli/install/{install_id}/revoke`
3. `POST /cli/install/{install_id}/rotate`
4. `GET /cli/installs`

## Provisioning Logic

Create a dedicated service for the installer flow rather than having the shell script orchestrate low-level `/api-keys` calls directly.

### Human Credential Provisioning

The approved install should mint a user-scoped API key for the signed-in human in the selected workspace.

Suggested defaults:

1. key name includes install purpose and device, e.g. `local-mcp johns-macbook-pro`
2. bounded expiration if desired, e.g. `90d`, or no expiration if product wants maximum convenience
3. permissions limited to what the local stdio MCP actually needs

This is intentionally simpler than introducing an owned agent or install-refresh-token design.

### Permissions Strategy

Start narrower than full API access unless there is a strong reason not to.

Current implementation:

1. uses the full current Teraslack stdio/MCP permission set
2. includes `api_keys.create`
3. prefers a key that works across the full current stdio surface, including `api_request`

Rationale:

1. the local stdio server exposes more than just basic messaging helpers
2. a narrower key would make parts of the installed MCP surface fail unexpectedly
3. this can still be revisited later if the stdio tool surface is narrowed or split

## Installer Responsibilities

The shell installer should stay operationally thin.

### Installer Steps

1. Determine OS and install directory.
2. Call `POST /cli/install/sessions`.
3. Open the browser using:
   - `open` on macOS
   - `xdg-open` on Linux
4. Poll `POST /cli/install/{install_id}/poll` until:
   - approved
   - expired
   - cancelled
5. Write local config.
6. Install a local launcher for the stdio MCP.
7. Register the MCP server with Codex if installed.
8. Register the MCP server with Claude if installed.
9. Add minimal local instructions for Codex and Claude.

### Local Config

Suggested path:

1. `~/.teraslack/config.json`

Suggested contents:

```json
{
  "base_url": "https://api.teraslack.ai",
  "api_key": "sk_live_...",
  "workspace_id": "T_...",
  "user_id": "U_...",
  "installed_at": "..."
}
```

Requirements:

1. create parent directory if needed
2. chmod file to `0600`
3. never print raw key after setup succeeds

### Local Launcher

Install a tiny wrapper, for example:

1. `~/.teraslack/bin/teraslack-stdio-mcp`

Responsibilities:

1. read config
2. export `TERASLACK_BASE_URL`
3. export `TERASLACK_API_KEY`
4. exec the Teraslack stdio MCP binary

This keeps Codex and Claude registration stable even if the config location changes later.

## Codex Registration

Register a local stdio MCP server, similar in spirit to supermanager's installer.

Suggested behavior:

1. detect whether `codex` exists
2. run a `codex mcp add ...` command pointing to the local launcher
3. optionally update `~/.codex/AGENTS.md` with a bounded Teraslack block

Suggested Codex instructions:

1. call `whoami` first when using Teraslack
2. establish a DM or default conversation before sending messages
3. prefer dedicated MCP tools over raw `api_request`

The instructions should be concise and idempotently managed using marker comments.

## Claude Registration

Register the same local launcher as a stdio MCP server for Claude.

Suggested behavior:

1. detect whether `claude` exists
2. run a `claude mcp add --transport stdio ...` command pointing to the local launcher
3. only introduce a Claude plugin if instruction packaging becomes necessary

For this plan, direct MCP registration is preferable to a custom plugin unless Claude needs extra install-time behavior.

## Reinstall, Idempotency, And Revocation

### Reinstall

Re-running the installer should:

1. overwrite local config safely
2. refresh client MCP registration if needed
3. create a fresh local credential if needed

### Uninstall

Should exist soon after initial ship:

1. delete local config
2. remove local launcher
3. remove Codex MCP registration
4. remove Claude MCP registration
5. revoke the underlying human API key if local metadata is still present

### Rotation

Later improvement:

1. add a `teraslack auth rotate` command
2. use existing API key rotation primitives

## Documentation Deliverables

Add docs for:

1. install flow
2. security model
3. local file locations
4. Codex registration behavior
5. Claude registration behavior
6. reinstall and uninstall behavior

## Recommended Implementation Order

1. Add install session persistence model.
2. Add `POST /cli/install/sessions`.
3. Add browser approval page and approval submit endpoint.
4. Add provisioning service for human credential issuance.
5. Add polling endpoint with one-time raw credential delivery.
6. Build the installer script for `https://teraslack.ai/install.sh`.
7. Add the local launcher and config writing.
8. Register stdio MCP with Codex and Claude.
9. Add minimal instructions injection for Codex and Claude.
10. Write initial docs.

## Non-Goals

These are intentionally out of scope for this plan:

1. Defaulting users to hosted remote OAuth MCP.
2. Device-grant or refresh-token broker architecture.
3. Owned-agent provisioning during install.
4. Cross-device credential sync.
5. Dynamic client registration for remote MCP OAuth.

## Summary

The intended architecture is:

1. `curl https://teraslack.ai/install.sh | sh`
2. browser login and approval
3. backend issues a long-lived local human credential
4. installer writes local config
5. installer registers a local stdio MCP for Codex and Claude

This is the simplest path that matches the current Teraslack backend, the existing stdio MCP shape, and the way Codex CLI and OpenCode appear to bootstrap local credentials today.
