# Railway Deployment

This repo is a multi-process deployment, not a single web process.

## Railway topology

Create one Railway project, then add multiple services from the same GitHub repo.

- `frontend`
  - root directory: `frontend`
  - builder: Railpack with explicit commands, or Nixpacks if you want to use `frontend/nixpacks.toml`
  - build command if using Railpack: `bun install --frozen-lockfile && bun run build`
  - start command if using Railpack: `bun run start`
- `server`
  - root directory: `server`
  - builder: Dockerfile
  - env: `APP_ROLE=server`
- `external-event-projector`
  - root directory: `server`
  - builder: Dockerfile
  - env: `APP_ROLE=external-event-projector`

Optional workers:

- `webhook-producer`
  - root directory: `server`
  - builder: Dockerfile
  - env: `APP_ROLE=webhook-producer`
- `webhook-worker`
  - root directory: `server`
  - builder: Dockerfile
  - env: `APP_ROLE=webhook-worker`
- `indexer`
  - root directory: `server`
  - builder: Dockerfile
  - env: `APP_ROLE=indexer`

## Recommended Railway services

Minimum useful deployment:

- `frontend`
- `server`
- `external-event-projector`

Add these if you want the corresponding features:

- `webhook-producer` and `webhook-worker` for webhook delivery
- `indexer` for Turbopuffer-backed search indexing

## Start commands

Deploy every Go service from the `server/` directory using `server/Dockerfile`, and set `APP_ROLE` per service.

- `server`: `APP_ROLE=server`
- `external-event-projector`: `APP_ROLE=external-event-projector`
- `webhook-producer`: `APP_ROLE=webhook-producer`
- `webhook-worker`: `APP_ROLE=webhook-worker`
- `indexer`: `APP_ROLE=indexer`

Deploy `frontend` from the `frontend/` directory. It is a separate TanStack Start app and uses its own `package.json`.

If Railway's default builder does not correctly detect the Bun runtime for `frontend`, set these explicitly in service settings:

- build command: `bun install --frozen-lockfile && bun run build`
- start command: `bun run start`

If you prefer, you can switch the `frontend` service to Nixpacks and reuse `frontend/nixpacks.toml`.

## Railway setup steps

1. Create a Railway project and connect this GitHub repo.
2. Add the `frontend` service with root directory `frontend`.
3. Add the `server` service with root directory `server`, then set `APP_ROLE=server`.
4. Add `external-event-projector` with the same root directory `server`, then set `APP_ROLE=external-event-projector`.
5. Add `webhook-producer`, `webhook-worker`, and `indexer` only if you need those features.
6. Provision or attach Postgres, then copy its connection strings into the shared env vars.
7. Generate public domains for `frontend` and `server`.
8. Set `BASE_URL` to the public HTTPS URL of `server`.
9. Set `FRONTEND_URL` and `VITE_API_BASE_URL` to the public HTTPS URLs of `frontend` and `server`.
10. Set `VITE_TEAM_ID` on the `frontend` service to the workspace ID you want the login page to target.
11. Redeploy all services after the env vars are in place.

## Make targets

The repo root `Makefile` includes Railway deploy helpers:

- `make railway-status`
- `make deploy-frontend`
- `make deploy-server`
- `make deploy-external-event-projector`
- `make deploy-webhook-producer`
- `make deploy-webhook-worker`
- `make deploy-indexer`
- `make deploy-core`

For a generic target, use:

- `make railway-deploy SERVICE=server`

Optional overrides:

- `RAILWAY_ENV=production` to target a non-linked Railway environment
- `RAILWAY_UP_FLAGS=--ci` to stream build logs and exit

## Healthcheck

For the `server` service, use:

- path: `/healthz`

For the `frontend` service, use:

- path: `/`

## Shared env

Use [`.env.railway.example`](/Users/johnsuh/teraslack/.env.railway.example) as the template. Fill the values in Railway, not in git.

Important notes:

- `DATABASE_URL` should be the pooled app connection for normal queries.
- `MIGRATION_DATABASE_URL` should be the direct Postgres connection for startup migrations.
- `BASE_URL` must be the public HTTPS URL of the `server` service.
- `FRONTEND_URL` must be the public HTTPS URL of the `frontend` service.
- `ENCRYPTION_KEY` is required by the API server and webhook worker.
- `AUTH_STATE_SECRET` is only needed if you enable OAuth login flows.
- File uploads, webhook queues, and indexing queues all rely on S3-compatible storage.
- `VITE_API_BASE_URL` should point the frontend at the API, for example `https://api.teraslack.ai`.
- `VITE_TEAM_ID` should be the target workspace ID for the frontend login page. Without it, `/login` cannot start OAuth.

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
- The frontend reads `VITE_API_BASE_URL`, so if that variable is missing it will try `http://localhost:8080` and fail in production.
- `server` already has a multi-binary Dockerfile plus `APP_ROLE` switcher, so no code changes are required to deploy the backend services.
- `frontend` already has working Bun build and start scripts. It also includes `frontend/nixpacks.toml` if you choose to deploy it with Nixpacks instead of Railway's default builder.
