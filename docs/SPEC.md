# Teraslack Core Messaging Spec

## Status

- Proposed greenfield specification
- Scope: identity model, database schema, HTTP API, authorization rules, and core user flows
- This document describes a clean rewrite of Teraslack's messaging model from scratch

## Product Thesis

Teraslack has two communication surfaces:

- direct conversations between users
- channels inside workspaces

The system should feel like this:

- a user is the real identity
- a workspace is a collaboration container
- direct conversations happen between users directly
- channels belong to one workspace
- selecting a workspace is client UI state, not an authentication prerequisite for direct messaging

This removes the ambiguity that happens when a product has both a global user and a second workspace-local user identity.

## Design Goals

- Use one canonical global identity model.
- Make direct conversations user-native and workspace-independent.
- Make channels explicitly workspace-scoped.
- Preserve an immutable internal event log for replay, audit, and asynchronous integrations.
- Keep authorization simple and explainable.
- Avoid duplicate identity tables that model the same person twice.
- Make the API resource-oriented and unsurprising.
- Let human users and agents share the same core identity and conversation model.

## Non-Goals

- Voice or video
- Enterprise org hierarchies
- Friends / follower graph
- File storage
- Threads
- Reactions
- Per-channel custom ACL languages
- Full Discord-style permission bitsets

## Core Concepts

### User

A `User` is the canonical global identity.

Users can be:

- human
- agent

System processes are not modeled as normal messaging participants.

### Workspace

A `Workspace` is a named collaboration container.

Workspaces contain:

- members
- channels
- settings

Workspaces do not own direct conversations.

### Workspace Member

A `WorkspaceMember` is not a separate identity. It is a workspace-scoped projection of a user.

It combines:

- the underlying user
- the user's membership in one workspace
- optional workspace-local profile overrides

Important rule:

- there is no canonical persisted `WorkspaceUser` identity separate from `User`

If the product needs a workspace-facing member shape, it is an API projection, not a second identity model.

### Conversation

A `Conversation` is a message container.

Conversation kinds:

- `direct_conversation`
- `channel`

Rules:

- a `direct_conversation` is global and never belongs to a workspace
- a `channel` always belongs to exactly one workspace

### Direct Message Forms

The product exposes two direct-message forms, but they are derived, not separately typed.

Derived forms:

- one-to-one direct message: a `direct_conversation` with exactly 2 participants
- group direct message: a `direct_conversation` with 3 or more participants

Rules:

- one-to-one direct messages are canonical by unordered user pair
- group direct messages are not canonical by participant set

### Channel Visibility

A `channel` has one visibility mode:

- `public`
- `private`

Rules:

- public channels are visible to all active workspace members
- private channels are visible only to explicit channel participants

### Participant

A `Participant` is a user that is explicitly a member of a conversation.

Rules:

- all direct conversations use explicit participants
- private channels use explicit participants
- public channels do not require explicit participant rows for visibility

### Message

A `Message` is authored by a user and belongs to one conversation.

Messages do not need a workspace-local author id.

If the client wants workspace-local rendering data, that is derived from the user plus workspace membership when the conversation is a channel.

## Canonical Product Rules

1. `User` is the only canonical messaging identity.
2. Workspaces grant access to channels, not to direct conversations.
3. Direct conversations are addressed by user membership only.
4. Public channels are visible to all active workspace members.
5. Private channels are visible only to explicit channel participants.
6. A workspace member is a user in a workspace, not a new identity.
7. The server never requires a workspace-selected session to access a direct conversation.
8. The server never creates workspace membership as a side effect of direct-conversation creation.
9. One-to-one direct messages and group direct messages are derived views over one persisted `direct_conversation` kind.
10. Every successful state mutation appends an immutable internal event in the same transaction.
11. Public event delivery happens from projected external events, not directly from the internal log.

## Event-Sourcing Model

Teraslack uses a hybrid event-sourced architecture:

- normalized relational tables are the primary online state model
- every successful mutation appends an immutable internal event
- asynchronous projectors derive consumer-facing event streams and rebuildable read-side artifacts from the internal log

This is not a pure "events only" system. It is a transactional state model with a canonical immutable event log attached to every write.

### Event Layers

There are three event layers:

1. domain mutation
2. internal event log
3. external event stream

#### Domain Mutation

Command handlers update primary tables such as:

- `users`
- `workspace_memberships`
- `conversations`
- `conversation_participants`
- `messages`

These writes happen in a database transaction.

#### Internal Event Log

In the same transaction, the service appends one or more immutable `internal_events` rows.

Internal events are used for:

- auditability
- replay
- rebuilding projections
- asynchronous integrations
- downstream event delivery

#### External Event Stream

A separate projector reads `internal_events` and emits consumer-facing `external_events`.

External events are used for:

- `GET /events`
- webhooks
- future streaming transports

The external stream is intentionally decoupled from the internal event log so the public event contract can stay stable and filtered even if internal event shapes evolve.

