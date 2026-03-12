.PHONY: build test lint run import

BINARY ?= ./bin/dash14
CMD ?= ./cmd/dash14
CONFIG ?= config.yaml
IMPORT_FILE ?= teams/teams.yaml

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
