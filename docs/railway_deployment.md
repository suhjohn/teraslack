# Railway Deployment

This repo is a multi-process deployment, not a single web process.

## Recommended Railway services

Minimum useful deployment:

- `server`
- `external-event-projector`

Add these if you want the corresponding features:

- `webhook-producer` and `webhook-worker` for webhook delivery
- `indexer` for Turbopuffer-backed search indexing

## Start commands

Use the root `Dockerfile` for every Railway service and set `APP_ROLE` per service.

- `server`: `APP_ROLE=server`
- `external-event-projector`: `APP_ROLE=external-event-projector`
- `webhook-producer`: `APP_ROLE=webhook-producer`
- `webhook-worker`: `APP_ROLE=webhook-worker`
- `indexer`: `APP_ROLE=indexer`

## Healthcheck

For the `server` service, use:

- path: `/healthz`

## Shared env

See [.env.railway.example](/Users/johnsuh/teraslack/.env.railway.example).

Important notes:

- `DATABASE_URL` must be reachable from Railway and should include SSL.
- `BASE_URL` must be the public HTTPS URL of the `server` service.
- `ENCRYPTION_KEY` is required by the API server and webhook worker.
- `AUTH_STATE_SECRET` is only needed if you enable OAuth login flows.
- File uploads, webhook queues, and indexing queues all rely on S3-compatible storage.

## Storage layout

One S3-compatible bucket is enough.

Suggested key layout:

- uploads: `S3_KEY_PREFIX=uploads`
- webhook queue: `WEBHOOK_QUEUE_S3_KEY=queues/webhooks/queue.json`
- index queue: `INDEX_QUEUE_S3_KEY=queues/index/queue.json`

## PlanetScale Postgres

Use a Postgres connection string with SSL enabled.

Because the API server currently runs migrations on startup, using the direct Postgres connection is the safest default for now.

## Operational notes

- The API server runs migrations on startup.
- `external-event-projector` should always be running if you depend on `/events` or webhooks.
- `webhook-producer` and `webhook-worker` should be deployed together.
- `indexer` can be omitted entirely if search indexing is not needed.