### Internal And External Event Naming

Internal event names express domain facts.

Examples:

- `user.created`
- `user.updated`
- `workspace.created`
- `workspace.updated`
- `workspace.membership.added`
- `workspace.membership.updated`
- `conversation.created`
- `conversation.updated`
- `conversation.archived`
- `conversation.participant.added`
- `conversation.participant.removed`
- `message.posted`
- `message.updated`
- `message.deleted`

External event names express the public integration contract.

Examples:

- `user.created`
- `user.updated`
- `workspace.created`
- `workspace.updated`
- `conversation.created`
- `conversation.updated`
- `conversation.archived`
- `conversation.participant.added`
- `conversation.participant.removed`
- `conversation.message.created`
- `conversation.message.updated`
- `conversation.message.deleted`

The projector is responsible for mapping internal event names and payloads into the public external event envelope.

### Write-Path Rule

Every successful command follows this rule:

1. begin transaction
2. apply primary state mutation
3. append internal event rows
4. commit transaction

If the transaction rolls back, no internal event is recorded.

### Projection Rule

Projectors are asynchronous and replayable.

Rules:

- projectors read internal events in id order within each shard
- projectors maintain durable checkpoints
- projectors may be restarted and replayed safely
- projection output must be idempotent
- projector failure must not roll back the original write transaction

### Visibility Rule

External events are never broadcast by per-user write-time fanout.

Instead:

- each external event is written once
- feed tables index the event against relevant resources
- `/events` applies visibility filters at read time

This keeps event writes `O(1)` with respect to workspace size and conversation membership size.

## Authorization Model

### User Authentication

Every authenticated request resolves to exactly one `user_id`.

Authentication may happen through:

- session token
- API key

### Workspace Access

Workspace access is determined by `workspace_memberships`.

A caller can access workspace resources only if their membership in that workspace is active.

### Conversation Access Matrix

#### `direct_conversation`

Access rule:

- caller must be a participant user

#### `channel` with `visibility = public`

Access rule:

- caller must have active membership in the workspace

#### `channel` with `visibility = private`

Access rule:

- caller must have active membership in the workspace
- caller must also be an explicit participant of the channel

### Workspace Roles

Workspace roles are intentionally simple:

- `owner`
- `admin`
- `member`

Rules:

- `owner` can do all workspace operations
- `admin` can manage members and channels but cannot remove the last owner
- `member` can read and post according to channel visibility

No workspace role ever grants access to a direct conversation the caller is not part of.

## Database Schema

### ID Format

All ids are opaque string ids. A ULID with a short type prefix is recommended.

Examples:

- `user_...`
- `ws_...`
- `conv_...`
- `msg_...`
- `sess_...`
- `key_...`

### Tables

#### `users`

Canonical global identity.

Fields:

- `id` PK
- `principal_type` enum: `human | agent`
- `email` nullable unique
- `status` enum: `active | suspended | deleted`
- `created_at`
- `updated_at`

Constraints:

- only `active` users may authenticate
- `email` is unique when present

#### `user_profiles`

Global display profile for a user.

Fields:

- `user_id` PK, FK `users.id`
- `handle` unique
- `display_name`
- `avatar_url` nullable
- `bio` nullable
- `created_at`
- `updated_at`

Constraints:

- exactly one profile row per user
- `handle` is globally unique

#### `auth_sessions`

Browser or app sessions.

Fields:

- `id` PK
- `user_id` FK `users.id`
- `token_hash` unique
- `expires_at`
- `last_seen_at`
- `revoked_at` nullable
- `created_at`

Important rule:

- sessions do not store a selected workspace

Selected workspace is client state.

#### `api_keys`

Programmatic credentials.

Fields:

- `id` PK
- `user_id` FK `users.id`
- `label`
- `secret_hash`
- `scope_type` enum: `user | workspace`
- `scope_workspace_id` nullable FK `workspaces.id`
- `expires_at` nullable
- `last_used_at` nullable
- `revoked_at` nullable
- `created_at`

Rules:

- `scope_type = user` means the key can act as the user across direct-conversation and workspace surfaces the user may access
- `scope_type = workspace` means the key can act only inside one workspace

#### `workspaces`

Workspace container.

Fields:

- `id` PK
- `slug` unique
- `name`
- `created_by_user_id` FK `users.id`
- `created_at`
- `updated_at`

#### `workspace_memberships`

Canonical workspace access record.

Fields:

- `id` PK
- `workspace_id` FK `workspaces.id`
- `user_id` FK `users.id`
- `role` enum: `owner | admin | member`
- `status` enum: `invited | active | suspended | removed`
- `invited_by_user_id` nullable FK `users.id`
- `joined_at` nullable
- `created_at`
- `updated_at`

Constraints:

- unique `(workspace_id, user_id)`
- at least one active `owner` must exist for every workspace

#### `workspace_profiles`

