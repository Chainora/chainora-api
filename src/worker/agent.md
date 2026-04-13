# Worker Agent Notes

## Scope
- Background scheduler, jobs, scanners, worker HTTP health endpoints

## Rules
- Put async/retry logic in worker jobs, not REST handlers.
- Keep jobs idempotent and observable via logs.
- Use YAML under `src/worker/config/config.yaml` for worker config.

## Verify
- `cd src/worker && go test ./...`
