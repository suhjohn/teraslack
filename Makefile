SERVER_DIR := server
RAILWAY ?= railway
RAILWAY_PROJECT ?=
RAILWAY_ENV ?=
RAILWAY_UP_FLAGS ?=
RAILWAY_CHAIN_UP_FLAGS ?= --ci
ENV_FILE ?= .env
COMPOSE := docker compose --env-file $(ENV_FILE)
COMPOSE_DEV := $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml
SEARCH_ENV_FILE ?= .env.railway
INTEGRATION_SEARCH_RUN ?= Search
CLI_RELEASE_BUMP_TARGETS := bump-patch bump-minor bump-major

define railway_prepare_context
tmpdir=""; \
cleanup() { \
	if [ -n "$$tmpdir" ]; then \
		rm -rf "$$tmpdir"; \
	fi; \
}; \
trap cleanup EXIT; \
if [ -n "$(RAILWAY_PROJECT)" ]; then \
	tmpdir=$$(mktemp -d); \
	cd "$$tmpdir"; \
	if [ -n "$(RAILWAY_ENV)" ]; then \
		$(RAILWAY) link --project "$(RAILWAY_PROJECT)" --environment "$(RAILWAY_ENV)" >/dev/null; \
	else \
		$(RAILWAY) link --project "$(RAILWAY_PROJECT)" >/dev/null; \
	fi; \
fi;
endef

.PHONY: run build test lint migrate-up migrate-down docker-up docker-down integration_test openapi-generate openapi-check permissions-generate permissions-check \
		integration-search dev dev-down dev-reset dev-logs railway-status railway-deploy railway-ensure-service deploy deploy-frontend deploy-server deploy-queue-broker deploy-indexer deploy-external-event-projector \
		deploy-webhook-producer deploy-webhook-worker deploy-core build-cli-release upload-cli-release release-cli $(CLI_RELEASE_BUMP_TARGETS)

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
	cd $(SERVER_DIR) && go test -count=1 -tags=integration ./internal/integration

integration-search:
	@set -eu; \
	if [ ! -f "$(SEARCH_ENV_FILE)" ]; then \
		echo "search env file not found: $(SEARCH_ENV_FILE)" >&2; \
		exit 1; \
	fi; \
	tmp_env=$$(mktemp); \
	trap 'rm -f "$$tmp_env"' EXIT; \
	grep -E '^(TURBOPUFFER_API_KEY|TURBOPUFFER_REGION|TURBOPUFFER_NS_PREFIX|MODAL_SERVER_API_KEY|MODAL_EMBEDDING_SERVER_URL)=' "$(SEARCH_ENV_FILE)" \
		| sed -E \
			-e 's/^TURBOPUFFER_API_KEY=/INTEGRATION_TURBOPUFFER_API_KEY=/' \
			-e 's/^TURBOPUFFER_REGION=/INTEGRATION_TURBOPUFFER_REGION=/' \
			-e 's/^TURBOPUFFER_NS_PREFIX=/INTEGRATION_TURBOPUFFER_NS_PREFIX=/' \
			-e 's/^MODAL_SERVER_API_KEY=/INTEGRATION_MODAL_SERVER_API_KEY=/' \
			-e 's/^MODAL_EMBEDDING_SERVER_URL=/INTEGRATION_MODAL_EMBEDDING_SERVER_URL=/' \
		> "$$tmp_env"; \
	set -a; \
	. "$$tmp_env"; \
	set +a; \
	cd $(SERVER_DIR) && INTEGRATION_LIVE_SEARCH=1 go test -count=1 -tags=integration ./internal/integration -run '$(INTEGRATION_SEARCH_RUN)'

$(CLI_RELEASE_BUMP_TARGETS):
	@:

build-cli-release:
	@set -eu; \
	version="$$(./scripts/resolve-cli-release-version.sh --version "$(VERSION)" --bump "$(BUMP)" --goals "$(MAKECMDGOALS)")"; \
	echo "building CLI release $$version"; \
	./scripts/build-cli-release.sh "$$version"

upload-cli-release:
	@set -eu; \
	version="$$(./scripts/resolve-cli-release-version.sh --version "$(VERSION)" --bump "$(BUMP)" --goals "$(MAKECMDGOALS)")"; \
	echo "uploading CLI release $$version"; \
	./scripts/upload-cli-release.sh "$$version"

release-cli:
	@set -eu; \
	version="$$(./scripts/resolve-cli-release-version.sh --version "$(VERSION)" --bump "$(BUMP)" --goals "$(MAKECMDGOALS)")"; \
	echo "releasing CLI $$version"; \
	$(MAKE) build-cli-release VERSION="$$version"; \
	$(MAKE) upload-cli-release VERSION="$$version"

railway-status:
	@set -eu; \
	$(railway_prepare_context) \
	$(RAILWAY) status

define railway_role_for_service
$(strip \
$(if $(filter server,$(1)),server, \
$(if $(filter queue-broker,$(1)),queue-broker, \
$(if $(filter indexer,$(1)),indexer, \
$(if $(filter external-event-projector,$(1)),external-event-projector, \
$(if $(filter webhook-producer,$(1)),webhook-producer, \
$(if $(filter webhook-worker,$(1)),webhook-worker, \)))))))
endef