Workspace-local display overrides.

Fields:

- `workspace_id` FK `workspaces.id`
- `user_id` FK `users.id`
- `display_name` nullable
- `avatar_url` nullable
- `title` nullable
- `status_text` nullable
- `status_emoji` nullable
- `created_at`
- `updated_at`

Primary key:

- `(workspace_id, user_id)`

Purpose:

- render a user differently inside one workspace without changing that user's global direct-message identity

#### `workspace_invites`

Workspace onboarding tokens.

Fields:

- `id` PK
- `workspace_id` FK `workspaces.id`
- `email` nullable
- `invited_by_user_id` FK `users.id`
- `token_hash` unique
- `expires_at`
- `accepted_at` nullable
- `accepted_by_user_id` nullable FK `users.id`
- `created_at`

#### `internal_events`

Immutable internal event log.

Fields:

- `id` PK bigint sequence
- `event_type`
- `aggregate_type`
- `aggregate_id`
- `workspace_id` nullable FK `workspaces.id`
- `actor_user_id` nullable FK `users.id`
- `shard_key`
- `shard_id`
- `payload` JSONB
- `metadata` nullable JSONB
- `created_at`

Rules:

- append-only
- never updated in place
- `payload` contains the event snapshot needed for replay or projection
- `metadata` may contain actor and request context

Recommended aggregate types:

- `user`
- `workspace`
- `workspace_membership`
- `conversation`
- `message`

#### `projector_checkpoints`

Durable progress for background projectors.

Fields:

- `name` PK
- `last_event_id`
- `updated_at`

Rules:

- one checkpoint per projector stream or per projector shard
- checkpoints are advanced only after projection side effects commit

#### `conversations`

Canonical message container.

Fields:

- `id` PK
- `kind` enum: `direct_conversation | channel`
- `workspace_id` nullable FK `workspaces.id`
- `channel_visibility` nullable enum: `public | private`
- `title` nullable
- `description` nullable
- `created_by_user_id` FK `users.id`
- `archived_at` nullable
- `last_message_at` nullable
- `created_at`
- `updated_at`

Constraints:

- `workspace_id IS NULL` for `kind = direct_conversation`
- `workspace_id IS NOT NULL` for `kind = channel`
- `channel_visibility IS NULL` for `kind = direct_conversation`
- `channel_visibility IN ('public', 'private')` for `kind = channel`
- `title IS NOT NULL` for `kind = channel`

Rules:

- one-to-one direct-message titles are derived from the other participant
- group direct-message titles may be derived from participants or set explicitly
- channel titles are stored directly
- `archived_at` is only meaningful for channels

#### `direct_conversation_pairs`

Canonical lookup table for one-to-one direct messages.

Fields:

- `conversation_id` PK, FK `conversations.id`
- `user_low_id` FK `users.id`
- `user_high_id` FK `users.id`

Constraints:

- unique `(user_low_id, user_high_id)`
- `user_low_id <> user_high_id`

Purpose:

- exactly one one-to-one direct message exists for one unordered pair of users

#### `conversation_participants`

Explicit membership table.

Fields:

- `conversation_id` FK `conversations.id`
- `user_id` FK `users.id`
- `added_by_user_id` nullable FK `users.id`
- `joined_at`

Primary key:

- `(conversation_id, user_id)`

Rules:

- required for all `direct_conversation`
- required for all `channel` rows with `channel_visibility = private`
- optional and normally unused for public channels

#### `conversation_reads`

Per-user read cursor.

Fields:

- `conversation_id` FK `conversations.id`
- `user_id` FK `users.id`
- `last_read_message_id` nullable FK `messages.id`
- `last_read_at` nullable
- `updated_at`

Primary key:

- `(conversation_id, user_id)`

#### `messages`

Canonical message rows.

Fields:

- `id` PK
- `conversation_id` FK `conversations.id`
- `author_user_id` FK `users.id`
- `body_text`
- `body_rich` nullable JSONB
- `metadata` nullable JSONB
- `edited_at` nullable
- `deleted_at` nullable
- `created_at`

Rules:

- `author_user_id` is always required
- `body_text` may be empty only if `body_rich` is present

#### `external_events`

Canonical public event envelope used by `/events` and webhook delivery.

Fields:

- `id` PK bigint sequence
- `workspace_id` nullable FK `workspaces.id`
- `type`
- `resource_type`
- `resource_id`
- `occurred_at`
- `payload` JSONB
- `source_internal_event_id` nullable FK `internal_events.id`
- `source_internal_event_ids` JSONB
- `dedupe_key`
- `created_at`

Rules:

- `workspace_id` is nullable for global direct-conversation events
- `resource_type` is a public-facing type, not necessarily the internal aggregate type
- `dedupe_key` makes projector writes idempotent

Recommended resource types:

- `user`
- `workspace`
- `conversation`
- `message`

#### `workspace_event_feed`

