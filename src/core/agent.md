# Core Agent Notes

## Scope
- Domain entities, constants, usecases, interfaces

## Rules
- Keep core framework-agnostic.
- No direct HTTP, DB, or SDK usage in core.
- Validate invariants in usecases.

## Verify
- `cd src/core && go test ./...`
