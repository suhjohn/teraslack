# Slack-Like Roles And Access Control Spec

## Status

- Proposed target-state specification
- Target system: Teraslack
- Scope: workspace account types, delegated admin roles, resource visibility, API enforcement

## Why

Teraslack currently relies on:

- workspace isolation by `workspace_id`
- a small set of API-key scopes
- a few user booleans such as `is_owner`, `is_admin`, and `is_restricted`

That is too flat for Slack-like access control. It does not model:

- an explicit top-level workspace custodian
- delegated admin roles
- per-resource visibility inheritance
- policy-based authorization for human sessions

This spec defines a full access-control model that is Slack-inspired but tailored to Teraslack's current resource model.

## Goals

- Support Slack-like account types for human users.
- Keep existing `principal_type` for `human`, `agent`, and `system`.
- Add delegated admin powers without making everyone an admin.
- Enforce the same authorization model for session auth and API keys.
- Make conversation, message, file, and event visibility consistent.
- Keep authorization decisions explainable and auditable.

## Core Engineering Performance Constraints

These are hard design constraints, not implementation suggestions.

### Event Visibility

- The external event system must not perform per-user or per-membership event fanout on write.
- Emitting one resource event must remain `O(1)` with respect to:
  - workspace user count
  - conversation member count
  - total number of conversations in the workspace
- A message posted in a channel with 1,000,000 members must not create 1,000,000 event-delivery rows.
- A workspace with 1,000,000 channels or DMs must not require event-write amplification proportional to workspace conversation count.
- Visibility for `/events` must be evaluated by indexed query-time filtering over:
  - resource-level feed tables
  - conversation membership tables
  - explicit external sharing grants
  - capability scopes

### Authorization Evaluation

- Authorization checks must scale with the number of directly relevant resources, not total workspace population.
- No request path may scan all members of a large conversation to answer whether a caller may read one event, message, or file.
- No request path may scan all conversations in a large workspace to answer whether a caller may access one conversation-scoped resource.

### External Shared Agents

- External shared-agent access must be represented as explicit resource grants, not materialized per-event recipient copies.
- Granting an external agent access to a channel must create at most:
  - one access-grant row
  - one conversation-assignment row per shared conversation
- Event reads for external shared agents must scale with the number of shared conversations for that agent, not with total workspace channels or users.

### Conversation Membership

- Membership checks for private channels, IMs, and MPIMs must be index-backed lookups.
- A conversation with 1,000,000 members must still support membership authorization checks without linear scans.

### File Visibility

- File visibility must be derived from uploader ownership and shared-conversation visibility.
- File-access checks must not expand file visibility into per-user ACL rows for every channel member.

### Search And Event Stream

- Search and `/events` must use the same visibility model.
- Neither system may precompute per-user copies of message, file, or event visibility for the entire workspace.

### Capacity Targets

The authorization and event design must remain operationally viable for at least these shapes:

- 1,000,000 users in a single workspace
- 1,000,000 conversations in a single workspace
- 1,000,000 members in a single conversation
- 1,000,000 DMs or channels overall

Meeting these targets requires query-time, index-backed visibility checks and forbids write-time recipient fanout.

### Complexity And SLO Requirements

- Event emission for a single logical resource event must be `O(1)` in:
  - workspace user count
  - conversation member count
  - workspace conversation count
- Event write amplification targets:
  - conversation-scoped event: at most 2 durable writes in the visibility path
    - one `external_events` row
    - one resource-feed row
  - file-scoped event: at most 2 durable writes in the visibility path
  - user- and workspace-scoped event: at most 2 durable writes in the visibility path
- Granting external shared access to one agent for one conversation must be `O(1)`.
- Granting one agent access to `N` conversations may be `O(N)` in number of granted conversations only.
- No event or auth path may be `O(workspace_users)`, `O(workspace_conversations)`, or `O(conversation_members)` for a single read/write decision.

Target p95 latency budgets under warm-cache, indexed-query conditions:

- private conversation membership authorization check: <= 20 ms
- message read authorization check: <= 25 ms
- file download authorization check: <= 30 ms
- `/events` page fetch with `limit=100`: <= 150 ms
- `/events` page fetch with explicit `resource_type` and `resource_id`: <= 75 ms

