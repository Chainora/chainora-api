# Chainora API Agent Guide

## Scope
Modular Go backend in this folder.

## Prompt Update Rule
After every prompt that changes this folder, update this `agent.md`.
Keep updates short and focused on: changed areas, verification commands, and caveats.

## Agent Skill
- Custom agent: `.github/agents/chainora-api.agent.md`
- Skill: `.github/skills/chainora-api/SKILL.md`

## Fast Navigation
- `src/core/`: domain entities and usecases
- `src/adapter/`: repositories and external clients
- `src/rest/`: Gin handlers, routing, and WebSocket hub
- `src/migration/`: SQL migrations
- `src/worker/`: background worker runtime (`bootstrap/config/handlers/jobs/orchestrators/routers/scanners`)

## Username Policy
- Username is on-chain source-of-truth.
- Do not re-introduce manual username edit endpoint in backend.
- `GET /v1/auth/profile` resolves username via Initia username APIs.

## Verify
- `cd src/rest && go test ./...`
- `cd src/worker && go test ./...`
- `make rest`
- `make worker`
