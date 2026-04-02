# Verification Checklist

## Pre-Edit
- Identify behavior-sensitive paths (auth session flow, signature verify, WS notifications).
- Confirm non-negotiable constraints.

## Post-Edit
- Run targeted diagnostics.
- Run compile checks:
  - `cd src/rest && go test ./...`
- Verify requirement-to-edit mapping is complete.

## For API Changes
- Verify endpoint routes and method bindings.
- Verify request/response payload compatibility.
- Verify status codes for success/failure paths.

## For Auth/Signature Changes
- Verify EIP-191 hash behavior remains correct.
- Verify signature lengths (64/65) and V normalization logic.
- Verify WS broadcast on successful verification.
