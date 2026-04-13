# Adapter Agent Notes

## Scope
- Repository implementations
- External clients/services (RPC, relayer, crypto helpers)

## Rules
- Keep IO boundaries in `adapter` only.
- Do not add business rules that belong to `core/usecases`.
- Return explicit, actionable errors.

## Verify
- `cd src/adapter && go test ./...`
