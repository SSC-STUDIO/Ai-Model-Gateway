# AI Model Gateway Verification

## Fast gate
Run: go test ./internal/gateway/api ./internal/core ./internal/control/compiler

## Streaming and routing gate
Cover retry budget, provider fallback, stream start boundaries, stream idle timeout, and circuit-breaker recovery.

## Broad gate
Run: go test ./...

## Evidence required
Include changed files, relevant test names, exit codes, gateway health response, config validation result, and remaining risks.