Resource feed index for workspace-scoped events.

Fields:

- `feed_id` PK bigint sequence
- `workspace_id` FK `workspaces.id`
- `external_event_id` FK `external_events.id`
- `created_at`

Unique key:

- `(workspace_id, external_event_id)`

#### `conversation_event_feed`

Resource feed index for conversation-scoped events.

Fields:

- `feed_id` PK bigint sequence
- `conversation_id` FK `conversations.id`
- `external_event_id` FK `external_events.id`
- `created_at`

Unique key:

- `(conversation_id, external_event_id)`

#### `user_event_feed`

Resource feed index for user-scoped events.

Fields:

- `feed_id` PK bigint sequence
- `user_id` FK `users.id`
- `external_event_id` FK `external_events.id`
- `created_at`

Unique key:

- `(user_id, external_event_id)`

#### `event_subscriptions`

Webhook subscriptions for external events.

Fields:

- `id` PK
- `owner_user_id` FK `users.id`
- `workspace_id` nullable FK `workspaces.id`
- `url`
- `enabled`
- `encrypted_secret`
- `event_type` nullable
- `resource_type` nullable
- `resource_id` nullable
- `created_at`
- `updated_at`

Rules:

- subscriptions are evaluated against the caller's visibility
- `workspace_id` narrows events to one workspace when present
- `resource_id` requires `resource_type`

#### `external_event_projection_failures`

Operational failure log for projector errors.

Fields:

- `id` PK bigint sequence
- `internal_event_id` FK `internal_events.id`
- `error`
- `created_at`

## Database Invariants

1. Every `direct_conversation` has at least two distinct participant users.
2. A `direct_conversation` with exactly two participants is a one-to-one direct message.
3. A `direct_conversation` with three or more participants is a group direct message.
4. Every row in `direct_conversation_pairs` references a `direct_conversation` with exactly two participants.
5. A `channel` with `channel_visibility = public` has no participant requirement for visibility.
6. A `channel` with `channel_visibility = private` participant must also be an active workspace member of the same workspace.
7. A `direct_conversation` never has a `workspace_id`.
8. A `channel` always has a `workspace_id`.
9. Deleting or leaving a workspace does not delete a user's direct conversations.
10. `internal_events` is append-only.
11. `external_events` is idempotent on projection scope plus `dedupe_key`.
12. Every event-feed row points to exactly one `external_events` row.
13. Projector checkpoints advance monotonically.

## Recommended Indexes

- `users(email)`
- `user_profiles(handle)`
- `workspace_memberships(workspace_id, user_id)`
- `workspace_memberships(user_id, status)`
- `conversations(workspace_id, kind, updated_at DESC)`
- `conversations(kind, updated_at DESC)`
- `conversation_participants(user_id, conversation_id)`
- `conversation_reads(user_id, conversation_id)`
- `messages(conversation_id, created_at DESC)`
- `direct_conversation_pairs(user_low_id, user_high_id)`
- `internal_events(id)`
- `internal_events(aggregate_type, aggregate_id, id)`
- `internal_events(shard_id, id)`
- `external_events(id)`
- `external_events(workspace_id, dedupe_key)`
- `workspace_event_feed(workspace_id, external_event_id)`
- `conversation_event_feed(conversation_id, external_event_id)`
- `user_event_feed(user_id, external_event_id)`
- `event_subscriptions(owner_user_id)`
- `event_subscriptions(workspace_id)`

## API Conventions

- Bearer auth: `Authorization: Bearer <token>`
- JSON request and response bodies
- Unversioned resource-oriented paths
- Cursor pagination for collection routes
- Stable error shape

### Error Shape

```json
{
  "code": "forbidden",
  "message": "You do not have access to this resource.",
  "request_id": "req_123"
}
```

### Collection Shape

```json
{
  "items": [],
  "next_cursor": "cursor_123"
}
```

## Resource Shapes

### User

```json
{
  "id": "user_01",
  "principal_type": "human",
  "status": "active",
  "profile": {
    "handle": "jane",
    "display_name": "Jane",
    "avatar_url": "https://..."
  }
}
```

### Workspace Member

```json
{
  "workspace_id": "ws_01",
  "user_id": "user_01",
  "role": "member",
  "status": "active",
  "user": {
    "id": "user_01",
    "principal_type": "human",
    "profile": {
      "handle": "jane",
      "display_name": "Jane"
    }
  },
  "workspace_profile": {
    "display_name": "Jane S.",
    "title": "Engineering"
  }
}
```

### Conversation

```json
{
  "id": "conv_01",
  "kind": "direct_conversation",
  "workspace_id": null,
  "channel_visibility": null,
  "direct_message_kind": "one_to_one",
  "participant_count": 2,
  "title": null,
  "description": null,
  "created_by_user_id": "user_01",
  "last_message_at": "2026-04-04T20:00:00Z",
  "created_at": "2026-04-01T10:00:00Z",
  "updated_at": "2026-04-04T20:00:00Z"
}
```

