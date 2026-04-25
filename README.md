# Chainora API

Backend service for Chainora QR login flow across the web dApp, native wallet, and card verification flows.

## Supported Local Setup

- Go `1.25.x`
- Supabase Postgres via a direct connection string with SSL
- Windows PowerShell is the primary local workflow for this repository

`make` remains available for Unix-like environments, but the checked-in `Makefile` assumes `/bin/bash`. On Windows, run the Go commands directly.

## Configuration Model

- Public, non-secret settings live in:
  - `src/rest/config/config.yaml`
  - `src/worker/config/config.yaml`
- Secrets live in `src/migration/config/.env`
- Both REST and worker auto-load `src/migration/config/.env` when started from their module directories

Do not place secrets in YAML. The REST loader will panic if values like `jwt.secret` or `database.url` are committed there.

## Quick Start

### 1. Create the secret env file

Copy `src/migration/config/.env.example` to `src/migration/config/.env`.

Minimum values to fill:

- `DATABASE_URL`: Supabase direct Postgres URL with `sslmode=require`
- `JWT_SECRET`
- `CHAINORA_RPC_URL`

Optional feature flags and secrets are documented inline in the example file.

Example Supabase direct connection string:

```env
DATABASE_URL=postgresql://postgres.<project-ref>:<password>@db.<project-ref>.supabase.co:5432/postgres?sslmode=require
```

### 2. Review public YAML config

Update these files only for non-secret settings such as ports, CORS origins, or public RPC endpoints:

- `src/rest/config/config.yaml`
- `src/worker/config/config.yaml`

### 3. Install and tidy modules

PowerShell:

```powershell
cd src\core
go mod tidy

cd ..\adapter
go mod tidy

cd ..\migration
go mod tidy

cd ..\rest
go mod tidy

cd ..\worker
go mod tidy
```

### 4. Run migrations

PowerShell:

```powershell
cd src\migration
go run .
```

### 5. Start the services

REST API:

```powershell
cd src\rest
$env:CGO_ENABLED = "0"
go run .
```

Worker:

```powershell
cd src\worker
$env:CGO_ENABLED = "0"
go run .
```

### 6. Optional commands

Reputation backfill:

```powershell
cd src\rest
$env:CGO_ENABLED = "0"
go run . reputation-backfill [optional-pool-address]
```

Install `abigen` only when regenerating Ethereum bindings:

```powershell
go install github.com/ethereum/go-ethereum/cmd/abigen@v1.11.5
```

## Validation

PowerShell:

```powershell
cd src\rest
$env:CGO_ENABLED = "0"
go test ./...

cd ..\core
$env:CGO_ENABLED = "0"
go test ./...

cd ..\worker
$env:CGO_ENABLED = "0"
go test ./...
```

Known issue: `src/core` currently has a duplicate fake SQL driver registration in tests. Treat that as a repo issue, not a setup failure.

## Project Structure

- `src/core`: domain entities, constants, properties, and usecases
- `src/adapter`: repositories, external services, and chain clients
- `src/rest`: HTTP server, bootstrap, routing, and config loading
- `src/worker`: scheduled background jobs
- `src/migration`: SQL migration runner and migration files

## Authentication Endpoints

- `GET /v1/auth/session`
- `POST /v1/auth/verify`
