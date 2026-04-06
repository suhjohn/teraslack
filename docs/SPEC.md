# Teraslack Core Messaging Spec

## Status

- Proposed greenfield specification
- Scope: identity model, database schema, HTTP API, authorization rules, and core user flows
- This document describes a clean rewrite of Teraslack's messaging model from scratch

## Product Thesis

Teraslack has one messaging primitive:

- conversations

Conversations can live in two scopes:

- global
- inside one workspace

The system should feel like this:

- a user is the real identity
- a conversation is the canonical message container
- a workspace is a collaboration container that scopes some conversations
- direct messages, group messages, and channels are derived views over one conversation model
- choosing a workspace scope is a caller-side concern, not an authentication prerequisite for global conversations

This removes the ambiguity that happens when a product has both a global user and a second workspace-local user identity, and it avoids modeling DMs and channels as separate storage concepts.

## Design Goals

- Use one canonical global identity model.
- Use one canonical conversation model.
- Infer conversation scope from `workspace_id` instead of duplicating scope state.
- Preserve an immutable internal event log for replay, audit, and asynchronous integrations.
- Keep authorization simple and explainable.
- Avoid duplicate identity tables that model the same person twice.
- Make the API resource-oriented and unsurprising.
- Let human users and agents share the same core identity and conversation model.

### Runtime And Deployment Baseline

- frontend remains a separate TypeScript app in `frontend/`
- backend remains a Go service set rooted in `server/`
- PostgreSQL remains the system-of-record database
- local development remains Docker Compose based
- deployment remains a multi-service Railway layout
- the backend remains split into separate Go binaries for the API server and background workers

Expected long-running services:

- `frontend`
- `server`
- `external-event-projector`
- `webhook-producer`
- `webhook-worker`
- `indexer`

### Frontend Stack Baseline

- React 19
- TypeScript
- Bun for package management and frontend scripts
- Vite
- TanStack Start
- TanStack Router with file-based routes
- TanStack Query
- Tailwind CSS v4
- Radix UI primitives where custom product-styled controls are needed
- Lucide icons
- Nitro runtime integration used by the TanStack Start setup
- Vitest and Testing Library for frontend tests
- ESLint and Prettier for linting and formatting

### Frontend API Contract Baseline

- generated client code lives under `frontend/src/lib/openapi/`
- `orval` generates the frontend API client from `server/api/openapi.yaml`
- custom fetch behavior continues to live in `frontend/src/lib/orval-mutator.ts`

### Frontend Folder Structure Baseline

- `frontend/src/routes/` for route files and route-local screens
- `frontend/src/components/` for shared UI and feature components
- `frontend/src/lib/` for API wrappers, generated clients, utilities, and non-visual helpers
- `frontend/src/styles.css` for app-wide styling entrypoint
- `frontend/src/routeTree.gen.ts` remains generated output, not handwritten architecture

### Backend Stack Baseline

- Go 1.25
- standard `net/http` server flow with generated OpenAPI bindings
- `oapi-codegen` for strict server interfaces and API models
- PostgreSQL 16
- `pgx/v5` for Postgres access
- `golang-migrate` for schema migrations
- `sqlc` for query code generation
- `kin-openapi` for OpenAPI tooling and validation
- AWS SDK v2 for S3-compatible object storage integration
- object-storage queue files with CAS writes and per-process in-memory batching
- `testcontainers-go` for integration-heavy backend tests

### Supporting Integrations Baseline

- Resend for email auth delivery
- Google OAuth
- GitHub OAuth
- S3-compatible object storage
- Turbopuffer for search/indexing

### Backend Folder Structure Baseline

- `server/api/` for the OpenAPI contract and generation config
- `server/cmd/` for binary entrypoints
- `server/internal/domain/` for core domain types, enums, and generated shared contracts
- `server/internal/service/` for application services and write/read orchestration
- `server/internal/handler/` for HTTP adapters and request/response wiring
- `server/internal/repository/` for persistence interfaces, migrations, queries, and generated DB access
- `server/internal/eventsourcing/` for the internal event log and projection machinery
- `server/internal/queue/` for background queue producer and worker logic
- `server/internal/search/` for search indexing integrations
- `server/internal/s3/` for object storage integration
- `server/internal/openapicli/` for CLI exposure of the API contract
- `server/internal/teraslackmcp/` and `server/internal/teraslackstdio/` for MCP and stdio-facing server integrations
- `server/pkg/` only for truly reusable non-domain helper packages

### Backend Binary Baseline

- `server`
- `external-event-projector`
- `webhook-producer`
- `webhook-worker`
- `indexer`
- codegen and admin helpers may exist as separate commands when needed

### Root Repository Layout Baseline

- `docs/` for specs and supporting architecture docs
- `frontend/` for the web app
- `server/` for the Go backend and worker binaries
- `scripts/` for release and integration scripts
- `docker-compose.yml` and `docker-compose.dev.yml` for local orchestration
- root `Makefile` for common dev, deploy, and release workflows

### Contract And Codegen Baseline

- `server/api/openapi.yaml` remains the canonical HTTP API contract
- backend bindings are generated into `server/internal/api/openapi.gen.go`
- frontend API client code is generated from the same OpenAPI document
- SQL query code is generated from checked-in SQL and migrations, not handwritten ad hoc data access
- generated files remain checked in where that was the v0 workflow

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
- conversations
- settings

Workspaces do not own global conversations.

### Workspace Member

A `WorkspaceMember` is not a separate identity. It is a workspace-scoped projection of a user.

It combines:

- the underlying user
- the user's membership in one workspace

Important rule:

- there is no canonical persisted `WorkspaceUser` identity separate from `User`

