.PHONY: run build test lint migrate-up migrate-down openapi-generate openapi-check permissions-generate permissions-check

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server
	go build -o bin/external-event-projector ./cmd/external-event-projector
	go build -o bin/webhook-producer ./cmd/webhook-producer
	go build -o bin/webhook-worker ./cmd/webhook-worker
	go build -o bin/indexer ./cmd/indexer
	go build -o bin/teraslack-mcp-server ./cmd/teraslack-mcp-server

test:
	go test ./... -race -count=1

openapi-generate:
	go generate ./internal/api

openapi-check:
	go generate ./internal/api
	git diff --exit-code -- api/openapi.yaml internal/api/openapi.gen.go

permissions-generate:
	go generate ./internal/domain

permissions-check:
	go generate ./internal/domain
	git diff --exit-code -- ../frontend/src/lib/generated/permissions.ts

lint:
	golangci-lint run ./...

migrate-up:
	go run ./cmd/server migrate-up

migrate-down:
	go run ./cmd/server migrate-down
