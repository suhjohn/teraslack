# OPTIMIZATION TODOS

Implementation backlog derived from `OPTIMIZATION.md`.

This is ordered so the transactional correctness work lands before the larger
throughput and partitioning changes.

## Phase 1: Core Schema

- [x] Add a new Postgres migration under `server/internal/repository/migrations/`
      for the core state-model gaps:
      - `conversations.last_message_ts`
      - `conversations.last_activity_ts`
      - canonical DM uniqueness storage
      - exact thread participant storage
- [x] Decide where canonical DM uniqueness lives:
      - dedicated `canonical_dms` table keyed by `(team_id, user_a_id, user_b_id)`
      - or new DM-only uniqueness columns on `conversations`
- [x] Prefer a dedicated `canonical_dms` table so DM uniqueness is explicit and
      does not overload non-DM conversation rows.
- [x] Add a `thread_participants` table keyed by
      `(channel_id, thread_ts, user_id)` so `reply_users_count` can be updated
      incrementally instead of rescanning replies.
- [x] Add indexes for:
      - canonical DM lookup
      - conversation listing by membership and activity
      - thread participant uniqueness
      - conversation activity ordering
- [x] Add backfill SQL in the migration to initialize:
      - `last_message_ts`
      - `last_activity_ts`
      - thread participant rows from existing replies
- [ ] Decide how to handle existing duplicate DMs during migration:
      - fail fast and repair manually
      - or reconcile to a single canonical conversation

## Phase 2: Conversation Queries And Repository

- [x] Update `server/internal/repository/queries/conversations.sql` to support:
      - canonical DM lookup
      - canonical DM insert
      - incremental member-count increment
      - incremental member-count decrement
      - user-scoped conversation listing ordered by `last_activity_ts DESC`
- [x] Remove the current full recount path based on
      `UpdateConversationMemberCount`.
- [x] Refactor `server/internal/repository/postgres/conversation.go` so
      `Create()` uses find-or-create semantics for `im` conversations.
- [x] Keep non-DM conversation creation as plain insert logic.
- [x] Refactor `AddMember()` to:
      - insert membership row
      - increment `num_members` only when insert succeeds
- [x] Refactor `RemoveMember()` to:
      - delete membership row
      - decrement `num_members` only when delete succeeds
- [x] Preserve transaction boundaries so membership and counter updates remain
      atomic.
- [x] Update `server/internal/repository/interfaces.go` if repository methods
      need new query inputs or separate DM lookup/create entry points.

## Phase 3: Conversation Service And API Semantics

- [x] Refactor `server/internal/service/conversation.go` so DM creation returns
      the canonical conversation instead of always creating a new IM.
- [x] Ensure canonical DM creation does not emit duplicate create side effects
      when the DM already exists.
- [x] Change conversation listing semantics from team-scoped to user-scoped.
- [x] Update `server/internal/service/conversation.go` and
      `server/internal/handler/conversation.go` so:
      - private channels, IMs, and MPIMs come from membership
      - public channel visibility follows the chosen product rule
- [x] Make sure external/shared-access filtering still composes correctly with
      the new user-scoped listing query.

## Phase 4: Message Write Path

- [x] Update `server/internal/repository/queries/messages.sql` to support
      row-only message fetches separate from reaction aggregation.
- [x] Add explicit query separation:
      - `get_message_row`
      - `get_message_with_reactions`
- [x] Refactor `server/internal/repository/postgres/message.go` so write paths
      and existence checks use row-only fetches.
- [x] Refactor top-level message creation so it updates conversation summary
      state transactionally:
      - `conversations.last_message_ts`
      - `conversations.last_activity_ts`
- [x] Decide and document exact semantics for thread replies:
      - update only `last_activity_ts`
      - or update both `last_activity_ts` and `last_message_ts`
- [x] Apply that rule consistently in repository logic, service logic, and
      tests.

## Phase 5: Thread Replies

- [x] Replace `UpdateParentReplyStats` rescans with O(1) updates.
- [x] On reply insert:
      - increment `reply_count`
      - set `latest_reply`
      - insert into `thread_participants`
      - increment `reply_users_count` only if participant insert is new
- [x] Keep all reply-side updates in the same transaction as message insert.
- [x] Verify reply count excludes the thread root and counts only replies.
- [x] Verify `reply_users_count` reflects exact distinct repliers.

## Phase 6: Read State

- [x] Keep `server/internal/repository/queries/conversation_reads.sql` pointer
      based.
- [x] Keep `server/internal/service/conversation_read.go` O(1) on writes.
- [x] Add unread derivation logic based on:
      - `conversation_reads.last_read_ts`
      - `conversations.last_message_ts`
- [x] Decide whether unread boolean/counts are exposed in list responses now or
      deferred to a follow-up API change.

## Phase 7: Contracts And Generated Code

- [x] If conversation responses expose new activity fields, update:
      - `server/internal/domain/conversation.go`
      - `server/api/openapi.yaml`
      - generated OpenAPI outputs in server and frontend
- [ ] If message fetch semantics change publicly, update:
      - `server/internal/domain/message.go`
      - `server/api/openapi.yaml`
      - frontend generated API client under `frontend/src/lib/openapi/`
- [x] Regenerate committed generated files after SQL/OpenAPI changes.

## Phase 8: Tests

- [ ] Add repository tests for:
      - canonical DM uniqueness
      - incremental member counts
      - activity timestamp updates
      - exact thread counters
- [x] Add service tests for:
      - DM find-or-create behavior
      - user-scoped conversation listing
      - row-only fetch use on write paths
- [x] Extend `server/internal/eventsourcing/flow_integration_test.go` for:
      - duplicate DM prevention
      - activity-ordered conversation listing
      - thread stats correctness after multiple replies
      - read-state derived unread behavior
- [ ] Add migration/backfill verification coverage for existing data.

## Phase 9: Event And Outbox Throughput

- [x] Treat event/outbox throughput as a second implementation phase after core
      transactional correctness lands.
- [x] Decide whether to:
      - shard the existing `internal_events` stream
      - or introduce a dedicated `outbox_events` table
- [x] Update:
      - `server/internal/repository/postgres/event_store.go`
      - `server/internal/repository/queries/internal_events.sql`
      - background consumers and checkpointing
- [x] Guarantee ordering per conversation, not global ordering.
- [x] Add shard ownership and checkpointing rules for workers.

## Phase 10: Partitioning

- [ ] Treat physical partitioning as a separate migration series after the
      logical state model is corrected.
- [ ] Plan partitioning for:
      - `messages`
      - `internal_events` or `outbox_events`
      - optionally downstream event tables
- [ ] Preserve conversation-local history paging while distributing write load.

## Suggested Execution Order

- [ ] Phase 1: schema migration and indexes
- [ ] Phase 2: conversation queries and repository changes
- [ ] Phase 3: conversation service and listing semantics
- [ ] Phase 4: message row-fetch split and summary updates
- [ ] Phase 5: thread reply O(1) stats
- [ ] Phase 6: read-state derivation
- [ ] Phase 7: contracts and generated code
- [ ] Phase 8: tests
- [ ] Phase 9: outbox sharding
- [ ] Phase 10: partitioning