If the product needs a workspace-facing member shape, it is an API projection, not a second identity model.

### Conversation

A `Conversation` is a message container.

Rules:

- `workspace_id IS NULL` means the conversation is global
- `workspace_id IS NOT NULL` means the conversation belongs to exactly one workspace

### Conversation Access Policy

A `Conversation` has one access policy:

- `members`
- `workspace`
- `authenticated`

Rules:

- `members` means only explicit conversation participants may access the conversation
- `workspace` means any active member of `workspace_id` may access the conversation
- `authenticated` means any authenticated user may access the conversation
- `workspace` is valid only when `workspace_id IS NOT NULL`
- `authenticated` is valid only when `workspace_id IS NULL`

### Derived Conversation Forms

The product still exposes direct messages and channels, but they are derived views over one persisted `conversation`.

Derived forms:

- one-to-one direct message: global conversation with `access_policy = members`, exactly 2 participants, and a row in `conversation_pairs`
- private global conversation: global conversation with `access_policy = members` that is not a canonical one-to-one direct message
- workspace-wide channel: workspace conversation with `access_policy = workspace`
- workspace private channel: workspace conversation with `access_policy = members`
- global named channel: global conversation with `access_policy = authenticated`

Rules:

- one-to-one direct messages are canonical by unordered user pair
- other member-only conversations are not canonical by participant set or title
- a non-DM member-only conversation stays non-DM even if its participant count later becomes 2
- private global conversations may be presented in the product as a group chat or a private channel

### Participant

A `Participant` is a user that is explicitly a member of a conversation.

Rules:

- all `members` conversations use explicit participants
- `workspace` and `authenticated` conversations do not require participant rows for visibility

### Message

A `Message` is authored by a user and belongs to one conversation.

Messages do not need a workspace-local author id.

## Canonical Product Rules

1. `User` is the only canonical messaging identity.
2. `Conversation` is the only canonical messaging container.
3. Conversation scope is inferred from `workspace_id`.
4. Conversation visibility is determined by `access_policy`.
5. A workspace member is a user in a workspace, not a new identity.
6. The server never requires a workspace-selected session to access a global conversation.
7. The server never creates workspace membership as a side effect of global conversation creation.
8. One-to-one direct messages are canonical by unordered user pair.
9. Direct messages and channels are derived views over one persisted `conversation` model.
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
- malformed internal events are recorded as projection failures and skipped so later events in the shard can continue

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

Interactive login methods are:

- email verification via Resend
- Google OAuth
- GitHub OAuth

All interactive login methods must:

- resolve to one canonical `user_id`
- create the user on first successful login when needed
- issue the same session token type

### API-First Auth Shape

The canonical auth contract is bearer-first:

- `Authorization: Bearer <session_token_or_api_key>`

Rules:

- email and OAuth are login methods, not separate runtime auth models
- successful login returns a session token
- API clients may use that session token directly as a bearer token
- browsers may additionally store the same session token in a secure cookie, but bearer semantics remain the canonical API contract
- API keys are the separate long-lived programmatic auth mechanism

### Workspace Access

Workspace access is determined by `workspace_memberships`.

A caller can access workspace resources only if their membership in that workspace is active.

### Conversation Access Matrix

#### global conversation with `access_policy = members`

Access rule:

- caller must be a participant user

#### workspace conversation with `access_policy = workspace`

Access rule:

- caller must have active membership in the workspace

#### workspace conversation with `access_policy = members`

Access rule:

- caller must have active membership in the workspace
- caller must also be an explicit participant of the conversation

#### global conversation with `access_policy = authenticated`

Access rule:

- caller must be authenticated

### Workspace Roles

Workspace roles are intentionally simple:

- `owner`
- `admin`
- `member`

Rules:

- `owner` can do all workspace operations
- `admin` can manage members and workspace conversations but cannot remove the last owner
- `member` can read and post according to conversation access policy

No workspace role ever grants access to a global `members` conversation the caller is not part of.

## Database Schema

### ID Format

All primary ids for domain entities use UUIDv4.

Rules:

- entity ids are generated as UUIDv4
- entity ids are stored in the database as raw 16-byte values, not text
- APIs serialize entity ids using canonical UUID string form
- application code should treat entity ids as UUID values, not arbitrary strings
- event-log and feed ordering tables use monotonically increasing bigint ids instead of UUIDs

Examples:

- `550e8400-e29b-41d4-a716-446655440000`
- `7c45a4b8-7d2f-4d2f-a8d4-3f6f9cbb7c12`
- `2f1c6b1a-9d8e-4c3b-a6f2-1b5d9e8c7a44`

Use UUIDv4 entity ids for:

- `users.id`
- `user_profiles.user_id`
- `auth_sessions.id`
- `email_login_challenges.id`
- `oauth_accounts.id`
- `api_keys.id`
- `workspaces.id`
- `workspace_memberships.id`
- `workspace_invites.id`
- `conversations.id`
- `conversation_pairs.conversation_id`
- `conversation_participants.conversation_id`
- `conversation_participants.user_id`
- `conversation_invites.id`
- `conversation_reads.conversation_id`
- `conversation_reads.user_id`
- `messages.id`
- `messages.conversation_id`
- `messages.author_user_id`
- `event_subscriptions.id`

Use monotonically increasing bigint ids for:

- `internal_events.id`
- `external_events.id`
- `external_event_projection_failures.id`

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

Workspace scope is chosen by the caller on each request. The server does not persist a selected workspace on the session.

#### `email_login_challenges`

Short-lived email verification challenges used by `email/start` and `email/verify`.

Fields:

- `id` PK
- `email`
- `code_hash`
- `expires_at`
- `consumed_at` nullable
- `created_at`