Rules:

- `direct_message_kind` is a derived response field for direct conversations only
- allowed values are `one_to_one` and `group`

### Message

```json
{
  "id": "msg_01",
  "conversation_id": "conv_01",
  "author_user_id": "user_01",
  "body_text": "hello",
  "created_at": "2026-04-04T20:00:00Z"
}
```

## HTTP API

### Auth

#### `POST /auth/sessions`

Create a session.

Request:

```json
{
  "provider": "google",
  "oauth_code": "..."
}
```

Response:

```json
{
  "session": {
    "token": "secret",
    "expires_at": "2026-04-05T20:00:00Z"
  },
  "user": {
    "id": "user_01",
    "principal_type": "human"
  }
}
```

#### `DELETE /auth/sessions/current`

Revoke the current session.

#### `GET /me`

Return the authenticated bootstrap surface.

Response:

```json
{
  "user": {
    "id": "user_01",
    "principal_type": "human",
    "status": "active",
    "profile": {
      "handle": "jane",
      "display_name": "Jane"
    }
  },
  "workspaces": [
    {
      "workspace_id": "ws_01",
      "role": "owner",
      "status": "active",
      "name": "Acme"
    }
  ]
}
```

### Users

#### `GET /users/me`

Return the authenticated user and global profile.

#### `PATCH /users/me/profile`

Update the global user profile used for direct conversations.

Request:

```json
{
  "display_name": "Jane",
  "avatar_url": "https://..."
}
```

### Search

#### `POST /search`

Unified search across all caller-visible entities.

Request:

```json
{
  "query": "jane",
  "entity_types": ["user", "workspace", "channel", "direct_conversation"],
  "workspace_id": "ws_01",
  "limit": 20,
  "cursor": null
}
```

Rules:

- `entity_types` is optional; when omitted, the server searches all searchable entity types
- `workspace_id` is optional and narrows workspace-scoped results when present
- all results are filtered by caller visibility rules
- the endpoint is the only search surface in the API

Searchable entity types in v1:

- `user`
- `workspace`
- `channel`
- `direct_conversation`

Result shape:

```json
{
  "items": [
    {
      "entity_type": "user",
      "id": "user_01",
      "title": "Jane",
      "subtitle": "@jane",
      "workspace_id": null
    }
  ],
  "next_cursor": "cursor_123"
}
```

Visibility rules:

- `user` results include only users that share at least one workspace with the caller
- `workspace` results include only workspaces the caller belongs to
- `channel` results include only channels visible to the caller
- `direct_conversation` results include only direct conversations the caller participates in

This endpoint supports direct-message creation, workspace navigation, and future in-product search without adding resource-specific search routes.

### Workspaces

#### `GET /workspaces`

List workspaces the user belongs to.

#### `POST /workspaces`

Create a workspace.

Request:

```json
{
  "name": "Acme",
  "slug": "acme"
}
```

Behavior:

- creator becomes `owner`
- the system creates a default `general` public channel

#### `GET /workspaces/{workspace_id}`

Return workspace metadata.

#### `PATCH /workspaces/{workspace_id}`

Update workspace metadata.

Owner/admin only.

### Workspace Members

#### `GET /workspaces/{workspace_id}/members`

List members in a workspace.

Response items are `WorkspaceMember` projections.

#### `POST /workspaces/{workspace_id}/invites`

Create a workspace invite.

Request:

```json
{
  "email": "person@example.com"
}
```

#### `POST /workspace-invites/{token}/accept`

Accept a workspace invite as the authenticated user.

#### `PATCH /workspaces/{workspace_id}/members/{user_id}`

Update member role or state.

Request:

```json
{
  "role": "admin",
  "status": "active"
}
```

Owner/admin only.

### Direct Conversations

#### `GET /direct-conversations`

List direct conversations for the authenticated user.

Rules:

- returns only `kind = direct_conversation`
- each item includes derived `direct_message_kind`
- ordered by most recent message activity

#### `POST /direct-conversations`

Create or fetch a direct conversation.

Request:

```json
{
  "participant_user_ids": ["user_target"],
  "title": null
}
```

Behavior:

- authenticated user is implicitly included
- the final distinct participant set must contain at least two users
- if the final participant set contains exactly two users, the server returns the canonical one-to-one direct message for that pair
- if the final participant set contains three or more users, the server creates a new group direct message

#### `GET /direct-conversations/{conversation_id}`

Return a direct conversation if the caller is a participant.

The response includes:

- `direct_message_kind = one_to_one` when participant count is 2
- `direct_message_kind = group` when participant count is 3 or more

#### `GET /direct-conversations/{conversation_id}/participants`

List direct-conversation participants.

#### `POST /direct-conversations/{conversation_id}/participants`

Add participants to a group direct message.

Request:

```json
{
  "user_ids": ["user_02", "user_03"]
}
```

