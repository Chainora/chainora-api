SHELL := /bin/bash

.PHONY: rest worker tidy migrate migrate-up migrate-all migration abigen abigen-gen
MIGRATION_NAME ?= migration
ABIGEN ?= $(shell go env GOPATH)/bin/abigen
ABI_FILE ?=
BIN_FILE ?=
OUT_FILE ?=
OUT_PKG ?= contract

rest:
	cd src/rest && go run .

worker:
	cd src/worker && go run .

tidy:
	cd src/core && go mod tidy
	cd src/adapter && go mod tidy
	cd src/migration && go mod tidy
	cd src/rest && go mod tidy
	cd src/worker && go mod tidy

migrate:
	cd src/migration && go run .

migrate-up:
	$(MAKE) migrate

migrate-all:
	$(MAKE) migrate

migration:
	@mkdir -p src/migration/migrations
	@ts=$$(date +%Y%m%d%H%M%S); \
	name=$$(echo "$(MIGRATION_NAME)" | tr ' ' '_' | tr '[:upper:]' '[:lower:]'); \
	file="src/migration/migrations/$${ts}_$${name}.up.sql"; \
	printf -- "-- migration: %s\n" "$${name}" > "$${file}"; \
	echo "Created $${file}"

abigen:
	go install github.com/ethereum/go-ethereum/cmd/abigen@v1.11.5

abigen-gen:
	@test -n "$(ABI_FILE)" || (echo "ABI_FILE is required" && exit 1)
	@test -n "$(OUT_FILE)" || (echo "OUT_FILE is required" && exit 1)
	@if [ -n "$(BIN_FILE)" ]; then \
		$(ABIGEN) --abi "$(ABI_FILE)" --bin "$(BIN_FILE)" --pkg "$(OUT_PKG)" --out "$(OUT_FILE)"; \
	else \
		$(ABIGEN) --abi "$(ABI_FILE)" --pkg "$(OUT_PKG)" --out "$(OUT_FILE)"; \
	fi