railway-ensure-service:
	@if [ -z "$(SERVICE)" ]; then \
		echo "SERVICE is required. Example: make railway-ensure-service SERVICE=server"; \
		exit 1; \
	fi
	@set -eu; \
	$(railway_prepare_context) \
	if $(RAILWAY) status --json | grep -Eq "\"name\"[[:space:]]*:[[:space:]]*\"$(SERVICE)\""; then \
		echo "Railway service $(SERVICE) already exists"; \
	else \
		echo "Creating Railway service $(SERVICE)"; \
		$(RAILWAY) add --service "$(SERVICE)"; \
	fi; \
	if [ -n "$(RAILWAY_SERVICE_VARS)" ]; then \
		set -- $(RAILWAY) variable set --service "$(SERVICE)" --skip-deploys; \
		if [ -n "$(RAILWAY_ENV)" ]; then \
			set -- "$$@" --environment "$(RAILWAY_ENV)"; \
		fi; \
		for kv in $(RAILWAY_SERVICE_VARS); do \
			set -- "$$@" "$$kv"; \
		done; \
		"$$@"; \
	fi

railway-deploy:
	@if [ -z "$(SERVICE)" ]; then \
		echo "SERVICE is required. Example: make railway-deploy SERVICE=server"; \
		exit 1; \
	fi
	@set -eu; \
	case "$(SERVICE)" in \
		frontend) path="frontend" ;; \
		server|queue-broker|indexer|external-event-projector|webhook-producer|webhook-worker) path="server" ;; \
		*) echo "unknown Railway service: $(SERVICE)" >&2; exit 1 ;; \
	esac; \
	project_args=""; \
	if [ -n "$(RAILWAY_PROJECT)" ]; then \
		project_args="--project $(RAILWAY_PROJECT)"; \
	fi; \
	env_args=""; \
	if [ -n "$(RAILWAY_ENV)" ]; then \
		env_args="--environment $(RAILWAY_ENV)"; \
	fi; \
	echo "Deploying $(SERVICE) from $$path"; \
	$(RAILWAY) up $(RAILWAY_UP_FLAGS) $$project_args $$env_args --service "$(SERVICE)" --path-as-root "$$path"

deploy-frontend:
	$(MAKE) railway-ensure-service SERVICE=frontend
	$(MAKE) railway-deploy SERVICE=frontend

deploy-server:
	$(MAKE) railway-ensure-service SERVICE=server RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,server)"
	$(MAKE) railway-deploy SERVICE=server

deploy-queue-broker:
	$(MAKE) railway-ensure-service SERVICE=queue-broker RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,queue-broker)"
	$(MAKE) railway-deploy SERVICE=queue-broker

deploy-indexer:
	$(MAKE) railway-ensure-service SERVICE=indexer RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,indexer)"
	$(MAKE) railway-deploy SERVICE=indexer

deploy-external-event-projector:
	$(MAKE) railway-ensure-service SERVICE=external-event-projector RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,external-event-projector)"
	$(MAKE) railway-deploy SERVICE=external-event-projector

deploy-webhook-producer:
	$(MAKE) railway-ensure-service SERVICE=webhook-producer RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,webhook-producer)"
	$(MAKE) railway-deploy SERVICE=webhook-producer

deploy-webhook-worker:
	$(MAKE) railway-ensure-service SERVICE=webhook-worker RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,webhook-worker)"
	$(MAKE) railway-deploy SERVICE=webhook-worker

deploy: deploy-core

deploy-core:
	@set -eu; \
	$(MAKE) railway-ensure-service SERVICE=frontend; \
	$(MAKE) railway-ensure-service SERVICE=server RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,server)"; \
	$(MAKE) railway-ensure-service SERVICE=queue-broker RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,queue-broker)"; \
	$(MAKE) railway-ensure-service SERVICE=indexer RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,indexer)"; \
	$(MAKE) railway-ensure-service SERVICE=external-event-projector RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,external-event-projector)"; \
	$(MAKE) railway-ensure-service SERVICE=webhook-producer RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,webhook-producer)"; \
	$(MAKE) railway-ensure-service SERVICE=webhook-worker RAILWAY_SERVICE_VARS="APP_ROLE=$(call railway_role_for_service,webhook-worker)"; \
	FLAGS="$(if $(RAILWAY_UP_FLAGS),$(RAILWAY_UP_FLAGS),$(RAILWAY_CHAIN_UP_FLAGS))"; \
	status=0; \
	$(MAKE) railway-deploy SERVICE=frontend RAILWAY_UP_FLAGS="$$FLAGS" & pid_frontend=$$!; \
	$(MAKE) railway-deploy SERVICE=server RAILWAY_UP_FLAGS="$$FLAGS" & pid_server=$$!; \
	$(MAKE) railway-deploy SERVICE=queue-broker RAILWAY_UP_FLAGS="$$FLAGS" & pid_queue_broker=$$!; \
	$(MAKE) railway-deploy SERVICE=indexer RAILWAY_UP_FLAGS="$$FLAGS" & pid_indexer=$$!; \
	$(MAKE) railway-deploy SERVICE=external-event-projector RAILWAY_UP_FLAGS="$$FLAGS" & pid_projector=$$!; \
	$(MAKE) railway-deploy SERVICE=webhook-producer RAILWAY_UP_FLAGS="$$FLAGS" & pid_webhook_producer=$$!; \
	$(MAKE) railway-deploy SERVICE=webhook-worker RAILWAY_UP_FLAGS="$$FLAGS" & pid_webhook_worker=$$!; \
	for pid in $$pid_frontend $$pid_server $$pid_queue_broker $$pid_indexer $$pid_projector $$pid_webhook_producer $$pid_webhook_worker; do \
		if ! wait $$pid; then status=1; fi; \
	done; \
	exit $$status
