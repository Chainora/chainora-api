# Chainora API Setup Guide

## Prerequisites
- Go 1.24+
- Docker (for Postgres)
- GNU Make
- Abigen (optional for contract bindings)

Install abigen:

```bash
make abigen
```

## 0) Initialize Migration Config

```bash
cp ./src/migration/config/local.yml.example ./src/migration/config/local.yml
```

## 1) Start Postgres (Docker)

```bash
make up
```

## 2) Install Dependencies

```bash
make tidy
```

## 3) Apply SQL Migrations

```bash
make migrate
```

The runner:
- reads `.up.sql` files from `src/migration/migrations/`
- applies them in sorted filename order
- records applied files in `schema_migrations`

## 4) Run REST API

```bash
make rest
```

Server default: `http://localhost:8080`

## 5) Full Docker Compose Flow

```bash
# Start DB
docker compose up -d postgres

# Run one-shot migration container
docker compose run --rm migrate

# Start API container
docker compose up -d api
```

## 6) Create New Migration

```bash
make migration
```

Optional name:

```bash
make migration MIGRATION_NAME=create_auth_sessions
```

## 7) Auth QR Flow
1. Dapp calls `GET /v1/auth/session` and receives `sessionId` + `nonce`.
2. Dapp opens `WS /v1/auth/ws/:sessionId` and waits.
3. Mobile app signs the nonce and calls `POST /v1/auth/verify`.
4. Server verifies signature (EIP-191), then pushes verified event to WebSocket.

## Signature Payload Notes
- Accepts signature as `r||s||v` (65 bytes) or `r||s` (64 bytes).
- If `v` is omitted in signature, send optional JSON field `v`.
- Supports both Ethereum styles for recovery bit: `0/1` and `27/28`.