Hard prohibitions:

- no per-user event inbox tables
- no per-user copies of conversation events
- no per-user file-visibility rows derived from channel membership
- no synchronous fanout loops over all conversation members during message post
- no synchronous fanout loops over all workspace users during channel or file events

## Non-Goals

- Full Enterprise Grid org/workspace hierarchy.
- Billing behavior identical to Slack.
- A fully generic policy language in v1.
- Fine-grained ABAC over arbitrary JSON fields.

## Reference Model

Slack's current model is more complex than Teraslack needs. This spec intentionally simplifies it to an AWS-like custody model:

- one explicit top-level workspace custodian
- admins for day-to-day privileged operations
- members for normal usage
- additive delegated roles on top of account type
- conversation visibility as the first filter for messages and files

Relevant references:

- `https://slack.com/help/articles/201314026-Permissions-by-role-in-Slack`
- `https://slack.com/help/articles/218124397-Change-a-members-role-Change-a-members-role`
- `https://slack.com/help/articles/202518103-Understand-guest-roles-in-Slack`
- `https://slack.com/help/articles/360052445454-Manage-permissions-for-channel-management-tools`
- `https://slack.com/help/articles/4409225141907-Manage-who-can-set-channel-posting-permissions`

Teraslack should keep the explicit top-custodian pattern, not Slack's full guest taxonomy.

## Canonical Identity And Sharing Model

The implementation target is:

- `account` = canonical identity across workspaces
- `workspace_membership` = normal membership in one workspace
- `external_workspace` = connection/trust record between workspaces
- `external_member` = conversation-scoped cross-workspace participant grant

Rules:

- `external_workspace` is connection metadata and bulk-revocation scope, not a permission grant by itself.
- `external_member` is the canonical cross-workspace authorization object for conversation-scoped access.
- A single account may have:
  - many `workspace_memberships`
  - zero or more `external_members`
- An external participant must never be treated as a normal workspace member unless a real `workspace_membership` exists.

## Core Concepts

Authorization is the intersection of five checks:

1. Authentication
2. Workspace membership
3. Account type
4. Additive delegated roles
5. Resource visibility and ownership

Every request must pass all applicable layers.

## Principal Model

Retain existing `principal_type`:

- `human`
- `agent`
- `system`

Introduce a new human-only `account_type`:

- `primary_admin`
- `admin`
- `member`

Rules:

- Exactly one `primary_admin` per workspace.
- `primary_admin`, `admin`, and `member` are full workspace members.
- `agent` and `system` principals do not receive human `account_type`.
- Internal agents derive permissions from ownership, explicit capabilities, and issued credentials.
- Cross-workspace agents derive permissions from explicit external sharing grants, not human guest account types.

## Agent Access Model

Agents are not modeled as human guests.

There are two supported agent modes:

- internal agent
- external shared agent

### Internal Agent

An internal agent belongs to the workspace it operates in.

Properties:

- `principal_type = agent`
- `workspace_id = host workspace`
- `owner_id` references a human sponsoring principal
- effective permissions come from:
  - platform defaults for agents
  - explicit capability grants
  - API-key scope intersection
  - resource visibility

Internal agents may be unrestricted or restricted, but the restriction model is capability- and resource-based, not human account types.

### External Shared Agent

An external shared agent does not belong to the host workspace.
Its access is created by an explicit sharing/linking grant from the host workspace.

Properties:

- `principal_type = agent`
- `home_workspace_id = external workspace`
- `workspace_id = host workspace` for authorization context
- `access_mode = external_shared`
- visibility limited to explicitly shared resources

External shared agents are the correct model for "agents from another workspace should only have access to certain channels".

They are Slack-Connect-like, not guest-like.

## External Shared Principal Access

Introduce a first-class sharing grant for cross-workspace principals.

Canonical resources:

- `workspace_external_workspaces`
- `external_members`

Target-state fields for `external_member`:

- `id`
- `conversation_id`
- `host_workspace_id`
- `external_workspace_id`
- `account_id`
- `access_mode`
- `allowed_capabilities`
- `invited_by`
- `created_at`
- `expires_at`
- `revoked_at`

Rules:

- `access_mode` values:
  - `external_shared`
  - `external_shared_readonly`
- both human and agent accounts may be represented through the same canonical `account` model
- the permission-bearing grant is always conversation-scoped
- an external participant must never be treated as a full workspace member
- no implicit directory access
- no implicit access to public channels
- access exists only through explicit sharing grants

## Human Members Versus External Agents

These concepts are intentionally different.

### Human Member

- belongs to the workspace
- has a human `account_type`
- may hold delegated roles
- can participate in normal workspace membership and directory semantics

### External Shared Agent

- does not belong to the workspace
- has no human account semantics
- must not appear as a normal workspace member
- access is based on explicit external sharing grants
- cannot inherit workspace-wide defaults

## Additive Delegated Roles

Account type answers "what kind of account is this?".
Delegated roles answer "what additional admin powers does this account have?".

Define these built-in delegated roles:

- `channels_admin`
- `roles_admin`
- `security_admin`
- `integrations_admin`
- `support_readonly`

Optional per-conversation role:

- `channel_manager`

Rules:

- `primary_admin` and `admin` can hold delegated roles, but most are redundant for admins.
- `member` may hold delegated roles.
- `channel_manager` is scoped to a single conversation, not workspace-wide.

## Effective Permission Evaluation

For every request, compute effective permissions in this order:

1. Authenticate the caller.
2. Resolve the acting principal.
3. Resolve the workspace and confirm membership.
4. Check resource visibility.
5. Check account-type baseline permissions.
6. Check additive delegated roles.
7. Check object-level constraints:
   - membership
   - creator ownership
   - message author ownership
   - file uploader ownership
   - target user not outranking acting user
8. If the request uses an API key, intersect the principal's permissions with the key's declared scopes.

API keys never expand what the principal can do.

## Visibility Rules

Visibility is the first hard gate for reads.

### Workspace

- `primary_admin`, `admin`, and `member` can see the workspace they belong to.
- External shared agents can see only minimal host-workspace metadata needed to operate on granted resources.

### Users

- Full members can view the workspace directory.
- Support/admin roles may read more profile data, subject to privacy settings in a future phase.
- External shared agents can view only users reachable from explicitly shared conversations, and only the minimum profile needed for message attribution and mention resolution.

### Conversations

- `public_channel`
  - visible to full members in the workspace
  - visible to external shared agents only if explicitly shared
- `private_channel`
  - visible only to members
  - visible to external shared agents only if explicitly shared
- `im`
  - visible only to participants
  - not shared with external agents in v1
- `mpim`
  - visible only to participants
  - not shared with external agents in v1

### Messages

- A principal may read a message only if they can see the parent conversation.
- A principal may post only if they can see the conversation and pass posting policy checks.

### Files

- A file starts private to the uploader until shared.
- A principal may view a file if:
  - they are the uploader, or
  - they can access at least one conversation the file is shared to, or
  - they are a workspace admin/primary admin with explicit elevated access
- Presigned download URLs must only be issued after this check.
- External shared agents inherit file visibility only from explicitly shared conversations, never from workspace-wide file listing rights.

### Event Subscriptions And Events

- Event subscription CRUD is admin-only by default.
- Event delivery and event-stream reads must be filtered by the same visibility rules as direct resource reads.
- External shared agents may read only events whose canonical resource is reachable through their shared conversations and granted capabilities.

## Baseline Permission Matrix

This matrix defines the default power for each account type before additive roles.

| Capability                 | Primary Admin | Admin                   | Member |
| -------------------------- | ------------- | ----------------------- | ------ |
| Read workspace resources   | yes           | yes                     | yes    |
| Create public channels     | yes           | yes                     | yes    |
| Create private channels    | yes           | yes                     | yes    |
| Start DMs                  | yes           | yes                     | yes    |
| Start MPIMs                | yes           | yes                     | yes    |
| Post messages              | yes           | yes                     | yes    |
| Upload/share files         | yes           | yes                     | yes    |
| Create API keys for self   | yes           | yes                     | yes    |
| Create event subscriptions | yes           | yes                     | no     |
| Manage members             | yes           | yes with rank limits    | no     |
| Manage roles               | yes           | no unless `roles_admin` | no     |
| Manage workspace settings  | yes           | limited                 | no     |
| Manage billing             | yes           | no                      | no     |

