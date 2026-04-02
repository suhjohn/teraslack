# API Contract

## Principles

- Canonical API paths are unversioned.
- The API is resource-oriented and avoids Slack-style RPC routes.
- Success responses use standard HTTP status codes and do not include an `ok` flag.
- Error responses are JSON objects with stable machine-readable `code` values.
- Collection endpoints use cursor pagination with a shared response envelope.
- The OpenAPI source of truth lives at `server/api/openapi.yaml`.
- The server publishes the generated contract at `GET /openapi.json` and `GET /openapi.yaml`.

## Success Responses

Single-resource responses return the resource directly:

```json
{
  "id": "U123",
  "name": "owner"
}
```

Collection responses return:

```json
{
  "items": [],
  "next_cursor": "cursor_123"
}
```

`next_cursor` is omitted when there is no next page.

## Error Responses

All error responses use this shape:

```json
{
  "code": "invalid_request",
  "message": "The request is invalid.",
  "request_id": "req_123"
}
```

Validation failures may include field-level details:

```json
{
  "code": "validation_failed",
  "message": "The request body is invalid.",
  "request_id": "req_123",
  "errors": [
    {
      "field": "email",
      "code": "invalid_format",
      "message": "Must be a valid email."
    }
  ]
}
```

## Status Codes

- `200` successful read or update with a response body
- `201` successful create
- `204` successful delete or bodyless mutation
- `400` malformed JSON or invalid request syntax
- `401` missing or invalid authentication
- `403` authenticated but not authorized
- `404` resource not found
- `409` state conflict
- `422` semantically invalid request when needed
- `429` rate limited
- `500` unexpected internal error

## Authentication

- The API uses `Authorization: Bearer <token>`.
- Missing or invalid credentials return `401`.
- Permission failures return `403`.
- Request ids are returned in the `X-Request-Id` response header and echoed in error bodies.

## Identity Model

- `accounts` are the canonical cross-workspace identities.
- `workspace_memberships` are the canonical normal-membership records for a workspace.
- `/users` is a compatibility view materialized from `accounts + workspace_memberships`.
- workspace-scoped role and admin rank come from `workspace_memberships`, not from legacy `users` rows.
- `workspace_external_workspaces` are org-to-org connection records only. They do not grant resource access by themselves.
- `external_members` are the canonical conversation-scoped cross-workspace grants.
- Cross-workspace reads and writes must be derived from `external_members`, not from normal workspace membership.

## Patch Semantics

- `PATCH` means partial update.
- Omitted fields remain unchanged.
- Nullable fields may be cleared with `null` when supported by the handler.

## Canonical Routes

### Workspaces

- `GET /workspaces`
- `GET /workspaces` is session-scoped and returns only the authenticated workspace for human sessions.
- `POST /workspaces`
- `GET /workspaces/{id}`
- `PATCH /workspaces/{id}`
- `GET /workspaces/{id}/admins`
- `GET /workspaces/{id}/owners`
- `GET /workspaces/{id}/access-logs`
- `GET /workspaces/{id}/billable-info`
- `GET /workspaces/{id}/billing`
- `GET /workspaces/{id}/external-workspaces`
- `DELETE /workspaces/{id}/external-workspaces/{external_workspace_id}`
- `GET /workspaces/{id}/integration-logs`
- `GET /workspaces/{id}/preferences`

### Users

- `GET /users`
- `POST /users`
- `GET /users/{id}`
- `PATCH /users/{id}`
- `/users` remains a compatibility surface keyed by materialized workspace-local `user_id`.
- Reads and authorization resolve through membership/account identity first, then materialize a compatibility `user_id` only when needed.

### Conversations

- `GET /conversations`
- `POST /conversations`
- `GET /conversations/{id}`
- `PATCH /conversations/{id}`
- `GET /conversations/{id}/members`
- `POST /conversations/{id}/members`
- `DELETE /conversations/{id}/members/{user_id}`
- `GET /conversations/{id}/external-members`
- `POST /conversations/{id}/external-members`
- `PATCH /conversations/{id}/external-members/{external_member_id}`
- `DELETE /conversations/{id}/external-members/{external_member_id}`
- `PUT /conversations/{id}/read-state`

### Messages

- `GET /messages`
- `POST /messages`
- `PATCH /messages/{conversation_id}/{message_ts}`
- `DELETE /messages/{conversation_id}/{message_ts}`
- `GET /messages/{conversation_id}/{message_ts}/reactions`
- `POST /messages/{conversation_id}/{message_ts}/reactions`
- `DELETE /messages/{conversation_id}/{message_ts}/reactions/{reaction_name}`

### Files

- `POST /file-uploads`
- `POST /file-uploads` accepts optional `channel_id`. External shared writes must provide a shared conversation here.
- `POST /file-uploads/{id}/complete`
- `GET /files`
- `POST /files`
- `POST /files` accepts optional `channel_id`. External shared writes must provide a shared conversation here.
- `GET /files/{id}`
- `DELETE /files/{id}`
- `POST /files/{id}/shares`

### Event Subscriptions

- `GET /event-subscriptions`
- `POST /event-subscriptions`
- `GET /event-subscriptions/{id}`
- `PATCH /event-subscriptions/{id}`
- `DELETE /event-subscriptions/{id}`

### Auth and API Keys

- `GET /auth/oauth/{provider}/start`
- `GET /auth/oauth/{provider}/callback`
- `DELETE /auth/sessions/current`
- `GET /auth/me`
- `GET /api-keys`
- `POST /api-keys`
- `GET /api-keys/{id}`
- `PATCH /api-keys/{id}`
- `DELETE /api-keys/{id}`
- `POST /api-keys/{id}/rotations`

### Events and Search

- `GET /events`
- `GET /events` accepts optional `workspace_id`. External actors may target a shared host workspace they can access through `external_members`.
- `POST /search`
- `POST /search` is workspace-scoped and external actors only receive results from explicitly shared conversations/files.
