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

## Recommended Railway services

Minimum useful deployment:

- `frontend`
- `server`
- `external-event-projector`

Add these if you want the corresponding features:

- `webhook-producer` and `webhook-worker` for webhook delivery

The default GitHub Actions deploy workflow bootstraps and deploys the full service set. Use the service-specific Make targets only if you intentionally want a reduced topology.

## Start commands

Deploy every Go service from the `server/` directory using `server/Dockerfile`, and set `APP_ROLE` per service.

- `server`: `APP_ROLE=server`
- `external-event-projector`: `APP_ROLE=external-event-projector`
- `webhook-producer`: `APP_ROLE=webhook-producer`
- `webhook-worker`: `APP_ROLE=webhook-worker`

Deploy `frontend` from the `frontend/` directory. It is a separate TanStack Start app and uses its own `package.json`.

If Railway's default builder does not correctly detect the Bun runtime for `frontend`, set these explicitly in service settings:

- build command: `bun install --frozen-lockfile && bun run build`
- start command: `bun run start`

If you prefer, you can switch the `frontend` service to Nixpacks and reuse `frontend/nixpacks.toml`.

## Railway setup steps

1. Create a Railway project and connect this GitHub repo.
2. Set the GitHub Actions deploy secrets: `RAILWAY_TOKEN`, `RAILWAY_API_TOKEN`, and `RAILWAY_PROJECT_ID`.
3. Provision or attach Postgres, then copy its connection strings into the shared env vars.
4. Provision or attach S3-compatible object storage, then set the shared `S3_*` env vars plus any queue key overrides you want.
5. Generate public domains for `frontend` and `server` if your workers are not on Railway private networking.
6. Map `teraslack.ai` to `frontend` and `api.teraslack.ai` to `server`.
7. Set `BASE_URL=https://api.teraslack.ai`.
8. Set `FRONTEND_URL=https://teraslack.ai`.
9. Set `VITE_API_BASE_URL=https://api.teraslack.ai`.
10. Set `CORS_ALLOWED_ORIGINS=https://teraslack.ai,https://www.teraslack.ai`.
11. Run the `Deploy` workflow manually once, or push to `main`. The deploy targets create any missing Railway services and deploy them from the correct repo subdirectory.

## Install Flow Deployment Notes

For the one-command installer:

1. `frontend/public/install.sh` is emitted into the frontend production build for macOS and Linux and should be reachable at `https://teraslack.ai/install.sh`.
2. `frontend/public/install.ps1` is emitted into the frontend production build for Windows and should be reachable at `https://teraslack.ai/install.ps1`.
3. Both installers expect prebuilt CLI binaries on `https://downloads.teraslack.ai/teraslack/cli/...`.
4. The installers write local base URL config only; users sign in afterward with `teraslack signin email --email you@example.com`.

Release bundle workflow:

1. Run `make build-cli-release VERSION=v0.1.0`.
2. Upload `dist/cli-release/latest.json`.
3. Upload `dist/cli-release/v0.1.0/SHA256SUMS`.
4. Upload each platform tarball under:
   - `teraslack/cli/v0.1.0/darwin-arm64/teraslack.tar.gz`
   - `teraslack/cli/v0.1.0/darwin-amd64/teraslack.tar.gz`
   - `teraslack/cli/v0.1.0/linux-amd64/teraslack.tar.gz`
   - `teraslack/cli/v0.1.0/linux-arm64/teraslack.tar.gz`
   - `teraslack/cli/v0.1.0/windows-amd64/teraslack.zip`
   - `teraslack/cli/v0.1.0/windows-arm64/teraslack.zip`

Automated downloads upload:

1. Set:
   - `S3_DOWNLOADS_BUCKET=teraslack-downloads`
   - `S3_DOWNLOADS_ACCOUNT_ID=<your_storage_account_id>` or `S3_DOWNLOADS_ENDPOINT=https://<storage-endpoint>`
   - `S3_DOWNLOADS_ACCESS_KEY_ID=<downloads_access_key_id>`
   - `S3_DOWNLOADS_SECRET_ACCESS_KEY=<downloads_secret_access_key>`
   - optional: `S3_DOWNLOADS_PREFIX=teraslack/cli`
2. Run `make release-cli VERSION=v0.1.0`.
3. If the bundle is already built, run `make upload-cli-release VERSION=v0.1.0`.

## GitHub Actions

This repo includes two workflows:

1. [`deploy.yml`](/Users/johnsuh/teraslack/.github/workflows/deploy.yml)
   - triggers on push to `main`
   - can also be run manually for first-time bootstrap
   - verifies all deployed Go binaries build, runs targeted server tests, and verifies the frontend build
   - creates missing Railway services, then deploys `frontend`, `server`, `external-event-projector`, `webhook-producer`, and `webhook-worker`
