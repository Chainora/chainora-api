---
name: chainora-api
description: "Implement and refactor chainora-api backend features with high exactness and prompt saveability. Use for QR auth flow updates, repository swaps, WebSocket behavior fixes, and modular clean-architecture changes."
argument-hint: "Task goal, non-negotiable constraints, target files, and acceptance criteria"
user-invocable: true
---

# Chainora API Exactness Skill

Use this skill when a task needs precise requirement execution and prompt-to-code traceability.

## When To Use
- API endpoint or payload behavior changes.
- QR auth flow adjustments.
- Repository/adapter swaps (in-memory to DB, RPC clients).
- Multi-module changes requiring import and boundary discipline.

## Procedure
1. Create a requirement contract using [prompt contract template](./assets/prompt-contract-template.md).
2. Identify invariant-sensitive paths (module imports, API contracts, signature verification logic, WS hub behavior).
3. Apply minimal, focused edits only in necessary files.
4. Run validation from [verification checklist](./assets/verification-checklist.md).
5. Report requirement coverage explicitly in the final response.

## Chainora-API Specific Guardrails
- Preserve strict module imports between `rest`, `core`, and `adapter`.
- Keep EIP-191 signature verification compatibility.
- Keep V handling compatible with 0/1 and 27/28.
- Keep WebSocket session hub behavior deterministic.
- Username source-of-truth is on-chain; avoid backend username edit paths.
- For async contract logic, prefer `src/worker` job + scanner + orchestrator flow over ad-hoc goroutines in REST handlers.
- Keep configuration module-local (`src/rest/config/config.yaml`, `src/worker/config/config.yaml`) instead of centralizing all runtime envs.
- Primary username selection must remain signature-gated when exposed through relayer endpoints.

## Output Requirements
- Requirement Coverage: each user requirement mapped to file-level changes.
- Validation Results: exact command(s) executed.
- Follow-up Risks: only if relevant.
