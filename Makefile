# Simplify Docker Compose detection - just use the command directly
# This works with both Docker Compose V2 and V1
DOCKER_COMPOSE_CMD = docker compose

# Only check for flow and go when running commands that need them
check-flow:
ifeq (, $(shell which flow))
	$(error "No flow in PATH. Install Flow CLI: https://docs.onflow.org/flow-cli/install/")
endif

check-go:
ifeq (, $(shell which go))
	$(error "No go in PATH")
endif

dev = $(DOCKER_COMPOSE_CMD) -f docker-compose.dev.yml -p flow-wallet-api-dev
lightweight = $(DOCKER_COMPOSE_CMD) -f docker-compose.lightweight.yml -p flow-wallet-api-lightweight
test-suite = $(DOCKER_COMPOSE_CMD) -f docker-compose.test-suite.yml -p flow-wallet-api-test

.PHONY: dev
dev:
	@$(dev) up --remove-orphans -d db pgadmin emulator redis

.PHONY: stop
stop:
	@$(dev) stop

.PHONY: down
down:
	@$(dev) down --remove-orphans

.PHONY: reset
reset: down dev

.PHONY: lightweight
lightweight:
	@$(lightweight) down --remove-orphans || true
	@$(lightweight) build api
	@$(lightweight) up -d

.PHONY: lightweight-logs
lightweight-logs:
	@$(lightweight) logs -f

.PHONY: lightweight-stop
lightweight-stop:
	@$(lightweight) stop

.PHONY: lightweight-down
lightweight-down:
	@$(lightweight) down --remove-orphans

.PHONY: lightweight-reset
lightweight-reset: lightweight-down lightweight

.PHONY: run-tests
run-tests: check-go
	@go test ./... -p 1

.PHONY: test
test: check-flow check-go start-emulator deploy run-tests

.PHONY: test-clean
test-clean: check-go clean-test-cache test

.PHONY: clean-test-cache
clean-test-cache: check-go
	@go clean -testcache

.PHONY: deploy
deploy: check-flow
	@cd flow && flow project deploy --update

.PHONY: start-emulator
start-emulator: check-flow emulator.pid
	@sleep 1

.PHONY: stop-emulator
stop-emulator: check-flow emulator.pid
	@kill `cat $<` && rm $<

emulator.pid: check-flow
	@cd flow && { flow emulator -b 100ms & echo $$! > ../$@; }

.PHONY: lint
lint:
	@golangci-lint run

.PHONY: run-test-suite
run-test-suite:
	@$(test-suite) build flow api
	@$(test-suite) up --remove-orphans -d db redis flow
	@$(test-suite) unpause \
	; echo "\nRunning tests, hang on...\n" \
	; $(test-suite) run --rm api go test ./... -p 1 \
	; echo "\nRunning linter, hang on...\n" \
	; $(test-suite) run --rm lint golangci-lint run \
	; $(test-suite) pause

.PHONY: stop-test-suite
stop-test-suite:
	@$(test-suite) down --remove-orphans

.PHONY: clean-test-suite
clean-test-suite:
	@$(test-suite) run --rm api go clean -testcache