Rules:

- valid only when the direct conversation already has 3 or more participants
- invalid for one-to-one direct messages
- all added users must be visible to the caller through unified search policy

#### `DELETE /direct-conversations/{conversation_id}/participants/{user_id}`

Remove a participant from a group direct message.

Rules:

- valid only when the direct conversation has 3 or more participants
- invalid for one-to-one direct messages
- invalid if removal would leave fewer than 3 participants

### Channels

#### `GET /workspaces/{workspace_id}/channels`

List channels visible to the authenticated user.

Visibility rules:

- all public channels in the workspace
- private channels where the caller is a participant

#### `POST /workspaces/{workspace_id}/channels`

Create a channel.

Request:

```json
{
  "visibility": "private",
  "title": "ops",
  "description": "Operations"
}
```

Rules:

- allowed visibilities are `public` and `private`
- creator must have permission to create channels
- creator is automatically added as a participant for `private` channels

#### `GET /channels/{channel_id}`

Return channel metadata if visible.

#### `PATCH /channels/{channel_id}`

Update channel metadata.

Request:

```json
{
  "title": "ops-platform",
  "description": "Ops and platform coordination",
  "archived": false
}
```

Owner/admin or channel manager policy can be added later. In v1, workspace owner/admin may manage channels.

#### `GET /channels/{channel_id}/participants`

List participants in a private channel.

Rules:

- for public channels this route may return an empty set or `400`; participant rows are not canonical for public visibility

#### `POST /channels/{channel_id}/participants`

Add participants to a private channel.

Request:

```json
{
  "user_ids": ["user_02", "user_03"]
}
```

Rules:

- valid only for private channels
- each added user must already be an active workspace member

#### `DELETE /channels/{channel_id}/participants/{user_id}`

Remove a participant from a private channel.

Rules:

- valid only for private channels
- channel cannot become empty

### Messages

#### `GET /conversations/{conversation_id}/messages`

List messages in a conversation.

Query params:

- `cursor`
- `limit`
- `before_message_id`

#### `POST /conversations/{conversation_id}/messages`

Post a message.

Request:

```json
{
  "body_text": "hello",
  "body_rich": null,
  "metadata": {
    "source": "web"
  }
}
```

Behavior:

- `author_user_id` is derived from auth
- no caller-supplied workspace member id exists in the API

#### `PATCH /messages/{message_id}`

Edit a message.

Rules:

- only the original author may edit in v1

#### `DELETE /messages/{message_id}`

Soft delete a message.

Rules:

- only the original author may delete in v1

### Read State

#### `PUT /conversations/{conversation_id}/read-state`

Update the caller's read cursor.

Request:

```json
{
  "last_read_message_id": "msg_99"
}
```

Behavior:

- one row per `(conversation_id, user_id)`
- used for unread badges in both direct conversations and channels

### Events

#### `GET /events`

List external events visible to the authenticated caller.

Query params:

- `cursor`
- `limit`
- `workspace_id`
- `type`
- `resource_type`
- `resource_id`

Behavior:

- returns only `external_events`
- visibility is enforced at read time
- `workspace_id` is optional and narrows workspace-scoped events
- `resource_id` requires `resource_type`

Response shape:

```json
{
  "items": [
    {
      "id": 101,
      "workspace_id": "ws_01",
      "type": "conversation.message.created",
      "resource_type": "conversation",
      "resource_id": "conv_01",
      "occurred_at": "2026-04-04T20:00:00Z",
      "payload": {
        "message_id": "msg_01"
      }
    }
  ],
  "next_cursor": "cursor_123",
  "has_more": true
}
```

#### `GET /event-subscriptions`

List webhook subscriptions owned by the caller.

#### `POST /event-subscriptions`

Create a webhook subscription.

Request:

```json
{
  "workspace_id": "ws_01",
  "url": "https://hooks.example.com/teraslack",
  "event_type": "conversation.message.created",
  "resource_type": "conversation",
  "resource_id": "conv_01",
  "secret": "plaintext-shared-secret"
}
```

Rules:

- `resource_id` requires `resource_type`
- the caller may subscribe only to events they would be able to read
- the stored secret is encrypted at rest

#### `PATCH /event-subscriptions/{subscription_id}`

Update an event subscription.

Request:

```json
{
  "enabled": false
}
```

#### `DELETE /event-subscriptions/{subscription_id}`

Delete an event subscription.

## Server Logic

### Session Resolution

Every request resolves like this:

1. authenticate user
2. load conversation or workspace if required by route
3. apply workspace access rules when the resource is workspace-scoped
4. apply participant access rules when the resource is participant-scoped

There is no server-side concept of "selected workspace session".

### Mutation Transaction Rule

Every state-changing command uses this sequence:

1. authenticate and authorize
2. begin transaction
3. write primary tables
4. append one or more `internal_events`
5. commit transaction

The service never commits primary state without the matching internal event rows.