2. [`release-cli.yml`](/Users/johnsuh/teraslack/.github/workflows/release-cli.yml)
   - triggers on tags like `cli-v0.1.0`
   - can also be run manually
   - builds the CLI release bundle
   - uploads artifacts to the downloads bucket
   - creates or updates a GitHub Release

GitHub Actions secrets and vars:

1. Deploy workflow:
   - secret: `RAILWAY_TOKEN` if you are using a project token
   - secret: `RAILWAY_API_TOKEN` if you are using an account or workspace token
   - secret: `RAILWAY_PROJECT_ID`
   - optional variable: `RAILWAY_ENVIRONMENT` (defaults to `production`)
2. CLI release workflow:
   - secret: `S3_DOWNLOADS_BUCKET`
   - secret: `S3_DOWNLOADS_ACCOUNT_ID` or `S3_DOWNLOADS_ENDPOINT`
   - secret: `S3_DOWNLOADS_ACCESS_KEY_ID`
   - secret: `S3_DOWNLOADS_SECRET_ACCESS_KEY`
   - optional variable: `S3_DOWNLOADS_PREFIX` (defaults to `teraslack/cli`)

## Make targets

The repo root `Makefile` includes Railway deploy helpers:

- `make railway-status`
- `make deploy-frontend`
- `make deploy-server`
- `make deploy-external-event-projector`
- `make deploy-webhook-producer`
- `make deploy-webhook-worker`
- `make deploy-core`

For a generic target, use:

- `make railway-deploy SERVICE=server`

Important:

- Service-specific deploy targets now create the Railway service first if it does not already exist in the configured project:
  `make deploy-frontend`, `make deploy-server`, `make deploy-external-event-projector`, `make deploy-webhook-producer`, and `make deploy-webhook-worker`.
- The auto-created backend services are initialized with the matching `APP_ROLE`.
- `make deploy-core` now bootstraps missing services first, then deploys the full service set in parallel: `frontend`, `server`, `external-event-projector`, `webhook-producer`, and `webhook-worker`.
- The generic `make railway-deploy SERVICE=...` target still expects the Railway service to already exist.
- The `SERVICE=...` value must exactly match the Railway service name, for example `server`.

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
- `FRONTEND_URL` must be the primary public HTTPS URL of the `frontend` service.
- `CORS_ALLOWED_ORIGINS` can be set to a comma-separated list when the browser app is reachable from more than one origin, for example `https://teraslack.ai,https://www.teraslack.ai`.
- The API server and webhook worker require a shared encryption configuration.
  Use `AWS_KMS_KEY_ID` plus `AWS_KMS_REGION` for AWS KMS, or use `ENCRYPTION_KEY`.
  If both are set, new secrets are written with KMS and `ENCRYPTION_KEY` remains available to decrypt legacy env-key ciphertext during migration.
- `AUTH_STATE_SECRET` is only needed if you enable OAuth login flows.
- File uploads and webhook queues rely on S3-compatible storage.
- `VITE_API_BASE_URL` should point the frontend at the API, for example `https://api.teraslack.ai`.
- The frontend OAuth start flow does not require a `VITE_TEAM_ID`. It forwards `workspace_id` only when that query parameter is already present in the page URL.

## Storage layout

One S3-compatible bucket is enough.

Suggested key layout:

- uploads: `S3_KEY_PREFIX=uploads`
- projector queue: `PROJECTOR_QUEUE_S3_KEY=queues/projector/queue.json`
- webhook queue: `WEBHOOK_QUEUE_S3_KEY=queues/webhooks/queue.json`

## PlanetScale Postgres

Use Postgres connection strings with SSL enabled.

Recommended split:

- `DATABASE_URL`: pooled PlanetScale URL on port `6432`
- `MIGRATION_DATABASE_URL`: direct PlanetScale URL on port `5432`

## Operational notes

- The API server runs migrations on startup.
- The API server now expects `FRONTEND_URL` so CORS can allow the browser app.
- The API server and `webhook-worker` now fail fast if neither AWS KMS nor `ENCRYPTION_KEY` is configured, because webhook secrets are always stored encrypted.
- If production serves the frontend from multiple origins, set `CORS_ALLOWED_ORIGINS` explicitly or some browsers will fail with a missing `Access-Control-Allow-Origin` header even when the API is otherwise healthy.
- `external-event-projector` should always be running if you depend on `/events` or webhooks.
- Queue state is stored directly in S3-compatible storage, and each worker process uses compare-and-set writes against its queue JSON object.
- `external-event-projector`, `webhook-producer`, and `webhook-worker` should all agree on the same queue object keys.
- `webhook-producer` and `webhook-worker` should be deployed together.
- The frontend reads `VITE_API_BASE_URL`, so if that variable is missing it will try `http://localhost:8080` and fail in production.
- `server` already has a multi-binary Dockerfile plus `APP_ROLE` switcher, so no code changes are required to deploy the backend services.
- `frontend` already has working Bun build and start scripts. It also includes `frontend/nixpacks.toml` if you choose to deploy it with Nixpacks instead of Railway's default builder.
