SHELL := /bin/bash

.PHONY: rest tidy migrate migrate-up up down migration abigen

DOCKER_COMPOSE := docker compose
MIGRATION_NAME ?= migration

rest:
	cd src/rest && go run .

up:
	$(DOCKER_COMPOSE) up -d postgres

down:
	$(DOCKER_COMPOSE) down

tidy:
	cd src/core && go mod tidy
	cd src/adapter && go mod tidy
	cd src/migration && go mod tidy
	cd src/rest && go mod tidy

migrate:
	cd src/migration && go run .

migrate-up:
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
