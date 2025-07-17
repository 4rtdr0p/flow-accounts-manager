# Simplify Docker Compose detection - just use the command directly
# This works with both Docker Compose V2 and V1
DOCKER_COMPOSE_CMD = docker compose

# Detect OS for local commands
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
    OPEN_CMD = xdg-open
    KILL_CMD = pkill -f
else ifeq ($(UNAME_S),Darwin)
    OPEN_CMD = open
    KILL_CMD = pkill -f
else
    OPEN_CMD = echo "Unsupported OS for open command:"
    KILL_CMD = echo "Unsupported OS for kill command:"
endif

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

# Local commands without Docker
.PHONY: local-start
local-start: check-flow check-go
	@echo "Starting Phoenix Wallet API services locally..."
	@echo "Creating necessary directories..."
	@mkdir -p ./data
	
	@echo "Starting Flow Emulator..."
	@cd flow && { flow emulator --persist --storage-dir="../data/flowdb" --port=3569 & echo $$! > ../emulator.pid; }
	@echo "Flow Emulator started with PID: $$(cat emulator.pid)"
	@sleep 3
	
	@echo "Starting Swagger UI (using npx)..."
	@npx swagger-ui-dist@latest serve ./openapi.yml -p 8081 & echo $$! > swagger.pid
	@echo "Swagger UI started with PID: $$(cat swagger.pid)"
	@sleep 2
	
	@echo "Building and starting Phoenix Wallet API..."
	@go build -o ./phoenix-wallet-api
	@FLOW_WALLET_LIGHTWEIGHT_MODE=true \
	FLOW_WALLET_ACCESS_API_HOST=localhost:3569 \
	FLOW_WALLET_CHAIN_ID=flow-emulator \
	FLOW_WALLET_DATABASE_DSN=./data/wallet.db \
	./phoenix-wallet-api & echo $$! > api.pid
	@echo "Phoenix Wallet API started with PID: $$(cat api.pid)"
	
	@echo "All services started successfully!"
	@echo "API: http://localhost:3000"
	@echo "Swagger UI: http://localhost:8081"
	@echo "Flow Emulator: localhost:3569"
	@$(OPEN_CMD) http://localhost:8081

.PHONY: local-stop
local-stop:
	@echo "Stopping all Phoenix Wallet API services..."
	@if [ -f emulator.pid ]; then kill $$(cat emulator.pid) && rm emulator.pid && echo "Flow Emulator stopped"; else echo "Flow Emulator not running"; fi
	@if [ -f swagger.pid ]; then kill $$(cat swagger.pid) && rm swagger.pid && echo "Swagger UI stopped"; else echo "Swagger UI not running"; fi
	@if [ -f api.pid ]; then kill $$(cat api.pid) && rm api.pid && echo "Phoenix Wallet API stopped"; else echo "Phoenix Wallet API not running"; fi
	@echo "All services stopped successfully!"

.PHONY: local-status
local-status:
	@echo "Checking status of Phoenix Wallet API services..."
	@if [ -f emulator.pid ] && ps -p $$(cat emulator.pid) > /dev/null; then echo "Flow Emulator: Running (PID: $$(cat emulator.pid))"; else echo "Flow Emulator: Not running"; fi
	@if [ -f swagger.pid ] && ps -p $$(cat swagger.pid) > /dev/null; then echo "Swagger UI: Running (PID: $$(cat swagger.pid))"; else echo "Swagger UI: Not running"; fi
	@if [ -f api.pid ] && ps -p $$(cat api.pid) > /dev/null; then echo "Phoenix Wallet API: Running (PID: $$(cat api.pid))"; else echo "Phoenix Wallet API: Not running"; fi
