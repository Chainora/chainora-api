# REST Agent Notes

## Scope
- API routing, handlers, middleware, bootstrap wiring

## Rules
- Keep handlers thin; business logic stays in `core/usecases`.
- Preserve response envelope format.
- Keep auth and websocket flow backward compatible unless explicitly changed.

## Verify
- `cd src/rest && go test ./...`
