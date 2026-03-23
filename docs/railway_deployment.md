# Railway Deployment

This repo is a multi-process deployment, not a single web process.

## Recommended Railway services

Minimum useful deployment:

- `frontend`
- `server`
- `external-event-projector`

Add these if you want the corresponding features:

- `webhook-producer` and `webhook-worker` for webhook delivery
- `indexer` for Turbopuffer-backed search indexing

## Start commands

Use the root `Dockerfile` for every Go service and set `APP_ROLE` per service.

- `server`: `APP_ROLE=server`
- `external-event-projector`: `APP_ROLE=external-event-projector`
- `webhook-producer`: `APP_ROLE=webhook-producer`
- `webhook-worker`: `APP_ROLE=webhook-worker`
- `indexer`: `APP_ROLE=indexer`

Deploy `frontend` from the `frontend/` directory. It is a separate TanStack Start app and uses its own `package.json` and `nixpacks.toml`.

## Healthcheck

For the `server` service, use:

- path: `/healthz`

For the `frontend` service, use:

- path: `/`

## Shared env

Ask the user which .env file to use. Provide the template of all env variables we need.

Important notes:

- `DATABASE_URL` should be the pooled app connection for normal queries.
- `MIGRATION_DATABASE_URL` should be the direct Postgres connection for startup migrations.
- `BASE_URL` must be the public HTTPS URL of the `server` service.
- `FRONTEND_URL` must be the public HTTPS URL of the `frontend` service.
- `ENCRYPTION_KEY` is required by the API server and webhook worker.
- `AUTH_STATE_SECRET` is only needed if you enable OAuth login flows.
- File uploads, webhook queues, and indexing queues all rely on S3-compatible storage.
- `VITE_API_BASE_URL` should point the frontend at the API, for example `https://api.teraslack.ai`.

## Storage layout

One S3-compatible bucket is enough.

Suggested key layout:

- uploads: `S3_KEY_PREFIX=uploads`
- webhook queue: `WEBHOOK_QUEUE_S3_KEY=queues/webhooks/queue.json`
- index queue: `INDEX_QUEUE_S3_KEY=queues/index/queue.json`

## PlanetScale Postgres

Use Postgres connection strings with SSL enabled.

Recommended split:

- `DATABASE_URL`: pooled PlanetScale URL on port `6432`
- `MIGRATION_DATABASE_URL`: direct PlanetScale URL on port `5432`

## Operational notes

- The API server runs migrations on startup.
- The API server now expects `FRONTEND_URL` so CORS can allow the browser app.
- `external-event-projector` should always be running if you depend on `/events` or webhooks.
- `webhook-producer` and `webhook-worker` should be deployed together.
- `indexer` can be omitted entirely if search indexing is not needed.
