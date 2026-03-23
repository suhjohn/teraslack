# API Contract

## Principles

- Canonical API paths are unversioned.
- The API is resource-oriented and avoids Slack-style RPC routes.
- Success responses use standard HTTP status codes and do not include an `ok` flag.
- Error responses are JSON objects with stable machine-readable `code` values.
- Collection endpoints use cursor pagination with a shared response envelope.
- The OpenAPI source of truth lives at `api/openapi.yaml`.
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

## Patch Semantics

- `PATCH` means partial update.
- Omitted fields remain unchanged.
- Nullable fields may be cleared with `null` when supported by the handler.

## Canonical Routes

### Teams

- `GET /teams`
- `POST /teams`
- `GET /teams/{id}`
- `PATCH /teams/{id}`
- `GET /teams/{id}/admins`
- `GET /teams/{id}/owners`
- `GET /teams/{id}/access-logs`
- `GET /teams/{id}/billable-info`
- `GET /teams/{id}/billing`
- `GET /teams/{id}/external-teams`
- `DELETE /teams/{id}/external-teams/{external_team_id}`
- `GET /teams/{id}/integration-logs`
- `GET /teams/{id}/preferences`
- `GET /teams/{id}/profile-fields`

### Users

- `GET /users`
- `POST /users`
- `GET /users/{id}`
- `PATCH /users/{id}`

### Conversations

- `GET /conversations`
- `POST /conversations`
- `GET /conversations/{id}`
- `PATCH /conversations/{id}`
- `GET /conversations/{id}/members`
- `POST /conversations/{id}/members`
- `DELETE /conversations/{id}/members/{user_id}`
- `PUT /conversations/{id}/read-state`
- `GET /conversations/{id}/pins`
- `POST /conversations/{id}/pins`
- `DELETE /conversations/{id}/pins/{message_ts}`
- `GET /conversations/{id}/bookmarks`
- `POST /conversations/{id}/bookmarks`
- `PATCH /conversations/{id}/bookmarks/{bookmark_id}`
- `DELETE /conversations/{id}/bookmarks/{bookmark_id}`

### Messages

- `GET /messages`
- `POST /messages`
- `PATCH /messages/{conversation_id}/{message_ts}`
- `DELETE /messages/{conversation_id}/{message_ts}`
- `GET /messages/{conversation_id}/{message_ts}/reactions`
- `POST /messages/{conversation_id}/{message_ts}/reactions`
- `DELETE /messages/{conversation_id}/{message_ts}/reactions/{reaction_name}`

### Usergroups

- `GET /usergroups`
- `POST /usergroups`
- `GET /usergroups/{id}`
- `PATCH /usergroups/{id}`
- `GET /usergroups/{id}/members`
- `PUT /usergroups/{id}/members`

### Files

- `POST /file-uploads`
- `POST /file-uploads/{id}/complete`
- `GET /files`
- `POST /files`
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
- `POST /search`