### Direct-Conversation Create Logic

#### Algorithm

1. authenticate caller user
2. collect unique participant user ids from the request
3. add caller user id
4. validate that the final participant set has at least two distinct users
5. if the final participant count is exactly 2:
6. sort the pair into canonical order
7. look up `direct_conversation_pairs`
8. if found, return the existing conversation
9. otherwise create `conversations(kind='direct_conversation')`
10. insert two `conversation_participants`
11. insert `direct_conversation_pairs`
12. return the conversation
13. if the final participant count is 3 or more:
14. create `conversations(kind='direct_conversation')`
15. insert `conversation_participants`
16. return the conversation

### Direct-Conversation Participant Mutation Logic

#### Rules

- participant add/remove is invalid for one-to-one direct messages
- participant add/remove is valid for group direct messages
- participant removal is invalid if it would reduce the participant count below 3

If a client wants to turn a one-to-one direct message into a group direct message, it creates a new direct conversation with the larger participant set.

### Channel Create Logic

#### Algorithm

1. authenticate caller user
2. load workspace membership
3. ensure caller may create channels
4. create `conversations(kind='channel', workspace_id=..., channel_visibility=...)`
5. if private, add creator to `conversation_participants`
6. return the channel

### Message Post Logic

#### Direct Conversation

1. authenticate caller user
2. load conversation
3. ensure `kind = direct_conversation`
4. ensure caller is a participant
5. insert message with `author_user_id`

#### Public Channel

1. authenticate caller user
2. load conversation
3. ensure `kind = channel` and `channel_visibility = public`
4. ensure caller has active workspace membership
5. insert message with `author_user_id`

#### Private Channel

1. authenticate caller user
2. load conversation
3. ensure `kind = channel` and `channel_visibility = private`
4. ensure caller has active workspace membership
5. ensure caller is a channel participant
6. insert message with `author_user_id`

### Internal Event Append Logic

Every command emits one or more internal events with:

- `event_type`
- `aggregate_type`
- `aggregate_id`
- `workspace_id` when applicable
- `actor_user_id`
- `payload`
- optional `metadata`

Examples:

- channel create -> `conversation.created`
- private-channel participant add -> `conversation.participant.added`
- direct-conversation message post -> `message.posted`
- workspace invite accept -> `workspace.membership.added`

Internal events are immutable facts. They are not edited after insertion.

### External Event Projection Logic

The external event projector runs asynchronously.

Algorithm:

1. claim owned internal-event shards
2. load each shard checkpoint
3. read internal events after the checkpoint
4. map each internal event to zero or more `external_events`
5. insert `external_events` idempotently using a dedupe key
6. insert resource-feed rows such as `workspace_event_feed`, `conversation_event_feed`, and `user_event_feed`
7. advance the shard checkpoint

Rules:

- projector work is replayable
- projector writes are idempotent
- projection failures are recorded in `external_event_projection_failures`
- projector retries do not create duplicate public events

### `/events` Read Logic

`GET /events` reads from `external_events` plus feed tables, not from `internal_events` directly.

Read-time filtering rules:

- workspace events use `workspace_event_feed`
- direct-conversation and channel events use `conversation_event_feed`
- user-scoped events use `user_event_feed`
- direct-conversation event visibility is based on conversation participation
- public-channel event visibility is based on active workspace membership
- private-channel event visibility is based on active workspace membership plus channel participation

This avoids write-time fanout to per-user inbox tables.

### Webhook Delivery Logic

Webhook delivery is downstream of `external_events`.

Flow:

1. the external event projector inserts `external_events`
2. webhook workers match enabled `event_subscriptions`
3. each matching subscription receives the external event envelope
4. delivery retries are keyed by subscription id plus external event id

Secrets are encrypted at rest and used only when signing outbound webhook requests.

### Conversation List Logic

#### Direct Inbox

The direct inbox query:

- filters `conversation_participants` by caller `user_id`
- joins conversations with `kind = direct_conversation`
- derives `direct_message_kind` from participant count
- orders by latest activity

#### Workspace Channels

The workspace channels query:

- loads all public channels in the workspace
- unions private channels where the caller is a participant
- orders by configured sort or latest activity

### Replay And Rebuild Logic

The system must support:

- replay of all `internal_events` from the beginning
- rebuild of all `external_events`
- rebuild of feed tables from `external_events`
- replay from a checkpoint for incremental projection recovery

Operationally this means:

- primary tables remain online serving state
- event-derived artifacts can be reconstructed after corruption or schema changes

## User Flows

### App Startup

1. Client loads with stored session token.
2. Client calls `GET /me`.
3. Client renders:
   - user identity
   - workspace list
4. Client calls `GET /direct-conversations`.
5. When the user enters a workspace, client calls `GET /workspaces/{workspace_id}/channels`.

### Start A One-to-One Direct Message From Global Search

