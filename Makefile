# Entry points for local development and CI.
#
# Migrations are a separate target from running the service, deliberately:
# PLAN/14-DEPLOYMENT.md requires them to run as a reviewable step before
# application rollout, never implicitly on startup in production.

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# --- local stack ------------------------------------------------------------

.PHONY: up
up: ## Start the local stack (postgres, redis, mailpit)
	docker compose -f deploy/docker-compose.yml up -d postgres redis mailpit

.PHONY: up-all
up-all: ## Start the local stack including the auth service container
	docker compose -f deploy/docker-compose.yml up -d --build

.PHONY: down
down: ## Stop the local stack, keeping data
	docker compose -f deploy/docker-compose.yml down

.PHONY: clean
clean: ## Stop the local stack and DELETE all local data
	docker compose -f deploy/docker-compose.yml down -v

.PHONY: logs
logs: ## Follow local stack logs
	docker compose -f deploy/docker-compose.yml logs -f

# --- database ---------------------------------------------------------------

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	cd backend && go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	cd backend && go run ./cmd/migrate down 1

.PHONY: migrate-status
migrate-status: ## Show the current schema version
	cd backend && go run ./cmd/migrate status

.PHONY: migrate-new
migrate-new: ## Create a new migration pair: make migrate-new NAME=add_widgets
	cd backend && go run ./cmd/migrate new $(NAME)

# --- backend ----------------------------------------------------------------

.PHONY: run
run: ## Run the auth service against the local stack
	cd backend && go run ./cmd/authservice

.PHONY: build
build: ## Build the backend binaries
	cd backend && go build ./...

.PHONY: test
test: ## Run backend unit tests
	cd backend && go test ./...

.PHONY: test-integration
test-integration: ## Run integration tests against real Postgres and Redis
	cd backend && go test -tags=integration ./...

.PHONY: vet
vet: ## Run go vet
	cd backend && go vet ./...

.PHONY: fmt
fmt: ## Format Go source
	cd backend && gofmt -w ./cmd ./internal

.PHONY: fmt-check
fmt-check: ## Fail if Go source is not formatted
	@cd backend && test -z "$$(gofmt -l ./cmd ./internal)" || (echo "unformatted files:"; gofmt -l ./cmd ./internal; exit 1)

# --- quality gates ----------------------------------------------------------

.PHONY: check
check: fmt-check vet test ## Run every fast local gate

.PHONY: hooks
hooks: ## Install the repository's git hooks
	sh scripts/install-hooks.sh
