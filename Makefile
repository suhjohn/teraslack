SERVER_DIR := server
RAILWAY ?= railway
RAILWAY_ENV ?=
RAILWAY_UP_FLAGS ?=
RAILWAY_CHAIN_UP_FLAGS ?= --ci
ENV_FILE ?= .env
COMPOSE := docker compose --env-file $(ENV_FILE)
COMPOSE_DEV := $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml

.PHONY: run build test lint migrate-up migrate-down docker-up docker-down integration_test openapi-generate openapi-check permissions-generate permissions-check \
	dev dev-down dev-reset dev-logs railway-status railway-deploy railway-ensure-service deploy-frontend deploy-server deploy-external-event-projector \
	deploy-webhook-producer deploy-webhook-worker deploy-indexer deploy-mcp-server deploy-core

run build test lint migrate-up migrate-down openapi-generate openapi-check permissions-generate permissions-check:
	$(MAKE) -C $(SERVER_DIR) $@

dev:
	$(COMPOSE_DEV) up --build --watch

dev-down:
	$(COMPOSE_DEV) down

dev-reset:
	$(COMPOSE_DEV) down -v --remove-orphans

dev-logs:
	$(COMPOSE_DEV) logs -f --tail=200

docker-up:
	$(COMPOSE) -f docker-compose.yml up --build -d

docker-down:
	$(COMPOSE) -f docker-compose.yml down

integration_test:
	./integration_test

railway-status:
	$(RAILWAY) status

define railway_role_for_service
$(strip \
$(if $(filter server,$(1)),server, \
$(if $(filter external-event-projector,$(1)),external-event-projector, \
$(if $(filter webhook-producer,$(1)),webhook-producer, \
$(if $(filter webhook-worker,$(1)),webhook-worker, \
$(if $(filter indexer,$(1)),indexer, \
$(if $(filter mcp-server,$(1)),mcp-server,)))))))
endef

railway-ensure-service:
	@if [ -z "$(SERVICE)" ]; then \
		echo "SERVICE is required. Example: make railway-ensure-service SERVICE=mcp-server"; \
		exit 1; \
	fi
	@set -eu; \
	if $(RAILWAY) status --json | grep -Fq "\"name\": \"$(SERVICE)\""; then \
		echo "Railway service $(SERVICE) already exists"; \
	else \
		echo "Creating Railway service $(SERVICE)"; \
		if [ -n "$(RAILWAY_SERVICE_VARS)" ]; then \
			$(RAILWAY) add --service "$(SERVICE)" --variables "$(RAILWAY_SERVICE_VARS)"; \
		else \
			$(RAILWAY) add --service "$(SERVICE)"; \
		fi; \
	fi

railway-deploy:
	@if [ -z "$(SERVICE)" ]; then \
		echo "SERVICE is required. Example: make railway-deploy SERVICE=server"; \
		exit 1; \
	fi
	@set -eu; \
	case "$(SERVICE)" in \
		frontend) path="frontend" ;; \
		server|external-event-projector|webhook-producer|webhook-worker|indexer|mcp-server) path="server" ;; \
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
	$(MAKE) railway-ensure-service SERVICE=server RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,server)"
	$(MAKE) railway-deploy SERVICE=server

deploy-external-event-projector:
	$(MAKE) railway-ensure-service SERVICE=external-event-projector RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,external-event-projector)"
	$(MAKE) railway-deploy SERVICE=external-event-projector

deploy-webhook-producer:
	$(MAKE) railway-ensure-service SERVICE=webhook-producer RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,webhook-producer)"
	$(MAKE) railway-deploy SERVICE=webhook-producer

deploy-webhook-worker:
	$(MAKE) railway-ensure-service SERVICE=webhook-worker RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,webhook-worker)"
	$(MAKE) railway-deploy SERVICE=webhook-worker

deploy-indexer:
	$(MAKE) railway-ensure-service SERVICE=indexer RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,indexer)"
	$(MAKE) railway-deploy SERVICE=indexer

deploy-mcp-server:
	$(MAKE) railway-ensure-service SERVICE=mcp-server RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,mcp-server)"
	$(MAKE) railway-deploy SERVICE=mcp-server

deploy-core:
	@set -eu; \
	FLAGS="$(if $(RAILWAY_UP_FLAGS),$(RAILWAY_UP_FLAGS),$(RAILWAY_CHAIN_UP_FLAGS))"; \
	status=0; \
	$(MAKE) railway-deploy SERVICE=frontend RAILWAY_UP_FLAGS="$$FLAGS" & pid_frontend=$$!; \
	$(MAKE) railway-deploy SERVICE=server RAILWAY_UP_FLAGS="$$FLAGS" & pid_server=$$!; \
	$(MAKE) railway-deploy SERVICE=external-event-projector RAILWAY_UP_FLAGS="$$FLAGS" & pid_projector=$$!; \
	$(MAKE) railway-deploy SERVICE=webhook-producer RAILWAY_UP_FLAGS="$$FLAGS" & pid_webhook_producer=$$!; \
	$(MAKE) railway-deploy SERVICE=webhook-worker RAILWAY_UP_FLAGS="$$FLAGS" & pid_webhook_worker=$$!; \
	$(MAKE) railway-deploy SERVICE=indexer RAILWAY_UP_FLAGS="$$FLAGS" & pid_indexer=$$!; \
	$(MAKE) railway-deploy SERVICE=mcp-server RAILWAY_UP_FLAGS="$$FLAGS" & pid_mcp=$$!; \
	for pid in $$pid_frontend $$pid_server $$pid_projector $$pid_webhook_producer $$pid_webhook_worker $$pid_indexer $$pid_mcp; do \
		if ! wait $$pid; then status=1; fi; \
	done; \
	exit $$status