Rules:

- challenges are single-use
- challenges are short-lived
- plaintext codes are never persisted
- delivery is performed through Resend

#### `oauth_accounts`

External OAuth identities linked to users.

Fields:

- `id` PK
- `provider` enum: `google | github`
- `provider_user_id`
- `user_id` FK `users.id`
- `email` nullable
- `created_at`
- `updated_at`

Constraints:

- unique `(provider, provider_user_id)`

Rules:

- one user may have multiple linked OAuth identities
- OAuth login resolves by `(provider, provider_user_id)` first
- when no linked row exists, the system may attach by verified email or create a new user according to product policy

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

- `scope_type = user` means the key can act as the user across global and workspace conversation surfaces the user may access
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
- `shard_id`
- `payload` JSONB
- `created_at`

Rules:

- append-only
- never updated in place
- `payload` contains the event snapshot needed for replay or projection

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
- `workspace_id` nullable FK `workspaces.id`
- `access_policy` enum: `members | workspace | authenticated`
- `title` nullable
- `description` nullable
- `created_by_user_id` FK `users.id`
- `archived_at` nullable
- `last_message_at` nullable
- `created_at`
- `updated_at`

Constraints:

- `workspace_id IS NULL` implies `access_policy IN ('members', 'authenticated')`
- `workspace_id IS NOT NULL` implies `access_policy IN ('members', 'workspace')`

Rules:

- one-to-one direct-message titles are derived from the other participant
- other conversation titles may be derived or stored directly
- member-only conversations may be untitled
- workspace-wide and authenticated conversations should have a stored title

#### `conversation_pairs`

Canonical lookup table for one-to-one conversations.

Fields:

- `conversation_id` PK, FK `conversations.id`
- `first_user_id` FK `users.id`
- `second_user_id` FK `users.id`

Constraints:

- unique `(first_user_id, second_user_id)`
- `first_user_id <> second_user_id`
- `first_user_id < second_user_id` lexicographically

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

- required for all conversations with `access_policy = members`
- optional and normally unused for conversations with `access_policy = workspace`
- optional and normally unused for conversations with `access_policy = authenticated`

#### `conversation_invites`

Reusable invite links for member-only conversations.

Fields:

- `id` PK
- `conversation_id` FK `conversations.id`
- `created_by_user_id` FK `users.id`
- `token_hash` unique
- `expires_at` nullable
- `mode` enum: `link | restricted`
- `allowed_user_ids` nullable JSONB
- `allowed_emails` nullable JSONB
- `revoked_at` nullable
- `created_at`

Rules:

- `expires_at = null` means the invite does not expire
- invites are reusable until they expire or are revoked
- invites are valid only for conversations with `access_policy = members`
- invites are invalid for canonical one-to-one direct messages
- `mode = link` means any authenticated user with the token may join
- `mode = restricted` means only callers matching `allowed_user_ids` or their verified email in `allowed_emails` may join
- `mode = restricted` requires at least one allowed user id or allowed email
- accepting an invite adds the caller to `conversation_participants`
- accepting an invite for a workspace conversation still requires active workspace membership

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
- `dedupe_key`
- `created_at`

Rules:

- `workspace_id` is nullable for global conversation events
- `resource_type` is a public-facing type, not necessarily the internal aggregate type
- message lifecycle events are conversation-scoped and therefore use `resource_type = conversation`
- `dedupe_key` makes projector writes idempotent

Recommended resource types:

- `user`
- `workspace`
- `conversation`

#### `workspace_event_feed`

Resource feed index for workspace-scoped events.

Fields:

- `workspace_id` FK `workspaces.id`
- `external_event_id` FK `external_events.id`

Unique key:

- `(workspace_id, external_event_id)`

#### `conversation_event_feed`

Resource feed index for conversation-scoped events.

Fields:

- `conversation_id` FK `conversations.id`
- `external_event_id` FK `external_events.id`

Unique key:

- `(conversation_id, external_event_id)`

#### `user_event_feed`

Resource feed index for user-scoped events.

Fields:

- `user_id` FK `users.id`
- `external_event_id` FK `external_events.id`

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

1. Every conversation with `access_policy = members` has at least one participant user.
2. A global `members` conversation is a one-to-one direct message only when it has exactly two participants and a matching row in `conversation_pairs`.
3. A global `members` conversation without a matching row in `conversation_pairs` is a private member-only conversation and may start with one participant.
4. Every row in `conversation_pairs` references a global `members` conversation with exactly two participants.
5. A workspace conversation with `access_policy = workspace` has no participant requirement for visibility.
6. Every participant in a workspace conversation with `access_policy = members` must also be an active workspace member of the same workspace.
7. A global conversation never has `access_policy = workspace`.
8. A workspace conversation never has `access_policy = authenticated`.
9. Deleting or leaving a workspace does not delete a user's global conversations.
10. `internal_events` is append-only.
11. `external_events` is idempotent on projection scope plus `dedupe_key`.
12. Every event-feed row points to exactly one `external_events` row.
13. Projector checkpoints advance monotonically.
14. Every `conversation_invites` row references a member-only conversation that is not a canonical one-to-one direct message.

## Recommended Indexes

- `users(email)`
- `user_profiles(handle)`
- `workspace_memberships(workspace_id, user_id)`
- `workspace_memberships(user_id, status)`
- `conversations(workspace_id, access_policy, updated_at DESC)`
- `conversations(access_policy, updated_at DESC)`
- `conversation_participants(user_id, conversation_id)`
- `conversation_invites(token_hash)`
- `conversation_invites(conversation_id, created_at DESC)`
- `conversation_reads(user_id, conversation_id)`
- `messages(conversation_id, created_at DESC)`
- `conversation_pairs(first_user_id, second_user_id)`
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
- Bearer auth is used for both session tokens and API keys
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

