# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Install/tidy all modules
make tidy

# Run database migrations
make migrate

# Start REST API server (port 8080)
make rest

# Start background worker (port 8090)
make worker

# Run tests (from repo root)
cd src/rest && go test ./...
cd src/core && go test ./...
cd src/worker && go test ./...

# Run a single test
cd src/core && go test ./usecases/... -run TestGroupLifecycle

# Create a new migration file
make migration MIGRATION_NAME=add_some_table

# Reputation backfill (one-shot, run from src/rest)
cd src/rest && go run . reputation-backfill [optional-pool-address]

# Generate Go bindings from Ethereum ABI
make abigen-gen ABI_FILE=path/to/Contract.abi BIN_FILE=path/to/Contract.bin OUT_FILE=src/adapter/ethclient/contract.go OUT_PKG=ethclient
```

## Architecture

This is a **Go workspace** (`go.work`) with five independent modules:

```
src/core       — Domain layer: entities, usecases (interfaces + implementations), constants
src/adapter    — Infrastructure: repositories, JWT, EIP-191 crypto, Initia username service, EVM multi-client
src/rest       — HTTP server: Gin controllers, routers, middlewares, config, bootstrap (DI root)
src/worker     — Background worker: username sync, group invite notifications, funding reminders
src/migration  — SQL migration runner; migration files in migrations/
```

**Dependency direction:** `rest` → `core` + `adapter`; `worker` → `core`; `adapter` → `core`. The `core` module has no internal dependencies.

### Key design patterns

**Dependency injection** is assembled in `src/rest/bootstrap/bootstrap.go` (the DI root). All interfaces are defined in `src/core/usecases/` and implemented in `src/adapter/`.

**Config loading** (both `rest` and `worker`): YAML file is the non-secret public config (`src/rest/config/config.yaml`, `src/worker/config/config.yaml`). Secrets are loaded from `src/migration/config/.env` (auto-discovered relative path). The REST config enforcer will `panic` if secrets like `jwt.secret` or `database.url` appear in the YAML file.

**Auth flow:** QR login over WebSocket. Session is created (`GET /v1/auth/session`), mobile wallet signs the nonce and posts to `POST /v1/auth/verify`, result is broadcast via `WS /v1/auth/ws/:sessionId`. Sessions are stored in-memory (`sync.Map`) unless Postgres is configured.

**Groups / Pool state:** Group metadata is in Postgres; live on-chain state (`poolStatus`, `currentCycle`, etc.) is read from the pool smart contract via go-ethereum ABI calls. On list/get, stale records (>10s old) trigger async background refresh to keep latency low. Pass `?sync=true` to force synchronous refresh.

**Reputation:** Scored via `user_reputation_ledger` table (ledger-style, idempotent upserts). Synced on-chain to `ChainoraReputationAdapter` contract via `ReputationSyncService`. Triggered when a pool cycle completes.

**Worker jobs:** `UsernameSyncJob` polls Initia username API; `GroupInviteNotificationJob` and `FundingReminderNotificationJob` query Postgres + chain RPC, then insert rows into `notifications`. Scheduled by `orchestrators.Scheduler` on a configurable interval.

### Username policy

Username is sourced from the Initia on-chain name registry — the backend never stores a writable username field. `GET /v1/auth/profile` resolves via the Initia username API (`services.InitiaUsernameService`). Do not add a direct username-edit endpoint.

### API prefix

Routes are registered under both `/v1/...` and `/api/v1/...` (same handler, both prefixes are live).

## Config reference

| Secret env var | Purpose |
|---|---|
| `DATABASE_URL` / `DB_URL` | Postgres connection string |
| `JWT_SECRET` | JWT signing key |
| `RELAYER_MASTER_PRIVATE_KEY` | Hex private key for username relayer |
| `CARD_DEVICE_VERIFIER_PRIVATE_KEY` | Hex private key for device attestation signing |
| `REPUTATION_VERIFIER_PRIVATE_KEY` | Hex key for reputation proof signing |
| `REPUTATION_TX_SENDER_PRIVATE_KEY` | Hex key for on-chain reputation sync transactions |
| `CHAINORA_RPC_URL` | EVM RPC URL for pool contract calls |

All secrets must live in `src/migration/config/.env` (or be set as real env vars). The YAML config files must not contain any secret values — the loader panics on validation failure.
