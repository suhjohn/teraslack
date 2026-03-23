.PHONY: run build test lint migrate-up migrate-down docker-up docker-down integration_test openapi-generate openapi-check

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server
	go build -o bin/external-event-projector ./cmd/external-event-projector
	go build -o bin/webhook-producer ./cmd/webhook-producer
	go build -o bin/webhook-worker ./cmd/webhook-worker
	go build -o bin/indexer ./cmd/indexer

test:
	go test ./... -race -count=1

openapi-generate:
	go generate ./internal/api

openapi-check:
	go generate ./internal/api
	git diff --exit-code -- api/openapi.yaml internal/api/openapi.gen.go

lint:
	golangci-lint run ./...

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

integration_test:
	./integration_test

migrate-up:
	go run ./cmd/server migrate-up

migrate-down:
	go run ./cmd/server migrate-down
