APP_ROLE ?= server
OAPI_CODEGEN_VERSION ?= v2.4.1

.PHONY: run build test lint migrate-up migrate-down sqlc-generate sqlc-check openapi-generate openapi-check permissions-generate permissions-check

run:
	go run ./cmd/server

build:
	go build ./...

test:
	go test ./...

lint:
	go vet ./...

migrate-up:
	go run ./cmd/server -migrate-only

migrate-down:
	@echo "migrate-down is intentionally not implemented"

sqlc-generate:
	@test -f sqlc.yaml
	sqlc generate

sqlc-check: sqlc-generate

openapi-generate:
	@test -f api/openapi.yaml
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -config api/oapi-codegen.yaml api/openapi.yaml

openapi-check: openapi-generate

permissions-generate:
	@true

permissions-check:
	@true
