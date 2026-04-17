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
- Worker auto-loads `../migration/config/.env` (or `CHAINORA_SECRET_ENV_FILE`) for secrets.
- For dynamic `username-sync` wallet discovery, set `DB_URL`/`DATABASE_URL` (or `database.url` in worker YAML).  
  When `username_sync.addresses` is empty, worker will read distinct addresses from `users.address` and `groups.creator_address`.
- Optional override: set `USERNAME_SYNC_ADDRESSES` as comma-separated wallets to merge with YAML list.
