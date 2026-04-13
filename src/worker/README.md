# Worker Module

Small background worker scaffold for contract-related async logic.

## What exists now
- HTTP liveness endpoints: `/healthz`, `/readyz`
- Scheduler loop with periodic jobs
- `username-sync` sample job that resolves primary Initia usernames for configured addresses

## Structure
- `bootstrap/`: app wiring
- `config/`: YAML config loading
- `handlers/`: HTTP handlers
- `jobs/`: job interfaces and implementations
- `orchestrators/`: scheduler runtime
- `routers/`: route registration
- `scanners/`: chain/API scanning helpers
- `tests/handlers/`: handler tests

## Run locally
From repo root:

```bash
make worker
```

Or directly:

```bash
cd src/worker && go run .
```

## Environment
- Worker runtime config is YAML at `src/worker/config/config.yaml`.
