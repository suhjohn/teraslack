.PHONY: run build test lint migrate-up migrate-down docker-up docker-down

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

docker-up:
	docker compose up -d

docker-down:
	docker compose down

migrate-up:
	go run ./cmd/server migrate-up

migrate-down:
	go run ./cmd/server migrate-down
