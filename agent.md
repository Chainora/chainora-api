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

## Session 2026-04-15 createpool-hotfix
Summary: No module code changes this session.
Changed files: chainora-dapp/.env.contracts.example, chainora-dapp/.env.local, chainora-dapp/src/contract/chainoraAbis.ts, chainora-dapp/src/contract/chainoraProtocol.ts, chainora-dapp/src/pages/create-group.tsx, chainora-dapp/src/pages/dashboard.tsx, chainora-native-app/src/services/qrLoginService.ts, chainora-native-app/src/screens/QRScannerScreen.tsx
Validation: dapp yarn typecheck + yarn build passed; native npx tsc --noEmit passed
Next steps: Run mobile QR->NFC createPool on deployed contracts and confirm device adapter verification status for wallet

## Session 2026-04-15 create-pool onchain device verification
Summary: Added backend endpoint to issue EIP-712 device attestation for ChainoraDeviceAdapter after backend hardware verification.
Changed files: chainora-api/src/rest/handler/card_handler.go, chainora-api/src/rest/routers/routes.go, chainora-api/src/rest/bootstrap/bootstrap.go, chainora-api/src/rest/config/config.go, chainora-api/src/rest/config/config.yaml, chainora-api/src/rest/config/config.yaml.example, chainora-api/src/rest/properties/properties.go, chainora-native-app/src/services/qrLoginService.ts
Validation: go test ./... (rest) passed; npx tsc --noEmit (native) passed; yarn typecheck (dapp) passed
Next steps: Set CARD_DEVICE_VERIFIER_PRIVATE_KEY and trust its address on ChainoraDeviceAdapter, then run QR+NFC createPool E2E

## Session 2026-04-15 create-pool session lock + status sync
Summary: No module code changes this session.
Changed files: chainora-dapp/src/pages/create-group.tsx, chainora-dapp/.env.contracts.example, chainora-dapp/.env.local, chainora-native-app/src/services/qrLoginService.ts, chainora-native-app/src/screens/QRScannerScreen.tsx
Validation: native npx tsc --noEmit passed; dapp yarn typecheck passed
Next steps: Restart dapp/native/api, verify one-scan create-pool UX and confirm registry points to new factory dependencies

## Session 2026-04-15 precheck latency analysis
Summary: No module code changes. Traced create-pool precheck latency path and identified native+RPC bottlenecks affecting dapp status updates.
Changed files: none
Validation: Code inspection only (no build/test run).
Next steps: Optionally tune viem transport retry/timeout and trim duplicate on-chain diagnostics to reduce precheck wait time.

## Session 2026-04-15 create-pool precheck latency optimization
Summary: No module code changes. Continued create-pool incident triage and focused optimization in native precheck execution path.
Changed files: chainora-native-app/src/services/web3Client.ts; chainora-native-app/src/services/qrLoginService.ts; chainora-native-app/src/services/activitySyncService.ts; chainora-native-app/src/screens/QRScannerScreen.tsx
Validation: cd chainora-native-app && npx tsc --noEmit (pass)
Next steps: Measure median create_pool_precheck duration from scan to signing stage; if still high, add dedicated fast RPC endpoint for precheck-only reads.

## Session 2026-04-15 login-session device verification warmup
Summary: No module code changes. Kept current auth/card endpoints and reused existing progress websocket contract for additional login warmup statuses.
Changed files: chainora-dapp/src/services/authQrFlow.ts; chainora-dapp/src/components/auth/HeaderLoginButton.tsx; chainora-native-app/src/services/qrLoginService.ts; chainora-native-app/src/screens/QRScannerScreen.tsx
Validation: cd chainora-native-app && npx tsc --noEmit (pass); cd chainora-dapp && yarn typecheck (pass)
Next steps: Test one full login scan on mobile with stable RPC and verify ws statuses login_device_* then check create-pool precheck bypasses device adapter verification path.

## Session 2026-04-15 precheck-timeout bypass and new pool implementation config
Summary: No module code changes. Existing auth/progress/card endpoints were reused for continued create-pool/login flows.
Changed files: chainora-native-app/src/services/qrLoginService.ts; chainora-dapp/src/pages/create-group.tsx; chainora-dapp/.env.local; chainora-dapp/.env.contracts.example
Validation: cd chainora-native-app && npx tsc --noEmit (pass); cd chainora-dapp && yarn typecheck (pass)
Next steps: Restart dapp/native to reload env and flow logic, then test create-pool after login warmup; if tx still stalls, switch Chainora RPC endpoint to a healthier node.

## Session 2026-04-15 login/create-pool smoothness speed pass
Summary: No module code changes. Reused existing auth/progress and relayer contracts; no endpoint shape changes required.
Changed files: chainora-native-app/src/screens/QRScannerScreen.tsx; chainora-native-app/src/services/qrLoginService.ts; chainora-native-app/src/services/transactionService.ts
Validation: cd chainora-native-app && npx tsc --noEmit (pass); cd chainora-dapp && yarn typecheck (pass)
Next steps: Restart Metro/native app and dapp dev server; then verify login closes promptly after auth verify and create-pool reaches signing stage even when precheck RPC is sluggish.

## Session 2026-04-15 10:45
Summary: Enabled fast dashboard listing by returning DB group rows immediately and refreshing stale on-chain state asynchronously by default; added optional sync=true for force-sync requests.
Changed files: chainora-api/src/rest/handler/group_handler.go, chainora-dapp/src/services/groupsService.ts, chainora-dapp/src/pages/dashboard.tsx
Validation: GOCACHE=/tmp/go-build-cache go test ./... (chainora-api/src/rest) passed; yarn typecheck (chainora-dapp) passed
Next steps: Configure worker username_sync.addresses from DB-derived wallet list and restart worker/api services.
