# Migration Agent Notes

## Scope
- SQL migrations and migration runner configuration

## Rules
- Use forward-only migration pattern.
- Avoid destructive operations without explicit backup plan.
- Keep migration files deterministic and idempotent where possible.

## Verify
- `cd src/migration && go run .`
