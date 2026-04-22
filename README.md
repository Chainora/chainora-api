# Chainora API

Backend service for Chainora QR login flow (Web DApp + Mobile App + JavaCard signature verification).

## Quick Start

## Prerequisites

### Go Environment Setup

Use Go `1.24.x` (or newer compatible with this repository).

Verify these environment variables:
- `GOROOT`: Go installation directory
- `GOPATH`: local Go workspace path

Recommendation: use `gvm` (Go Version Manager) to manage versions and quickly switch environments.

### Docker (optional)

Docker is only needed when you want to build/run the backend image locally.

### Abigen

Install the Go binding generator for Ethereum smart contracts:

```bash
go install github.com/ethereum/go-ethereum/cmd/abigen@v1.11.5
```

Or run:

```bash
make abigen
```

## Bring Up Local Development Environment

### Initialize Module Config

Use module-local config files:

```bash
# edit directly (already created in repo)
vi ./src/rest/config/config.yaml
vi ./src/worker/config/config.yaml

# only migration env needs a private local file
cp ./src/migration/config/.env.example ./src/migration/config/.env
```

Policy:
- `src/rest/config/config.yaml` and `src/worker/config/config.yaml` are public, prefilled, non-secret config.
- Sensitive values (JWT secret, DB URL, relayer private key) stay in hidden `src/migration/config/.env` and are loaded for runtime secrets.
- REST security controls are configured in YAML under `security`:
	- `allowed_origins`
	- `max_request_body_bytes`
	- `allow_empty_origin_for_ws`

### Run Setup Commands

Run these commands in order:

```bash
make tidy
make migrate
```

Start API server:

```bash
make rest
```

Run one-shot reputation backfill (sync DB `users.reputation_score` to on-chain `ChainoraReputationAdapter`):

```bash
cd src/rest
go run . reputation-backfill [optional-pool-address]
```

Run worker:

```bash
make worker
```

## Making Migrations

Create a new migration file:

```bash
make migration
```

Optional custom name:

```bash
make migration MIGRATION_NAME=create_auth_sessions
```

## Generate Go Binding For Smart Contracts

```bash
make abigen
```

Generate bindings from ABI/BIN:

```bash
make abigen-gen ABI_FILE=path/to/Contract.abi BIN_FILE=path/to/Contract.bin OUT_FILE=src/adapter/ethclient/contract.go OUT_PKG=ethclient
```

## Project Structure

- `src/core`: domain entities, constants, properties, and usecases
- `src/adapter`: repository/service/eth clients
- `src/rest`: controllers, routers, middlewares, bootstrap, config
- `src/migration`: SQL migration runner and migration files

## Authentication Endpoints

- `GET /v1/auth/session`
- `GET /v1/auth/ws/:sessionId` (WebSocket)
- `POST /v1/auth/verify`

## Notes

- Current auth repository implementation is in-memory (`sync.Map`).
- Signature verification uses EIP-191 + secp256k1 recovery and handles both `v=0/1` and `v=27/28`.
- Username feature guide: `docs/USERNAME_FEATURE.md`.
- Reputation sync env (secret env only):
  - `REPUTATION_VERIFIER_PRIVATE_KEY`
  - `REPUTATION_TX_SENDER_PRIVATE_KEY`
  - `REPUTATION_SYNC_DEADLINE_SECONDS` (default `600`)
  - `REPUTATION_SYNC_RETRY_MAX` (default `2`)
  - `REPUTATION_SYNC_COOLDOWN_SECONDS` (default `15`)
  - `REPUTATION_SYNC_BATCH_SIZE` (default `100`)
# chainora-api
