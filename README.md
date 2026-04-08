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

### Initialize Database Config

Use a single env file and set your database URL (Supabase/Render):

```bash
cp ./src/migration/config/.env.example ./src/migration/config/.env
vi ./src/migration/config/.env
```

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
# chainora-api
