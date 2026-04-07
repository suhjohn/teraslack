# Search Design

## Status

- Implemented
- Date: 2026-04-06
- Scope: one `/search` API, Turbopuffer hybrid retrieval, queue-based indexing, Go embedding client

## Goal

Implement one enterprise-ready search system that:

- searches across all user-visible resources
- preserves the current access model exactly
- uses Turbopuffer for hybrid retrieval
- uses background queues for indexing
- uses a Go client for the Modal embedding service

## Decisions

- One public API: `POST /search`
- One logical search product, multiple internal Turbopuffer namespaces
- Hybrid retrieval everywhere: BM25 + vector + app-side fusion
- PostgreSQL remains the source of truth for authorization and hydration
- Turbopuffer is retrieval infrastructure, not the authority for access
- Indexing is event-driven and asynchronous
- Embeddings are fetched from `MODAL_EMBEDDING_SERVER_URL`
- The server uses the official `github.com/turbopuffer/turbopuffer-go` SDK

## Searchable Resources

Index all user-visible resources:

- messages
- conversations
- workspaces
- users
- external events

Do not index secret-bearing resources:

- API keys
- invite tokens
- webhook secrets
- auth sessions

## Access Model

Search will reuse the existing conversation/workspace visibility model.

Each indexed document will store:

- `kind`
- `canonical_id`
- `workspace_id`
- `conversation_id`
- `read_principal_ids`
- text fields for BM25
- `embedding`
- timestamps and archive state

`read_principal_ids` is the core ACL field. It contains deterministic principal IDs for:

- authenticated users
- a specific user
- a workspace
- a member-only conversation

At query time, the server resolves the caller's principal set from Postgres and filters Turbopuffer with `ContainsAny(read_principal_ids, caller_principal_ids)`.

This keeps authorization centralized, explainable, and stable under membership changes.

## Turbopuffer Layout

Use fixed shard rings, not namespace-per-workspace.

Corpora:

- `search-content-v1-{shard}` for messages and event-like text
- `search-entity-v1-{shard}` for conversations, workspaces, and users

Sharding:

- shard by canonical resource ID hash
- keep shard count fixed and versioned
- query all relevant shards behind the single `/search` API

This keeps fanout bounded and supports "search everywhere I can access" cleanly.

## Retrieval

For each relevant shard, issue one Turbopuffer multi-query:

- BM25 over weighted text fields
- ANN over the embedding field
- identical ACL and scope filters on both queries

Server-side flow:

1. authenticate caller
2. resolve caller principals from Postgres
3. embed the query with the Modal service
4. query Turbopuffer shards
5. fuse results with weighted reciprocal-rank fusion
6. hydrate top hits from Postgres
7. re-check access before returning results

If embedding is unavailable, the system falls back to BM25-only search.

## Indexing

Indexing is queue-driven and event-sourced.

Source:

- internal events
- brokered S3-backed queues

Worker behavior:

- consume search sync jobs from the queue
- claim and heartbeat jobs through the queue broker
- load current DB state
- build canonical search documents
- batch embeddings and Turbopuffer writes by namespace
- delete stale rows by `result_key` before batched upserts
- generate embeddings separately from text extraction so BM25 search remains available if embedding generation is delayed

Main job families:

- sync message
- sync conversation
- sync workspace
- sync user
- sync external event

Membership changes update principal resolution in Postgres immediately. Search documents do not need to be rewritten for ordinary membership churn because ACLs are principal-based.

## Go Clients

Add a Go embedding client modeled after [`embedding-client.ts`](/Users/johnsuh/teraslack/embedding-client.ts).

Packages:

- `server/internal/embedding/client.go`
- `github.com/turbopuffer/turbopuffer-go`

Embedding behavior:

- read `MODAL_EMBEDDING_SERVER_URL`
- read `MODAL_SERVER_API_KEY`
- `POST /embed/query`
- `POST /embed/documents`
- 30s timeout
- up to 3 attempts
- retry transient network and 5xx errors
- validate response sizes

Minimal embedding interface:

```go
type Client interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
}
```

## What Is Implemented

1. Config wiring for the embedding client and search service
2. A Go Modal embedding client
3. Search document builders for messages, conversations, workspaces, users, and external events
4. Queue jobs and an `indexer` worker for async indexing
5. One `/search` endpoint with hybrid retrieval, fusion, hydration, and auth re-check
6. A versioned namespace strategy for safe reindex and cutover

## Initial Constraints

- v1 prioritizes correctness and clarity over aggressive ranking sophistication
- access correctness is more important than indexing freshness
- one endpoint does not mean one physical namespace
- all results returned to clients are hydrated from Postgres before response
