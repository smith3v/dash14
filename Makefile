.PHONY: build test lint run import compose-up compose-down compose-logs compose-config compose-dev-up

BINARY ?= ./bin/dash14
CMD ?= ./cmd/dash14
CONFIG ?= config.yaml
IMPORT_FILE ?= teams/teams.yaml
COMPOSE ?= docker compose
COMPOSE_FILES ?= -f docker-compose.yml
COMPOSE_DEV_FILES ?= -f docker-compose.yml -f docker-compose.dev.yml

build:
	mkdir -p $(dir $(BINARY))
	go build -o $(BINARY) $(CMD)

test:
	go test ./...

lint:
	@fmt_out="$$(gofmt -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "gofmt check failed for:"; \
		echo "$$fmt_out"; \
		exit 1; \
	fi
	go vet ./...

run:
	go run $(CMD) --config $(CONFIG)

import:
	go run $(CMD) --config $(CONFIG) --import $(IMPORT_FILE)

compose-up:
	$(COMPOSE) $(COMPOSE_FILES) up -d

compose-down:
	$(COMPOSE) $(COMPOSE_FILES) down

compose-logs:
	$(COMPOSE) $(COMPOSE_FILES) logs -f

compose-config:
	$(COMPOSE) $(COMPOSE_FILES) config

compose-dev-up:
	$(COMPOSE) $(COMPOSE_DEV_FILES) up --build
