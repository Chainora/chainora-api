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
- `src/worker/`: background workers

## Verify
- `cd src/rest && go test ./...`
- `make rest`
