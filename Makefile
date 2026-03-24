SERVER_DIR := server
RAILWAY ?= railway
RAILWAY_ENV ?=
RAILWAY_UP_FLAGS ?=

.PHONY: run build test lint migrate-up migrate-down docker-up docker-down integration_test openapi-generate openapi-check \
	railway-status railway-deploy deploy-frontend deploy-server deploy-external-event-projector \
	deploy-webhook-producer deploy-webhook-worker deploy-indexer deploy-core

run build test lint migrate-up migrate-down openapi-generate openapi-check:
	$(MAKE) -C $(SERVER_DIR) $@

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

integration_test:
	./integration_test

railway-status:
	$(RAILWAY) status

railway-deploy:
	@if [ -z "$(SERVICE)" ]; then \
		echo "SERVICE is required. Example: make railway-deploy SERVICE=server"; \
		exit 1; \
	fi
	@set -eu; \
	case "$(SERVICE)" in \
		frontend) path="frontend" ;; \
		server|external-event-projector|webhook-producer|webhook-worker|indexer) path="server" ;; \
		*) echo "unknown Railway service: $(SERVICE)" >&2; exit 1 ;; \
	esac; \
	echo "Deploying $(SERVICE) from $$path"; \
	if [ -n "$(RAILWAY_ENV)" ]; then \
		$(RAILWAY) up $(RAILWAY_UP_FLAGS) --environment "$(RAILWAY_ENV)" --service "$(SERVICE)" --path-as-root "$$path"; \
	else \
		$(RAILWAY) up $(RAILWAY_UP_FLAGS) --service "$(SERVICE)" --path-as-root "$$path"; \
	fi

deploy-frontend:
	$(MAKE) railway-deploy SERVICE=frontend

deploy-server:
	$(MAKE) railway-deploy SERVICE=server

deploy-external-event-projector:
	$(MAKE) railway-deploy SERVICE=external-event-projector

deploy-webhook-producer:
	$(MAKE) railway-deploy SERVICE=webhook-producer

deploy-webhook-worker:
	$(MAKE) railway-deploy SERVICE=webhook-worker

deploy-indexer:
	$(MAKE) railway-deploy SERVICE=indexer

deploy-core:
	$(MAKE) deploy-frontend
	$(MAKE) deploy-server
	$(MAKE) deploy-external-event-projector