External shared agents are not part of this matrix. They use explicit grant-based capabilities only.

## Rank Rules

Administrative mutations must obey rank ordering.

Rank order:

1. `primary_admin`
2. `admin`
3. `member`

Rules:

- No one can mutate a user with a higher rank than themselves.
- Only `primary_admin` can transfer or assign `primary_admin`.
- `primary_admin` can promote/demote admins and members.
- Admins can manage members but cannot create or demote `primary_admin`.
- `roles_admin` can assign delegated roles only to principals they are otherwise allowed to manage.

## Delegated Role Permissions

### channels_admin

- archive/unarchive public channels
- rename channels
- convert channel posting policies
- manage channel managers
- may manage private channels only if also a member, or if future compliance mode is enabled

### roles_admin

- assign/remove delegated roles
- assign/remove account types except `primary_admin`
- view authorization audit logs

### security_admin

- revoke sessions
- deactivate accounts
- view auth and access logs
- require MFA in a future phase

### integrations_admin

- create/update/delete event subscriptions
- create/update/revoke API keys for service principals
- approve future app/integration installs

### support_readonly

- read workspace metadata, membership metadata, audit metadata
- no write privileges

### channel_manager

- scoped to one conversation
- rename channel
- set topic and purpose
- invite/remove members subject to workspace policy
- cannot archive channel unless separately granted

Delegated roles are not assignable to external shared agents in v1.

## Ownership Rules

Some writes require object ownership even when the actor can see the object.

### Messages

- author may edit and delete own messages
- `admin` and `primary_admin` may delete any message
- future delegated capability: `messages_moderate`

### Files

- uploader may delete own files
- `admin` and `primary_admin` may delete any file
- channel-level visibility still applies to reads

### API Keys

- principals may manage their own keys
- admins may manage keys owned by manageable principals
- `integrations_admin` may manage service-principal keys
- external shared agents may hold only keys scoped to their sharing grant

### Event Subscriptions

- creator may view their own subscription metadata
- only `admin`, `primary_admin`, or `integrations_admin` may mutate them by default

### External Shared Agents

- no object ownership implies workspace membership
- all writes require both:
  - explicit capability grant
  - explicit shared-resource visibility
- no ability to mutate workspace membership, roles, or settings

## Posting Policies

Introduce per-conversation posting policy:

- `everyone`
- `admins_only`
- `members_with_permission`
- `custom`

Custom policy may reference:

- account types
- delegated roles
- explicit user IDs

This is the Slack-inspired equivalent of restricted posting permissions.

## Capability Catalog

These become the canonical permission names for both human authz and API-key scopes.

### Workspace

- `workspace.read`
- `workspace.update`
- `workspace.preferences.read`
- `workspace.preferences.write`
- `workspace.logs.read`
- `workspace.billing.read`
- `workspace.external_teams.read`
- `workspace.external_teams.write`

### Users

- `users.read`
- `users.create`
- `users.update`
- `users.deactivate`
- `users.roles.write`

### Conversations

- `conversations.read`
- `conversations.create.public`
- `conversations.create.private`
- `conversations.update`
- `conversations.archive`
- `conversations.members.read`
- `conversations.members.write`
- `conversations.managers.write`
- `conversations.posting_policy.write`

### Messages

- `messages.read`
- `messages.write`
- `messages.write.thread`
- `messages.update.own`
- `messages.delete.own`
- `messages.delete.any`
- `messages.reactions.write`

### Files

- `files.read`
- `files.write`
- `files.share`
- `files.delete.own`
- `files.delete.any`

### Integrations

- `event_subscriptions.read`
- `event_subscriptions.write`
- `api_keys.read`
- `api_keys.write`
- `api_keys.rotate`
- `api_keys.revoke`

### Events And Search

- `events.read`
- `search.use`

## Role To Capability Mapping

### primary_admin

- all capabilities

### admin

- all operational capabilities except:
  - `workspace.billing.read`
  - `users.roles.write` for `primary_admin`
  - `workspace.update` for critical workspace identity fields if future policy disables it

### member