1. User opens the direct-message composer.
2. Client calls `POST /search`.
3. User selects a user.
4. Client calls `POST /direct-conversations` with that one target user id.
5. Server returns the existing or newly created one-to-one direct message.
6. Client navigates to the conversation and calls `GET /conversations/{conversation_id}/messages`.

### Start A One-to-One Direct Message From A Workspace Member List

1. User opens a workspace.
2. Client calls `GET /workspaces/{workspace_id}/members`.
3. User selects a member.
4. Client reads the selected `user_id`.
5. Client calls `POST /direct-conversations` with that user id.
6. Server returns the canonical one-to-one direct message.
7. Client navigates to the direct inbox conversation.

The resulting conversation is still global. It does not belong to the workspace the user started from.

### Start A Group Direct Message

1. User opens the direct-message composer.
2. User selects multiple users.
3. Client calls `POST /direct-conversations` with the selected user ids.
4. Server creates a direct conversation with 3 or more participants.
5. Client opens the new group direct message.

### Create A Workspace

1. User submits workspace name and slug.
2. Client calls `POST /workspaces`.
3. Server creates:
   - workspace
   - owner membership for the creator
   - default `general` public channel
4. Client navigates into the workspace.

### Join A Workspace

1. User opens an invite link.
2. Client authenticates if needed.
3. Client calls `POST /workspace-invites/{token}/accept`.
4. Server activates or creates the membership.
5. Client navigates into the workspace channel list.

### Create A Public Channel

1. User is inside a workspace.
2. User opens the channel composer.
3. Client calls `POST /workspaces/{workspace_id}/channels` with `visibility = public`.
4. Server creates the channel.
5. The channel becomes visible to all workspace members.

### Create A Private Channel

1. User is inside a workspace.
2. User opens the channel composer.
3. Client calls `POST /workspaces/{workspace_id}/channels` with `visibility = private`.
4. Server creates the channel and adds the creator as a participant.
5. Creator may then add additional workspace members.

### Read A Public Channel

1. User enters a workspace.
2. Client calls `GET /workspaces/{workspace_id}/channels`.
3. User opens a public channel.
4. Client calls `GET /conversations/{channel_id}/messages`.
5. Client sends `PUT /conversations/{channel_id}/read-state` as the user reads.

### Read A Private Channel

1. User enters a workspace.
2. Client calls `GET /workspaces/{workspace_id}/channels`.
3. Only private channels the user belongs to are returned.
4. User opens the channel and reads messages normally.

### Send A Message

1. User writes text in a direct conversation or channel.
2. Client calls `POST /conversations/{conversation_id}/messages`.
3. Server derives `author_user_id` from auth.
4. Server applies direct-conversation or channel access rules.
5. Message is stored and returned.

### Consume Events

1. Client or integration stores the last `next_cursor` from `/events`.
2. Client calls `GET /events` with that cursor.
3. Server returns only visible external events after that point.
4. Consumer updates its checkpoint and processes each event idempotently.

This is the canonical pull-based integration flow.

### Receive Webhooks

1. User or integration creates an `event_subscription`.
2. The system stores the destination and encrypted secret.
3. When matching external events are projected, webhook workers enqueue deliveries.
4. The remote consumer verifies the signature and handles retries idempotently.

## Identity Rendering Rules

### In Direct Conversations

Render participants from:

- `users`
- `user_profiles`

Do not render direct conversations from workspace-local display records as the source of truth.

### In Channels

Render participants from:

- `users`
- `workspace_profiles` for the active workspace when present

This lets one user have a global direct-message identity and a workspace-local presentation without splitting the underlying identity model.

## Explicit Exclusions

The following patterns are intentionally not part of this design:

- a persistent `accounts` table as a second canonical identity
- a persistent `workspace_users` table as a second canonical identity
- separate persisted `dm` and `group_dm` conversation kinds
- workspace-owned direct messages
- user ids hidden behind workspace-local member ids in message APIs
- server-side selected workspace on the session
- creating guest workspace membership as a side effect of direct-conversation creation

## Acceptance Criteria

The design is correct when all of the following are true:

1. The same person or bot has one canonical `user_id` everywhere.
2. The API can create or fetch a one-to-one direct message by user pair.
3. One-to-one direct messages and group direct messages are both stored as `kind = direct_conversation`.
4. Direct conversations work without a workspace id.
5. Channels require a workspace id.
6. Public channels are visible to all active workspace members.
7. Private channels require both workspace membership and explicit channel participation.
8. Message authorship is always stored as `author_user_id`.
9. Starting a direct message from a workspace member list still results in a global direct conversation.
10. The system never needs a workspace-local member id to authorize or write a direct-conversation message.
11. Every successful state mutation appends immutable `internal_events` rows in the same transaction.
12. `/events` reads from projected `external_events`, not directly from primary tables or the raw internal log.
13. Event feeds and webhook deliveries can be rebuilt from `internal_events` plus projector checkpoints.
