# OPTIMIZATION

## Purpose

This document describes the backend chat system mathematically:

- what state exists today
- what transition functions exist today
- what the target state and transition functions should be
- what correctness invariants we want
- what complexity each operation has as-is and should have in the target system

The goal is strict correctness with high throughput.

The intended end-state stack is:

- Postgres as the source of truth
- Redis as an optional disposable accelerator
- S3-backed workers for durable async side effects

Not required for the core transactional system:

- ClickHouse
- Kafka

## Scope

This document focuses on:

- canonical DM creation
- conversation membership
- top-level message send
- thread replies
- read state
- conversation listing
- message fetch semantics
- event/outbox throughput
- partitioning strategy

## Notation

Let:

- `U` be the set of users
- `C` be the set of conversations
- `M` be the set of messages
- `Team` be the set of teams
- `CM subseteq C x U` be the conversation membership relation
- `R subseteq C x U` be the read-pointer relation
- `D subseteq Team x U x U x C` be the canonical DM mapping
- `TR subseteq C x U` be the thread participant relation for exact distinct repliers

For a conversation `c in C`:

- `team(c)` is the team that owns `c`
- `type(c)` is the conversation type
- `num_members(c)` is the stored member count
- `last_message_ts(c)` is the stored timestamp of the latest top-level message
- `last_activity_ts(c)` is the stored timestamp of the latest activity in the conversation

For a message `m in M`:

- `channel(m)` is the conversation containing `m`
- `user(m)` is the author
- `ts(m)` is the message timestamp
- `thread_root(m)` is the root thread timestamp if `m` is a reply, else `null`

For a thread root `t`:

- `Replies(t) = { m in M : thread_root(m) = t }`
- `Participants(t) = { user(m) : m in Replies(t) }`
- `reply_count(t) = |Replies(t)|`
- `reply_users_count(t) = |Participants(t)|`
- `latest_reply(t) = max(ts(m)) for m in Replies(t)`

For read state:

- `last_read(u, c)` is the exact read pointer for user `u` in conversation `c`

For conversation size:

- `N_c = |{ u : (c, u) in CM }|`

For thread size:

- `R_t = |Replies(t)|`
- `P_t = |Participants(t)|`

## Global Design Goal

The main throughput rule is:

> A top-level message send must be independent of channel size.

That is, for a conversation `c`, the cost of `post_message(c, u, body)` should not depend on `N_c`.

Formally, target complexity is:

```text
cost(post_message(c, u, body)) = O(1)
```

with respect to:

- number of members in the conversation
- total messages in the conversation
- total users in the workspace

## 1. Canonical DM Creation

### As-Is State

A DM is just a conversation with:

```text
type(c) = im
```

There is no canonical uniqueness relation between a user pair and a DM conversation.

### As-Is Transition Function

```text
create_dm(team, a, b):
  c := new conversation(type = im)
  CM := CM union {(c, a), (c, b)}
  num_members(c) := recomputed count
  return c
```

### As-Is Problem

Duplicate DMs are possible:

```text
exists c1 != c2 such that
  team(c1) = team(c2) = team
  type(c1) = type(c2) = im
  members(c1) = members(c2) = {a, b}
```

This means the state model does not enforce "one pair, one DM".

### Target State

Introduce a canonical DM mapping:

```text
D(team, min(a, b), max(a, b)) = c
```

with uniqueness:

```text
forall team, a, b:
  exists exactly one c such that D(team, min(a, b), max(a, b)) = c
```

### Target Transition Function

```text
create_dm(team, a, b):
  key := (team, min(a, b), max(a, b))
  if key in D:
    return D[key]
  else:
    c := new conversation(type = im)
    D[key] := c
    CM := CM union {(c, a), (c, b)}
    num_members(c) := 2
    return c
```

### Complexity

As-is:

```text
O(1)
```

but incorrect state semantics.

Target:

```text
O(1)
```

with exact uniqueness.

### Invariant

```text
forall team, a, b:
  cardinality({ c : D(team, min(a, b), max(a, b)) = c }) = 1
```

## 2. Conversation Membership

