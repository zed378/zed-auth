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

# --- API contract -----------------------------------------------------------
#
# openapi/openapi.yaml is the source of truth (ADR-013). The Go server
# interface is generated from it, so a handler that stops matching the contract
# fails to compile. These targets keep the generated artifacts honest.

REDOCLY ?= redocly/cli:1.34.2

.PHONY: openapi-lint
openapi-lint: ## Lint the API contract
	docker run --rm -v "$(CURDIR)/openapi:/spec" $(REDOCLY) 	  lint --config /spec/redocly.yaml /spec/openapi.yaml

.PHONY: openapi-generate
openapi-generate: ## Regenerate the Go server interface from the contract
	cd backend && go generate ./internal/api/

.PHONY: openapi-check
openapi-check: openapi-lint ## Fail if the generated code is stale relative to the contract
	@cd backend && go generate ./internal/api/
	@git diff --exit-code --stat -- backend/internal/api/ 	  || (echo ""; 	      echo "The generated API code is stale."; 	      echo "openapi/openapi.yaml changed without regenerating. Run:"; 	      echo "  make openapi-generate"; 	      echo "and commit the result — the generated files are committed so a"; 	      echo "reviewer sees the contract change and its consequences in one diff."; 	      exit 1)

# --- test layers ------------------------------------------------------------
#
# PLAN/11's pyramid, one target per layer, so "run the security tests" is a
# command rather than a flag someone has to remember.

.PHONY: test-security
test-security: ## Run the abuse-case tests from PLAN/11 § Security Testing
	cd backend && go test -tags=integration -shuffle=on ./tests/security/...

.PHONY: test-e2e
test-e2e: ## Run the console end-to-end tests (needs a browser)
	cd console && npx playwright test

.PHONY: test-coverage
test-coverage: ## Check the coverage floors on the security-critical packages
	sh scripts/check-coverage.sh

# --- quality gates ----------------------------------------------------------

.PHONY: check
check: fmt-check vet test ## Run the fast local gates (format, vet, unit tests)

.PHONY: check-all
check-all: ## Run every gate CI runs — integration, security scans, shell, compose
	sh scripts/check.sh

.PHONY: hooks
hooks: ## Install the repository's git hooks
	sh scripts/install-hooks.sh
