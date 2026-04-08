<p align="center">
  <img src="./frontend/public/favicon.svg" alt="Teraslack logo" width="112" />
</p>

<h1 align="center">Teraslack</h1>

<p align="center">
  <strong>The open-source workspace for conversations, agents, and event-driven collaboration.</strong>
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache 2.0" /></a>
  <a href="./server/go.mod"><img src="https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.24+" /></a>
  <a href="./frontend/package.json"><img src="https://img.shields.io/badge/React-19-149ECA?logo=react&logoColor=white" alt="React 19" /></a>
  <a href="./server/api/openapi.yaml"><img src="https://img.shields.io/badge/OpenAPI-source-6BA539" alt="OpenAPI source" /></a>
</p>

Teraslack brings messaging, workspaces, search, agents, API keys, and signed event delivery into one system. Human users and agents share the same core identity and conversation model, and the product is built to be scriptable from the web UI, the HTTP API, and the CLI.

Create workspaces, start direct messages, open private rooms, search across what a caller can actually access, and plug activity into external systems through event feeds and webhooks.

## Capabilities

- One conversation model: direct messages, private rooms, and workspace channels are derived views over the same underlying conversation primitive.
- Global and workspace scopes: keep global conversations and workspace collaboration without inventing a second workspace-local identity model.
- Search with access control: hybrid search spans messages, conversations, workspaces, users, and external events while preserving the existing authorization model.
- Agents and API keys: create user-owned or workspace-owned agents with rotatable credentials for automation and integrations.
- Event feeds and webhook delivery: poll event history directly or subscribe to filtered, signed webhooks for downstream automation.
- CLI-friendly workflows: provision workspaces, invite teammates, post messages, search, and manage agents from scripts and CI.

## Get Started

### Run locally

```bash
cp .env.example .env
make dev
```

This starts the full local stack with Docker Compose, including:

- frontend on `http://localhost:3201`
- API server on `http://localhost:38080`
- queue broker on `http://localhost:38081`
- PostgreSQL, MinIO, the indexer, and background workers

If you want to run the frontend outside Docker:

```bash
cd frontend
nvm use 24
bun install
bun run dev
```

Useful repo-level commands:

```bash
make dev
make dev-logs
make dev-down
make test
make lint
make integration_test
```

### Install the CLI

macOS / Linux:
`curl -fsSL https://teraslack.ai/install.sh | sh`

Windows PowerShell:
`powershell -ExecutionPolicy Bypass -c "irm https://teraslack.ai/install.ps1 | iex"`

Then sign in:
`teraslack signin email --email you@example.com`

## License

Teraslack is licensed under the Apache License, Version 2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).
