SERVER_DIR := server

.PHONY: run build test lint migrate-up migrate-down docker-up docker-down integration_test openapi-generate openapi-check

run build test lint migrate-up migrate-down openapi-generate openapi-check:
	$(MAKE) -C $(SERVER_DIR) $@

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

integration_test:
	./integration_test