### As-Is State

Membership is stored in `CM`, but `num_members(c)` is maintained by recomputing:

```text
num_members(c) := |{ u : (c, u) in CM }|
```

on each add or remove.

### As-Is Transition Functions

```text
add_member(c, u):
  lock(c)
  CM := CM union {(c, u)}
  num_members(c) := |{ x : (c, x) in CM }|
```

```text
remove_member(c, u):
  lock(c)
  CM := CM \ {(c, u)}
  num_members(c) := |{ x : (c, x) in CM }|
```

### As-Is Complexity

For one mutation:

```text
O(N_c)
```

To build a channel from `0` to `N` members:

```text
sum(i = 1..N) O(i) = O(N^2)
```

### Target State

`CM` remains the source of truth for membership.

`num_members(c)` is maintained incrementally.

### Target Transition Functions

```text
add_member(c, u):
  if (c, u) not in CM:
    CM := CM union {(c, u)}
    num_members(c) := num_members(c) + 1
```

```text
remove_member(c, u):
  if (c, u) in CM:
    CM := CM \ {(c, u)}
    num_members(c) := num_members(c) - 1
```

### Target Complexity

Per mutation:

```text
O(1)
```

Building a channel to `N` members:

```text
O(N)
```

### Invariant

```text
num_members(c) = |{ u : (c, u) in CM }|
```

This should be maintained transactionally and periodically reconciled by a background audit job.

## 3. Top-Level Message Send

### As-Is State

A message send creates:

- a message row
- an internal event row

without fanout per recipient.

This is already directionally correct.

### As-Is Transition Function

```text
post_message(c, u, body):
  assert access(u, c)
  m := new top-level message in c
  M := M union {m}
  append internal_event(m)
  return m
```

### As-Is Complexity

```text
O(1)
```

with respect to:

- `N_c`
- total workspace users
- total messages in conversation

### Target State

Keep the same core shape, but make the conversation summary state explicit:

- `last_message_ts(c)`
- `last_activity_ts(c)`

### Target Transition Function

```text
post_message(c, u, body):
  assert access(u, c)
  m := new top-level message in c
  M := M union {m}
  last_message_ts(c) := ts(m)
  last_activity_ts(c) := ts(m)
  append outbox_event(m)
  return m
```

### Target Complexity

```text
O(1)
```

### Invariant

```text
last_message_ts(c) = max({ ts(m) : m in M and channel(m) = c and thread_root(m) = null })
```

and

```text
last_activity_ts(c) = max({ ts(m) : m in M and channel(m) = c })
```

or whatever exact conversation activity rule is chosen.

## 4. Thread Replies

### As-Is State

Thread metadata is derived by rescanning all replies on every new reply.

### As-Is Transition Function

```text
reply(t, u, body):
  m := new reply under thread root t
  M := M union {m}
  reply_count(t) := |Replies(t)|
  reply_users_count(t) := |Participants(t)|
  latest_reply(t) := ts(m)
```

### As-Is Complexity

Per reply:

```text
O(R_t)
```

For a thread with `R` replies:

```text
sum(i = 1..R) O(i) = O(R^2)
```

### Target State

Maintain:

- exact `reply_count(t)`
- exact `reply_users_count(t)`
- exact `latest_reply(t)`
- exact membership of `TR(t, u)` for distinct participants

### Target Transition Function

```text
reply(t, u, body):
  m := new reply under thread root t
  M := M union {m}
  reply_count(t) := reply_count(t) + 1
  latest_reply(t) := ts(m)
  if (t, u) not in TR:
    TR := TR union {(t, u)}
    reply_users_count(t) := reply_users_count(t) + 1
```

### Target Complexity

```text
O(1)
```

assuming a unique lookup on `(t, u)`.

### Invariants

```text
reply_count(t) = |Replies(t)|
```

```text
reply_users_count(t) = |Participants(t)|
```

```text
latest_reply(t) = max({ ts(m) : m in Replies(t) })
```

## 5. Read State

### As-Is State

Read state is pointer-based:

```text
last_read(u, c)
```