### Validation Error Shape

```json
{
  "code": "validation_failed",
  "message": "Request validation failed.",
  "request_id": "req_123",
  "errors": [
    {
      "field": "email",
      "code": "invalid_format",
      "message": "Must be a valid email address."
    }
  ]
}
```

### Collection Shape

```json
{
  "items": [],
  "next_cursor": "cursor_123"
}
```

Rules:

- `next_cursor` is omitted when there is no next page
- collection routes use `next_cursor` as the only pagination continuation signal
- collection routes do not also return a parallel `has_more` boolean

### HTTP Status Semantics

- `200` successful read or mutation with a response body
- `201` successful create when a new resource is persisted
- `202` accepted asynchronous side effect, such as login challenge dispatch
- `204` successful mutation with no response body
- `400` malformed JSON, malformed query params, or unsupported request syntax
- `401` missing or invalid bearer token
- `403` authenticated caller lacks permission
- `404` resource does not exist
- `409` request conflicts with current resource state
- `422` well-formed request fails semantic validation
- `429` rate limited
- `500` unexpected server error

Rules:

- malformed JSON always returns `400`
- semantic validation failures return `422` with the validation error shape when field-level detail is available
- routes that intentionally return a different success code call that out explicitly in their section

### Rate Limits

Authentication routes are rate limited aggressively. Authenticated APIs use very high limits and are not intended to be product-throttled in normal use.

Rules:

- unauthenticated authentication routes are limited by client IP
- email login routes may additionally be limited by normalized email address
- authenticated API requests are limited by authenticated user id
- authenticated API key requests are limited by API key id
- authenticated limits are intentionally high and exist only to protect infrastructure from abuse or accidental loops
- exceeding authenticated limits should be rare in normal operation
- if both bearer auth and cookie auth are present, bearer auth takes precedence and cookies are ignored

Recommended defaults:

- `POST /auth/email/start`: `20 requests/hour/IP` and `5 requests/hour/email`
- `POST /auth/email/verify`: `30 requests/hour/IP`
- `POST /auth/oauth/google/start`: `30 requests/hour/IP`
- `POST /auth/oauth/github/start`: `30 requests/hour/IP`
- authenticated user requests: `1000 requests/minute/user`
- authenticated API key requests: `5000 requests/minute/key`

## Resource Shapes