- `workspace.read`
- `users.read`
- `conversations.read`
- `conversations.create.public`
- `conversations.create.private`
- `conversations.members.read`
- `messages.read`
- `messages.write`
- `messages.write.thread`
- `messages.update.own`
- `messages.delete.own`
- `messages.reactions.write`
- `files.read`
- `files.write`
- `files.share`
- `files.delete.own`
- `search.use`
- optionally `api_keys.write` and `api_keys.rotate` for self-issued keys only

## API Behavior Changes

### User Schema

User model:

- add `account_type`
- add `delegated_roles`

Legacy authorization booleans are removed:

- `is_owner`
- `is_admin`
- `is_restricted`

Agent principals do not use `account_type`.
Their restrictions are modeled by explicit capabilities and resource grants.

### Existing Endpoints

`PATCH /users/{id}` gains:

- `account_type`
- `delegated_roles`

Authorization:

- only manageable by `primary_admin`, `admin`, or `roles_admin` subject to rank rules

### New Endpoints

- `GET /roles`
- `GET /roles/{id}`
- `GET /users/{id}/roles`
- `PUT /users/{id}/roles`
- `GET /conversations/{id}/managers`
- `PUT /conversations/{id}/managers`
- `GET /conversations/{id}/posting-policy`
- `PUT /conversations/{id}/posting-policy`
- `GET /workspaces/{id}/external-workspaces`
- `POST /workspaces/{id}/external-workspaces`
- `DELETE /workspaces/{id}/external-workspaces/{external_workspace_id}`
- `GET /conversations/{id}/external-members`
- `POST /conversations/{id}/external-members`
- `PATCH /conversations/{id}/external-members/{external_member_id}`
- `DELETE /conversations/{id}/external-members/{external_member_id}`

### Event Stream

Add external event types:

- `user.role.changed`
- `conversation.manager.added`
- `conversation.manager.removed`
- `conversation.posting_policy.updated`

## Data Model Changes

### users table

- `users` is now a compatibility/materialization table, not the canonical membership source.
- Workspace-scoped access rank comes from `workspace_memberships.account_type`.
- A `users` row may be materialized lazily from `accounts + workspace_memberships` when a legacy `user_id` is still required by old resources.
- remove legacy columns:
- `is_admin`
- `is_owner`
- `is_restricted`

### external_members

- `id`
- `conversation_id`
- `host_workspace_id`
- `external_workspace_id`
- `account_id`
- `access_mode`
- `allowed_capabilities jsonb`
- `invited_by`
- `created_at`
- `expires_at`
- `revoked_at`

Unique:

- one active row per `(conversation_id, account_id)`

### user_role_assignments

- `id`
- `workspace_id`
- `user_id`
- `role_key`
- `assigned_by`
- `created_at`

Unique:

- `(workspace_id, user_id, role_key)`

### conversation_manager_assignments

- `conversation_id`
- `user_id`
- `assigned_by`
- `created_at`

### conversation_posting_policies

- `conversation_id`
- `policy_type`
- `policy_json`
- `updated_by`
- `updated_at`

### authorization_audit_log

- `id`
- `workspace_id`
- `actor_id`
- `target_type`
- `target_id`
- `action`
- `decision`
- `reason`
- `metadata jsonb`
- `created_at`

## Service-Layer Refactor

Introduce an `Authorizer` component responsible for:

- loading principal access state
- evaluating capabilities
- evaluating visibility
- evaluating rank and ownership
- producing deny reasons

Suggested core API:

```go
type Authorizer interface {
    Can(ctx context.Context, action Action, resource ResourceRef) error
    FilterVisibleConversations(ctx context.Context, conversations []domain.Conversation) []domain.Conversation
    FilterVisibleUsers(ctx context.Context, users []domain.User) []domain.User
    FilterVisibleFiles(ctx context.Context, files []domain.File) []domain.File
}
```

Services should stop using direct boolean checks and ad hoc `requirePermission()` logic.

## API Key Model

API keys remain scoped credentials, but change semantics to:

- effective key permission = intersection of:
  - principal effective permissions
  - key-declared scopes

Examples:

- a member cannot mint a key with admin powers
- an `integrations_admin` service key may get `event_subscriptions.write`
- an external shared agent key cannot exceed its sharing grant or shared-resource bindings