This is the correct conceptual model for strict correctness without per-message fanout.

### As-Is Transition Function

```text
mark_read(u, c, ts):
  last_read(u, c) := ts
```

### As-Is Complexity

```text
O(1)
```

### Target State

Keep the same model.

Add a cheap conversation high-water mark:

```text
last_message_ts(c)
```

### Derived Functions

Unread boolean:

```text
is_unread(u, c) = 1[last_read(u, c) < last_message_ts(c)]
```

Exact unread count if needed:

```text
unread_count(u, c) = |{ m in M : channel(m) = c and ts(m) > last_read(u, c) }|
```

### Target Complexity

Updating the pointer:

```text
O(1)
```

The design rule is:

> Never materialize per-user-per-message unread state on write.

## 6. Conversation Listing

### As-Is State

Listing is team-scoped instead of user-scoped.

Mathematically, the current shape is roughly:

```text
list_conversations(team) =
  first_k(sort({ c in C : team(c) = team }, by = id))
```

This answers a workspace question, not a user question.

### As-Is Problem

The natural API is:

```text
list_conversations(u)
```

but the current state/query model starts from all team conversations.

### Target State

Listing should be a function of:

- membership `CM`
- conversation metadata
- `last_activity_ts(c)`

### Target Transition-Free Read Function

```text
list_conversations(u) =
  top_k(
    sort(
      { c in C : (c, u) in CM },
      by = last_activity_ts(c) desc
    )
  )
```

### Target Complexity

The exact cost depends on indexing and storage layout, but it should scale with:

```text
O(conversations visible to u)
```

not:

```text
O(all conversations in the team)
```

### Invariant

For private conversations and DMs:

```text
visible(u, c) iff (c, u) in CM
```

For public channels:

the visibility rule may differ, but the API should still be user-scoped.

## 7. Message Fetch Semantics

### As-Is State

Base message fetch also loads aggregated reactions.

### As-Is Function

```text
get_message(c, ts):
  m := message(c, ts)
  rx := aggregate_reactions(c, ts)
  return (m, rx)
```

### As-Is Problem

Write paths that only need the base message row pay reaction aggregation cost.

If a message has `K_react` reaction entries, then:

```text
cost(get_message(c, ts)) = O(K_react)
```

in addition to the base point lookup.

### Target State

Split the state access functions.

### Target Functions

```text
get_message_row(c, ts) -> m
```

```text
get_message_with_reactions(c, ts) -> (m, rx)
```

### Target Complexity

```text
cost(get_message_row(c, ts)) = O(1)
```

```text
cost(get_message_with_reactions(c, ts)) = O(K_react)
```

### Invariant

Only read APIs that actually return reactions should compute reactions.

## 8. Event / Outbox Throughput

### As-Is State

The write path appends internal events.

Background processing is effectively a single ordered stream:

```text
internal_events -> projector -> external_events / side effects
```

### As-Is Throughput Model

Let:

- `lambda` = committed event arrival rate
- `mu` = consumer throughput
- `B(t)` = backlog at time `t`

Then:

```text
B'(t) = lambda - mu
```

If:

```text
lambda > mu
```

then:

```text
B(t) -> infinity
```

### Target State

Use a sharded outbox.

Let:

```text
shard(c) = hash(c) mod P
```

or some other stable partitioning function.

Each shard has an ordered worker.

### Target Throughput Model

Let worker throughput per shard be `mu_i`.

Then:

```text
B'(t) = lambda - sum(i = 1..P) mu_i
```

System stability requires:

```text
sum(i = 1..P) mu_i >= lambda
```

### Ordering Invariant

We do not need global event order.

We need:

```text
forall conversation c:
  events for c are processed in commit order
```

This is the correct ordering granularity for chat semantics.

## 9. Partitioning

### As-Is State

Large append-heavy tables behave like single logical streams.

### Target State

Partition:

- `messages`
- `outbox_events` or `internal_events`
- optionally `external_events`

### Partition Functions

Messages:

```text
partition_messages(c) = hash(c) mod P
```

Outbox:

```text
partition_outbox(key) = hash(key) mod Q
```