### User

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
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
  "workspace_id": "7c45a4b8-7d2f-4d2f-a8d4-3f6f9cbb7c12",
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "role": "member",
  "status": "active",
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "principal_type": "human",
    "profile": {
      "handle": "jane",
      "display_name": "Jane"
    }
  }
}
```

### Conversation

```json
{
  "id": "2f1c6b1a-9d8e-4c3b-a6f2-1b5d9e8c7a44",
  "workspace_id": null,
  "access_policy": "members",
  "participant_count": 2,
  "title": null,
  "description": null,
  "created_by_user_id": "550e8400-e29b-41d4-a716-446655440000",
  "last_message_at": "2026-04-04T20:00:00Z",
  "created_at": "2026-04-01T10:00:00Z",
  "updated_at": "2026-04-04T20:00:00Z"
}
```

Rules:

- one-to-one direct messages are derived from `workspace_id = null`, `access_policy = members`, and `participant_count = 2`
- `title` is commonly null for one-to-one direct messages because it is derived from the other participant

### Message

```json
{
  "id": "9a3d2f10-6e4b-4f97-8c21-0f4d7b2e6a11",
  "conversation_id": "2f1c6b1a-9d8e-4c3b-a6f2-1b5d9e8c7a44",
  "author_user_id": "550e8400-e29b-41d4-a716-446655440000",
  "body_text": "hello",
  "created_at": "2026-04-04T20:00:00Z"
}
```

## HTTP API

### Auth

#### `POST /auth/email/start`

Start email login.

Request:

```json
{
  "email": "user@example.com"
}
```

Behavior:

- creates a short-lived login challenge
- sends the code via Resend
- returns a generic success response to avoid email enumeration

Response:

```json
{
  "status": "ok"
}
```

#### `POST /auth/email/verify`

Verify email login and create a session.

Request:

```json
{
  "email": "user@example.com",
  "code": "123456"
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
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "principal_type": "human"
  }
}
```

Behavior:

- verifies the one-time code
- resolves or creates the canonical user
- creates a session token

#### `POST /auth/oauth/google/start`

Start Google OAuth.

Request:

```json
{
  "redirect_uri": "https://app.example.com/auth/google/callback"
}
```

Response:

```json
{
  "auth_url": "https://accounts.google.com/...",
  "state": "opaque-state"
}
```

#### `GET /auth/oauth/google/callback`

Complete Google OAuth.

Behavior:

- exchanges the provider code
- resolves or creates the canonical user
- creates a session token
- redirects to the client or returns a session payload depending on client mode

#### `POST /auth/oauth/github/start`

Start GitHub OAuth.

Request:

```json
{
  "redirect_uri": "https://app.example.com/auth/github/callback"
}
```

Response:

```json
{
  "auth_url": "https://github.com/login/oauth/authorize?...",
  "state": "opaque-state"
}
```

#### `GET /auth/oauth/github/callback`

Complete GitHub OAuth.

Behavior:

- exchanges the provider code
- resolves or creates the canonical user
- creates a session token
- redirects to the client or returns a session payload depending on client mode

#### `DELETE /auth/sessions/current`

Revoke the current session.

#### `GET /me`

Return the authenticated bootstrap surface.

Rules:

- `/me` is the only current-user read surface in v1
- it returns both the caller's canonical user shape and enough workspace membership data to bootstrap subsequent API interactions

Response:

```json
{
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "principal_type": "human",
    "status": "active",
    "profile": {
      "handle": "jane",
      "display_name": "Jane"
    }
  },
  "workspaces": [
    {
      "workspace_id": "7c45a4b8-7d2f-4d2f-a8d4-3f6f9cbb7c12",
      "role": "owner",
      "status": "active",
      "name": "Acme"
    }
  ]
}
```

### Current User

#### `PATCH /me/profile`

Update the global user profile used for global conversations.

Request:

```json
{
  "display_name": "Jane",
  "avatar_url": "https://..."
}
```

### API Keys

#### `GET /api-keys`

List API keys owned by the authenticated user.

#### `POST /api-keys`

Create an API key.

Request:

```json
{
  "label": "local-dev",
  "scope_type": "user",
  "scope_workspace_id": null
}
```

Response:

```json
{
  "api_key": {
    "id": "3d6f0c71-22f4-4fd7-b0f7-03d6a9c1e882",
    "label": "local-dev",
    "scope_type": "user"
  },
  "secret": "plaintext-once"
}
```

#### `DELETE /api-keys/{key_id}`

Revoke an API key.

### Search

#### `POST /search`

Unified search across all caller-visible entities.

Request:

```json
{
  "query": "jane",
  "entity_types": ["user", "workspace", "conversation"],
  "workspace_id": "7c45a4b8-7d2f-4d2f-a8d4-3f6f9cbb7c12",
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
- `conversation`

Result shape:

```json
{
  "items": [
    {
      "entity_type": "user",
      "id": "550e8400-e29b-41d4-a716-446655440000",
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
- `conversation` results include only conversations visible to the caller

This endpoint supports caller-driven entity discovery, including user lookup for direct-message creation and workspace lookup, without adding resource-specific search routes.

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
- the system creates a default `general` workspace-wide conversation

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

Behavior:

- the server creates an invite token bound to the target workspace
- the server returns an invite token or invite URL that may be distributed out of band

#### `POST /workspace-invites/{token}/accept`

Accept a workspace invite as the authenticated user.

Behavior:

- the invite token must exist, be unexpired, and not be revoked
- the server activates an existing invited membership or creates a new active membership for the caller
- the route is idempotent when the caller is already an active member
- the server returns the caller's active `WorkspaceMember` projection for the workspace

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

### Conversations

#### `GET /conversations`

List conversations visible to the authenticated user.

Query params:

- `cursor`
- `limit`
- `workspace_id`
- `access_policy`

Rules:

- when `workspace_id` is omitted, the route returns global conversations visible to the caller
- when `workspace_id` is present, the route returns conversations in that workspace visible to the caller
- ordered by most recent message activity

#### `POST /conversations`

Create a conversation.

Special case:

- when the request describes a canonical one-to-one global direct message and that pair already exists, the server returns the existing conversation instead of creating a duplicate

Request:

```json
{
  "workspace_id": null,
  "access_policy": "members",
  "participant_user_ids": [],
  "title": null,
  "description": null
}
```

Behavior:

- valid access policies are `members`, `workspace`, and `authenticated`
- authenticated user is implicitly included when `access_policy = members`
- conversations with `access_policy = members` must contain at least one distinct user after the caller is added
- conversations with `access_policy = workspace` or `authenticated` must omit `participant_user_ids`
- `workspace_id = null` with `access_policy = members` and exactly one user creates a private global conversation with only the caller
- `workspace_id = null` with `access_policy = members` and exactly two users returns the canonical one-to-one direct message for that pair
- `workspace_id = null` with `access_policy = members` and three or more users creates a private global conversation
- `workspace_id != null` with `access_policy = members` creates a workspace private conversation
- `workspace_id != null` with `access_policy = workspace` creates a workspace-wide conversation
- `workspace_id = null` with `access_policy = authenticated` creates a global named conversation
- conversations with `access_policy = workspace` or `authenticated` require a stored `title`
- all participant users in a workspace conversation must already be active workspace members

Response semantics:

- `201` when a new conversation is created
- `200` when the canonical one-to-one direct message for the requested pair already exists and is returned

#### `GET /conversations/{conversation_id}`

Return a conversation if visible to the caller.

#### `PATCH /conversations/{conversation_id}`

Update conversation metadata.

Request:

```json
{
  "title": "ops-platform",
  "description": "Ops and platform coordination",
  "archived": false
}
```

Rules:

- metadata routes operate on all conversation forms
- ownership and admin policy can vary by scope; in v1, workspace owner/admin may manage workspace conversations

#### `GET /conversations/{conversation_id}/participants`

List explicit participants in a conversation.

Rules:

- canonical participant rows exist only for conversations with `access_policy = members`
- for `workspace` and `authenticated` conversations this route returns an empty collection because there are no explicit participant rows to list

#### `POST /conversations/{conversation_id}/participants`

Add participants to a member-only conversation.

Request:

```json
{
  "user_ids": ["user_02", "user_03"]
}
```

Rules:

- valid only when `access_policy = members`
- invalid for canonical one-to-one direct messages
- all added users in a workspace conversation must already be active workspace members

#### `DELETE /conversations/{conversation_id}/participants/{user_id}`

Remove a participant from a member-only conversation.

Rules:

- valid only when `access_policy = members`
- invalid for canonical one-to-one direct messages
- invalid if removal would leave the conversation with zero participants

### Conversation Invites

#### `POST /conversations/{conversation_id}/invites`

Create an invite link for a member-only conversation.

Request:

```json
{
  "expires_at": null,
  "mode": "link",
  "allowed_user_ids": null,
  "allowed_emails": null
}
```

Rules:

- valid only when `access_policy = members`
- invalid for canonical one-to-one direct messages
- `expires_at = null` means the invite does not expire
- `mode = link` means any authenticated user with the token may join
- `mode = restricted` means only callers matching `allowed_user_ids` or their verified email in `allowed_emails` may join
- `mode = restricted` requires at least one allowed user id or allowed email
- invite creation returns a token or invite URL that can be shared

#### `POST /conversation-invites/{token}/accept`

Join a conversation using an invite link.

Rules:

- invite must exist, be unexpired, and not be revoked
- restricted invites require the authenticated caller to match `allowed_user_ids` or their verified email in `allowed_emails`
- if the caller is already a participant, the route may succeed idempotently
- accepting the invite adds the caller to `conversation_participants`
- accepting an invite for a workspace conversation still requires active workspace membership
- the server returns the target `Conversation` resource after access has been granted

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
  "last_read_message_id": "9a3d2f10-6e4b-4f97-8c21-0f4d7b2e6a11"
}
```

Behavior:

- one row per `(conversation_id, user_id)`
- used for unread badges across all conversation forms

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
      "workspace_id": "7c45a4b8-7d2f-4d2f-a8d4-3f6f9cbb7c12",
      "type": "conversation.message.created",
      "resource_type": "conversation",
      "resource_id": "2f1c6b1a-9d8e-4c3b-a6f2-1b5d9e8c7a44",
      "occurred_at": "2026-04-04T20:00:00Z",
      "payload": {
        "message_id": "9a3d2f10-6e4b-4f97-8c21-0f4d7b2e6a11"
      }
    }
  ],
  "next_cursor": "cursor_123"
}
```

#### `GET /event-subscriptions`

List webhook subscriptions owned by the caller.

#### `POST /event-subscriptions`

Create a webhook subscription.

Request:

```json
{
  "workspace_id": "7c45a4b8-7d2f-4d2f-a8d4-3f6f9cbb7c12",
  "url": "https://hooks.example.com/teraslack",
  "event_type": "conversation.message.created",
  "resource_type": "conversation",
  "resource_id": "2f1c6b1a-9d8e-4c3b-a6f2-1b5d9e8c7a44",
  "secret": "plaintext-shared-secret"
}
```

Rules:

- `resource_id` requires `resource_type`
- the caller may subscribe only to events they would be able to read
- `resource_type` must be one of `conversation`, `user`, or `workspace`
- `secret` must not be blank
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
4. apply conversation access rules when the resource is member-scoped or authenticated-global

There is no server-side concept of "selected workspace session".

### Mutation Transaction Rule

Every state-changing command uses this sequence:

1. authenticate and authorize
2. begin transaction
3. write primary tables
4. append one or more `internal_events`
5. commit transaction

The service never commits primary state without the matching internal event rows.

### Conversation Create Logic

#### Algorithm

1. authenticate caller user
2. validate `workspace_id` and `access_policy` as a legal combination
3. if `workspace_id IS NOT NULL`, load workspace membership and ensure the caller may create conversations there
4. if `access_policy = members`, collect unique participant user ids from the request
5. if `access_policy = members`, add caller user id
6. if `access_policy = members`, validate that the final participant set has at least one distinct user
7. if `workspace_id IS NOT NULL` and `access_policy = members`, ensure every participant is an active workspace member
8. if `workspace_id IS NULL` and `access_policy = members` and the final participant count is exactly 2:
9. sort the pair into canonical lexicographic order
10. look up `conversation_pairs`
11. if found, return the existing conversation
12. otherwise create `conversations(workspace_id=NULL, access_policy='members')`
13. insert two `conversation_participants`
14. insert `conversation_pairs`
15. return the conversation
16. otherwise create `conversations(...)`
17. if `access_policy = members`, insert `conversation_participants`
18. return the conversation

### Conversation Participant Mutation Logic

#### Rules

- participant add/remove is valid only when `access_policy = members`
- participant add/remove is invalid for canonical one-to-one direct messages
- participant removal is invalid if it would leave the conversation with zero participants

If a caller wants to turn a one-to-one direct message into a larger private conversation, it creates a new conversation with the larger participant set.

### Conversation Invite Accept Logic

1. authenticate caller user
2. load invite by token and ensure it is not expired or revoked
3. load the target conversation and ensure `access_policy = members`
4. reject the request if the conversation is a canonical one-to-one direct message
5. if the invite is restricted, ensure the caller matches `allowed_user_ids` or their verified email in `allowed_emails`
6. if the conversation is workspace-scoped, ensure the caller has active workspace membership
7. insert `conversation_participants` idempotently for the caller

### Message Post Logic

#### global conversation with `access_policy = members`

1. authenticate caller user
2. load conversation
3. ensure `workspace_id IS NULL` and `access_policy = members`
4. ensure caller is a participant
5. insert message with `author_user_id`

#### workspace conversation with `access_policy = workspace`

1. authenticate caller user
2. load conversation
3. ensure `workspace_id IS NOT NULL` and `access_policy = workspace`
4. ensure caller has active workspace membership
5. insert message with `author_user_id`

#### workspace conversation with `access_policy = members`

1. authenticate caller user
2. load conversation
3. ensure `workspace_id IS NOT NULL` and `access_policy = members`
4. ensure caller has active workspace membership
5. ensure caller is a conversation participant
6. insert message with `author_user_id`

#### global conversation with `access_policy = authenticated`

1. authenticate caller user
2. load conversation
3. ensure `workspace_id IS NULL` and `access_policy = authenticated`
4. insert message with `author_user_id`

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

- conversation create -> `conversation.created`
- member-only conversation participant add -> `conversation.participant.added`
- one-to-one direct message post -> `message.posted`
- workspace invite accept -> `workspace.membership.added`

Internal events are immutable facts. They are not edited after insertion.

### External Event Projection Logic

The external event projector runs asynchronously.

Algorithm:

1. claim owned internal-event shards
2. load each shard checkpoint
3. read internal events after the checkpoint
4. enqueue projection jobs into the object-storage queue
5. projector workers claim jobs from the queue, map each internal event to zero or more `external_events`, and write feed rows
6. insert `external_events` idempotently using a dedupe key
7. advance the shard checkpoint only after the enqueue succeeds

Rules:

- projector work is replayable
- projector writes are idempotent
- projection failures are recorded in `external_event_projection_failures`
- malformed internal events do not block later events in the same shard
- projector retries do not create duplicate public events

### `/events` Read Logic

`GET /events` reads from `external_events` plus feed tables, not from `internal_events` directly.

Read-time filtering rules:

- workspace events use `workspace_event_feed`
- conversation events use `conversation_event_feed`
- user-scoped events use `user_event_feed`
- global `members` conversation visibility is based on conversation participation
- workspace `workspace` conversation visibility is based on active workspace membership
- workspace `members` conversation visibility is based on active workspace membership plus conversation participation
- global `authenticated` conversation visibility is based on authentication

This avoids write-time fanout to per-user inbox tables.

### Webhook Delivery Logic

Webhook delivery is downstream of `external_events`.

Flow:

1. the external event projector inserts `external_events`
2. the webhook producer advances an `external_events` checkpoint and enqueues one delivery job per matching subscription into the object-storage queue
3. queue state is persisted to S3-compatible storage with compare-and-set writes
4. webhook workers claim jobs, heartbeat while in flight, and ack or retry back into the same queue
5. each matching subscription receives the external event envelope

Secrets are encrypted at rest and used only when signing outbound webhook requests.

### Conversation List Logic

#### Global Conversations

The global conversation query:

- loads global conversations with `workspace_id IS NULL`
- includes member-only conversations where the caller is a participant
- includes authenticated-global conversations
- orders by latest activity

#### Workspace Conversations

The workspace conversation query:

- loads all workspace-wide conversations in the workspace
- unions member-only workspace conversations where the caller is a participant
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

## API Interaction Flows

These examples describe caller-server interactions.

Rules:

- `User A` and `User B` refer to human or agent principals
- `caller` refers to any API consumer acting on behalf of an authenticated user
- UI concerns such as rendering, navigation, and link presentation are intentionally out of scope unless required by an auth redirect flow

### Bootstrap Authenticated State

1. User A already has a valid session token or API key.
2. The caller sends `GET /me`.
3. The server returns User A's canonical user shape plus active workspace memberships.
4. The caller may send `GET /conversations` to load global conversations visible to User A.
5. The caller may send `GET /conversations?workspace_id={workspace_id}` for any workspace scope it wants to inspect.

### Sign In With Email

1. The caller sends `POST /auth/email/start` with User A's email address.
2. The server creates a login challenge, sends the code through Resend, and returns a generic accepted response.
3. User A receives the verification code through email.
4. The caller sends `POST /auth/email/verify` with User A's email address and code.
5. The server verifies the challenge, resolves or creates User A, and returns a session token.

### Sign In With Google Or GitHub

1. The caller sends `POST /auth/oauth/{provider}/start`.
2. The server returns an `auth_url` and `state`.
3. User A follows the provider authorization flow at `auth_url`.
4. The provider redirects back to `GET /auth/oauth/{provider}/callback`.
5. The server resolves or creates User A and creates a session.
6. The caller uses the returned session token for subsequent authenticated requests.

### Resolve A One-to-One Direct Message Between User A And User B

1. User A decides to start a one-to-one direct message with User B.
2. The caller obtains User B's `user_id` through any valid discovery path, such as `POST /search`, `GET /workspaces/{workspace_id}/members`, or previously stored state.
3. The caller sends `POST /conversations` with `workspace_id = null`, `access_policy = members`, and `participant_user_ids = ["{user_b_id}"]`.
4. The server implicitly includes User A, canonicalizes the unordered pair `(User A, User B)`, and returns the canonical direct message conversation.
5. The caller may then send `GET /conversations/{conversation_id}/messages` to read or continue the direct message.

The resulting conversation is global. It does not belong to any workspace even if User B was discovered through a workspace-scoped query.

### Start A Private Global Conversation

1. User A decides to create a private global conversation.
2. The caller sends `POST /conversations` with `workspace_id = null`, `access_policy = members`, and the selected `participant_user_ids`.
3. The server implicitly includes User A in the participant set.
4. If no other users were supplied, the server creates a private global conversation with only User A.
5. If exactly one other user was supplied, the server returns the canonical one-to-one direct message for User A and that user.
6. If two or more other users were supplied, the server creates a private global conversation for the final participant set.
7. The caller may then read or mutate the returned conversation normally.

This conversation may be presented in the product as a group chat or a private channel.

### Create A Conversation Invite Link

1. User A is already a participant in a member-only conversation that is not a canonical one-to-one direct message.
2. The caller sends `POST /conversations/{conversation_id}/invites`.
3. The server validates the conversation, creates an invite token with the requested policy, and returns an invite token or invite URL.
4. User A may distribute that token or URL through any out-of-band channel.

### Accept A Conversation Invite As User B

1. User B obtains a conversation invite token through any out-of-band channel.
2. If User B is not authenticated yet, User B first completes an auth flow.
3. The caller acting for User B sends `POST /conversation-invites/{token}/accept`.
4. The server validates the invite, checks any restriction rules, ensures any required workspace membership, and inserts User B into `conversation_participants` idempotently.
5. The server returns the target `Conversation` resource.
6. The caller may then send `GET /conversations/{conversation_id}/messages` or other conversation-scoped requests as User B.

### Create A Workspace

1. User A decides to create a workspace.
2. The caller sends `POST /workspaces` with the requested name and slug.
3. The server creates the workspace, an owner membership for User A, and a default `general` workspace-wide conversation.
4. The server returns the created workspace resource.
5. The caller may then use the returned `workspace_id` in subsequent workspace-scoped requests.

### Accept A Workspace Invite As User B

1. User B obtains a workspace invite token through any out-of-band channel.
2. If User B is not authenticated yet, User B first completes an auth flow.
3. The caller acting for User B sends `POST /workspace-invites/{token}/accept`.
4. The server validates the token and activates or creates User B's workspace membership.
5. The server returns User B's active `WorkspaceMember` projection for that workspace.
6. The caller may then send `GET /conversations?workspace_id={workspace_id}` or other workspace-scoped requests as User B.

### Create A Workspace-Wide Conversation

1. User A has an active membership in workspace `W`.
2. The caller sends `POST /conversations` with `workspace_id = W` and `access_policy = workspace`.
3. The server creates the conversation and returns it.
4. Any caller acting for a user with active membership in `W` can discover it through `GET /conversations?workspace_id=W`.

### Create A Workspace Private Conversation

1. User A has an active membership in workspace `W`.
2. The caller sends `POST /conversations` with `workspace_id = W`, `access_policy = members`, and any initial `participant_user_ids`.
3. The server validates that every listed user is an active member of `W`, implicitly includes User A, creates the conversation, and returns it.
4. The caller may later add more workspace members through `POST /conversations/{conversation_id}/participants`.

### Create A Global Named Conversation

1. User A decides to create a global authenticated conversation.
2. The caller sends `POST /conversations` with `workspace_id = null` and `access_policy = authenticated`.
3. The server creates the conversation and returns it.
4. Any authenticated caller can discover it through `GET /conversations`.

### Read A Workspace-Wide Conversation

1. The caller acting for User A sends `GET /conversations?workspace_id={workspace_id}`.
2. The server returns workspace-wide conversations visible to User A in that workspace.
3. The caller sends `GET /conversations/{conversation_id}/messages` for one of the returned conversations.
4. The server returns messages if User A still has access.
5. If the caller tracks read cursors, it may send `PUT /conversations/{conversation_id}/read-state`.

### Read A Workspace Private Conversation

1. The caller acting for User A sends `GET /conversations?workspace_id={workspace_id}`.
2. The server returns workspace-wide conversations plus member-only workspace conversations where User A is a participant.
3. The caller sends `GET /conversations/{conversation_id}/messages` for one of the returned member-only conversations.
4. The server returns messages only if User A still has both active workspace membership and explicit conversation participation.

### Send A Message

1. The caller acting for User A sends `POST /conversations/{conversation_id}/messages`.
2. The server derives `author_user_id` from auth.
3. The server applies the relevant conversation access rules for User A.
4. If authorized, the server stores the message and returns it.

### Consume Events

1. A caller acting for User A or another authorized consumer stores the last `next_cursor` from `/events`.
2. The caller sends `GET /events` with that cursor.
3. The server returns only external events visible to that caller after that point.
4. The consumer updates its checkpoint and processes each event idempotently.

This is the canonical pull-based integration flow.

### Receive Webhooks

1. A caller creates an `event_subscription`.
2. The server stores the destination and encrypted secret.
3. When matching external events are projected, webhook workers enqueue deliveries.
4. The remote consumer verifies the signature and handles retries idempotently.

## Identity Rendering Rules

Render participants from `users` and `user_profiles` in both global and workspace conversations.

## Explicit Exclusions

The following patterns are intentionally not part of this design:

- a persistent `accounts` table as a second canonical identity
- a persistent `workspace_users` table as a second canonical identity
- separate persisted `dm`, `group_dm`, `private_channel`, and `public_channel` conversation kinds
- separate persisted `channel` and `direct_conversation` root kinds
- workspace-owned direct messages
- user ids hidden behind workspace-local member ids in message APIs
- server-side selected workspace on the session
- creating guest workspace membership as a side effect of global conversation creation

## Acceptance Criteria

The design is correct when all of the following are true:

1. The same person or bot has one canonical `user_id` everywhere.
2. The API can create or fetch a one-to-one direct message by user pair.
3. One-to-one direct messages, private member-only conversations, workspace conversations, and global named conversations are all stored in one `conversations` table.
4. Conversation scope is inferred from `workspace_id`.
5. Conversation visibility is determined by `access_policy`.
6. Workspace-wide conversations are visible to all active workspace members.
7. Workspace private conversations require both workspace membership and explicit conversation participation.
8. Message authorship is always stored as `author_user_id`.
9. Resolving a direct message from a workspace-scoped user lookup still results in a global conversation.
10. The system never needs a workspace-local member id to authorize or write a global member-only conversation message.
11. Every successful state mutation appends immutable `internal_events` rows in the same transaction.
12. `/events` reads from projected `external_events`, not directly from primary tables or the raw internal log.
13. Event feeds and webhook deliveries can be rebuilt from `internal_events` plus projector checkpoints.
14. Email login, Google OAuth, and GitHub OAuth all resolve to the same canonical `user_id` model.
15. A non-DM member-only conversation can be created with only the caller as its first participant, then shared by invite.
16. A member-only conversation invite may be open-link or restricted to specific users or emails.