`on_behalf_of` rules:

- only `primary_admin`, `admin`, or `integrations_admin` may issue delegated keys
- creator must be allowed to act for both principal and `on_behalf_of` target

## Enforcement Requirements By Resource

### `/users`

- `/users` is a compatibility view over membership/account identity.
- list filtered by directory visibility
- get filtered by directory visibility
- create restricted to `primary_admin` or `admin`
- patch restricted by rank rules and ownership of role-management powers
- external shared agents cannot list the full workspace directory

### `/conversations`

- list filtered by visible conversations
- get requires visibility
- create depends on account type and policy
- update/archive requires creator, channel manager, or admin path depending on action
- membership changes require conversation visibility plus policy
- external shared agents can list/get only explicitly shared conversations
- external shared agents cannot create, archive, or manage membership in v1

### `/messages`

- all reads require parent conversation visibility
- post requires conversation visibility plus posting policy
- update/delete own only unless actor has moderation permission
- external shared agents may read/post only in explicitly shared conversations and only if granted the needed capabilities

### `/files`

- list filtered to visible files
- get/download requires file visibility
- share requires uploader or privileged actor
- external shared agents inherit visibility only from explicitly shared conversations
- external shared agents cannot perform standalone workspace-wide file listing unless every returned file is reachable from a shared conversation

### `/event-subscriptions`

- read/write require `event_subscriptions.read` or `event_subscriptions.write`
- default assignment: `primary_admin`, `admin`, and `integrations_admin`
- external shared agents cannot create or manage event subscriptions in v1 unless explicitly enabled by future policy

### `/api-keys`

- list/get limited to self-owned keys unless privileged
- write paths require `api_keys.write`
- external shared agents may manage only their own keys, within grant bounds

### `/events`

- results filtered by resource visibility and key scope
- `workspace_id` may target a shared host workspace when the caller has `external_member` visibility there
- external shared principals receive only conversation/file events reachable from explicitly shared conversations
- workspace and user events remain restricted to real workspace members

### `/search`

- only indexes visible content
- query results filtered by the same authorizer used for direct reads
- external shared principals search only over content in explicitly shared conversations and files

## Audit And Observability

Every authorization deny and every privileged role mutation should emit:

- internal audit event
- structured log
- optional external event for admin-visible changes

Minimum audited actions:

- account type changes
- delegated role changes
- posting policy changes
- session revocations
- API key creation/rotation/revocation

## Acceptance Criteria

- Human session auth and API-key auth use the same capability engine.
- External shared agents cannot access resources outside explicitly shared channels.
- Private channel, IM, and MPIM reads are membership-gated everywhere.
- File downloads are visibility-gated everywhere.
- `primary_admin` and `admin` cannot mutate principals above their rank.
- Event stream and search results never reveal inaccessible resources.
- Event subscription secrets and API key secrets remain one-time disclosure only.

## Follow-Up TODOs