where `key` is typically conversation id or team id.

### Design Goal

Preserve locality for a conversation while distributing total write load.

### Target Properties

- appends are distributed across partitions
- conversation history remains efficiently pageable
- workers can own disjoint shard sets

## 10. Redis

### Role

Redis is not part of the source-of-truth state model.

It is a cache over exact Postgres state.

### Allowed Derived State

- cached conversation summaries
- cached unread summaries
- rate-limit counters
- idempotency windows

### Forbidden Responsibility

Redis must not define:

- whether a message exists
- who belongs to a conversation
- which DM is canonical
- the exact read pointer

### Invariant

If Redis is empty or wrong, the API still produces correct answers from Postgres.

## 11. S3-Backed Async Jobs

### Role

S3-backed jobs are acceptable for:

- webhooks
- indexing
- replay
- exports
- backfills

### Mathematical Property

These jobs are downstream of committed truth.

If:

```text
commit(transaction) = success
```

then eventual async side effects may happen later, but correctness is already established.

### Invariant

Async work must be idempotent:

```text
apply(job, state)
apply(job, apply(job, state)) = apply(job, state)
```

for every replayable side-effect job.

## 12. ClickHouse

### Need

ClickHouse is not required for the transactional chat system.

### Appropriate Use

It can later support:

- analytics
- audits
- event exploration
- reporting

### Invariant

The core correctness path must not depend on ClickHouse.

## 13. Required End-State Stack

The required system is:

- `Postgres`
- `S3-backed async workers`

The recommended system is:

- `Postgres`
- `Redis`
- `S3-backed async workers`

Not required:

- `ClickHouse`
- `Kafka`

## 14. Summary Table

| Operation | As-Is | As-Is Complexity | Target | Target Complexity |
| --- | --- | --- | --- | --- |
| Create DM | create new IM every time | `O(1)` but duplicates allowed | find-or-create canonical DM | `O(1)` exact |
| Add member | insert + full recount | `O(N_c)` | insert + `+1` | `O(1)` |
| Remove member | delete + full recount | `O(N_c)` | delete + `-1` | `O(1)` |
| Post top-level message | insert message + event | `O(1)` | same + activity fields + outbox | `O(1)` |
| Reply in thread | insert + rescan thread | `O(R_t)` | insert + incremental stats | `O(1)` |
| Mark read | upsert pointer | `O(1)` | same | `O(1)` |
| List conversations | team-scoped scan/order | wrong function of state | user-scoped by membership and activity | proportional to visible set |
| Get message | base row + reaction aggregation | `O(1 + K_react)` | split row-only vs decorated | `O(1)` or `O(1 + K_react)` |
| Event projection | single-stream backlog risk | bounded by one `mu` | sharded ordered workers | bounded by `sum(mu_i)` |

## 15. End-State Invariants

The target system must satisfy all of the following:

### DM uniqueness

```text
forall team, a, b:
  exists exactly one canonical DM conversation c
```

### Exact membership count

```text
forall c:
  num_members(c) = |{ u : (c, u) in CM }|
```

### Top-level send independence from channel size

```text
cost(post_message(c, u, body)) = O(1)
```

with respect to `N_c`.

### Exact thread stats

```text
forall thread root t:
  reply_count(t) = |Replies(t)|
  reply_users_count(t) = |Participants(t)|
  latest_reply(t) = max reply ts
```

### Exact read pointers

```text
forall u, c:
  last_read(u, c) is authoritative
```

### User-scoped listing

```text
list_conversations(u)
```

must be defined as a function of the conversations visible to `u`, not all conversations in the team.

### Per-conversation ordering

```text
forall conversation c:
  committed events for c are processed in order
```

### Cache dispensability

```text
if Redis = empty or stale:
  correctness remains unchanged
```

### Async dispensability

```text
if webhook/index/export workers are delayed:
  transactional chat correctness remains unchanged
```

## 16. Final Design Rule

Use Postgres to define exact state transitions.

Use Redis to accelerate derived reads.

Use S3-backed workers to process committed side effects.

Do not move transactional correctness out of Postgres.