- Add explicit transfer workflow guarantees for the single `primary_admin`.
- Expand authorization audit coverage tests for privileged mutations.
- Tighten API and MCP coverage around opaque event cursors and long-polling consumers.
- Update domain types in [user.go](/Users/johnsuh/teraslack/internal/domain/user.go), [workspace.go](/Users/johnsuh/teraslack/internal/domain/workspace.go), [conversation.go](/Users/johnsuh/teraslack/internal/domain/conversation.go), [file.go](/Users/johnsuh/teraslack/internal/domain/file.go), [event.go](/Users/johnsuh/teraslack/internal/domain/event.go), and [api_key.go](/Users/johnsuh/teraslack/internal/domain/api_key.go) to reflect the target-state access model.
- Replace the current `requirePermission()`-only approach in [authz.go](/Users/johnsuh/teraslack/internal/service/authz.go) with an `Authorizer` that evaluates account type, delegated roles, resource visibility, ownership, and key scopes.
- Add context plumbing so session auth and API-key auth both carry the data required for the same authorization engine.
- Ensure API-key effective permissions are the intersection of principal permissions and declared key scopes.
- Add rank-aware mutation rules for `primary_admin`, `admin`, and `member`.
- Add explicit transfer logic for the single `primary_admin`.
- Prevent removal or downgrade of the final `primary_admin` without a replacement.
- Define the built-in delegated roles `channels_admin`, `roles_admin`, `security_admin`, `integrations_admin`, `support_readonly`, and `channel_manager` as canonical constants.
- Define the canonical capability catalog in code and make it the single source of truth for both human authorization and API-key scopes.
- Expand OpenAPI schemas and request/response types to expose `account_type`, delegated roles, external principal access, conversation managers, and posting policies.
- Add new endpoints for roles, external principal access, conversation managers, and posting policies.
- Update router registration and generated OpenAPI bindings for all new endpoints.
- Update `GET /auth/me` to expose the caller’s effective role/account metadata if needed by clients.
- Enforce directory visibility rules on `/users` list and get paths.
- Enforce rank-aware authorization on `/users` create and patch paths.
- Enforce `primary_admin` and delegated-role constraints on workspace settings, logs, billing, and external-workspace endpoints.
- Enforce conversation visibility through the authorizer on all conversation read paths.
- Enforce conversation creation policy through `account_type` and capabilities.
- Enforce conversation membership mutation policy through rank, delegated roles, and channel manager rules.
- Add channel manager CRUD and enforcement.
- Add conversation posting policy CRUD and enforcement.
- Enforce posting policy on message post and reply operations.
- Enforce author-or-moderator rules on message update and delete operations.
- Enforce visibility on reaction read and write operations.
- Enforce file visibility from uploader ownership and shared-conversation visibility.
- Gate presigned file download URL issuance on the authorizer.
- Enforce file deletion rules for uploader vs privileged operator.
- Restrict event subscription CRUD to `primary_admin`, `admin`, or `integrations_admin` unless future policy explicitly expands it.
- Enforce event-stream visibility from resource visibility plus capability scopes.
- Enforce search result visibility with the same authorizer used by direct resource reads.
- Add external shared-agent authorization rules for conversations, messages, files, events, and search.
- Ensure external shared agents never appear as full workspace members in directory or membership APIs.
- Ensure external shared agents cannot access IMs or MPIMs in v1.
- Ensure external shared agents cannot manage workspace membership, roles, settings, or event subscriptions in v1.
- Ensure external shared-agent keys cannot exceed the sharing grant or resource assignments.
- Preserve the current non-fanout event architecture in [external_event.go](/Users/johnsuh/teraslack/internal/repository/postgres/external_event.go) and reject any implementation that creates per-user event copies.
- Add indexes needed for new authorization queries, especially rank lookups, external principal access lookups, posting policy lookups, and conversation manager lookups.
- Verify all membership checks remain index-backed and do not scan large membership sets.
- Verify `/events` remains query-time filtered and does not introduce per-user materialization.
- Add audit logging for account type changes, delegated role changes, external principal grants, posting policy changes, session revocations, and key lifecycle operations.
- Add internal events and external events for role changes, conversation manager changes, posting policy changes, and external principal access changes.
- Update seed/test helpers and stubs to create users with `account_type`.
- Add service tests for rank rules, delegated roles, posting policies, external shared-agent access, and file visibility.
- Add repository tests for new lookup tables and indexes.
- Add handler tests for new endpoints and forbidden-path behavior.
- Add integration tests for `primary_admin` transfer, delegated-role assignment, external shared-agent channel grants, and `/events` visibility.
- Add performance tests covering one million users in one workspace.
- Add performance tests covering one million conversations in one workspace.
- Add performance tests covering one million members in one conversation.
- Add performance tests covering `/events` query behavior under large feed tables.
- Add performance tests covering private-channel membership checks under large membership tables.
- Add assertions in tests that no implementation introduces per-user event fanout.
- Update developer docs to describe the simplified role model and external shared-agent model.
- Update API docs to describe the new authorization semantics and endpoint surface.

## Notes For This Repo

The spec is intentionally aligned with current Teraslack resources:

- workspaces
- users
- conversations
- messages
- files
- API keys
- event subscriptions
- external event stream

It is also compatible with the recent fixes already made in this branch:

- private conversation visibility enforcement for messages
- missing `conversation_reads` schema addition
- redaction of event subscription secrets from public APIs
