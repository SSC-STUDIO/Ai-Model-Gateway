# Task Plan — Revision 108: Fix WebSocket NewProxy nil CheckOrigin panic

## Goal
Fix a nil pointer dereference panic in `internal/gateway/websocket.NewProxy()` that crashes the gateway when the websocket upgrader's `CheckOrigin` field is called without being initialized. `NewProxy()` calls `NewProxyWithOrigin(nil, nil)` which sets `CheckOrigin: nil` (line 104 of proxy.go). Any direct call to `proxy.upgrader.CheckOrigin(r)` panics because Go cannot call a nil function value. The test `TestNewProxy` at `proxy_test.go:48` reproduces this with `proxy.upgrader.CheckOrigin(nil)`. The fix: when `fn` is nil, set `CheckOrigin` to `DefaultCheckOrigin(nil)` (reject-all cross-origin, allow non-browser) instead of leaving it nil.

## Baseline
- HEAD: `9ea36f1` on branch `main` (published revision 107)
- Working tree: clean (no staged/unstaged changes)
- Broad gate `go test ./...`: two FAIL packages
  1. `internal/gateway/ratelimit` — `TestMiddleware_NoAuthHeader` fails (burst=0 IP rate limit denies no-auth requests; separate defect, NOT in scope)
  2. `internal/gateway/websocket` — `TestNewProxy` panics with nil pointer dereference at `proxy_test.go:48` calling `CheckOrigin(nil)`
- Fast gate (`go test ./internal/gateway/api ./internal/core ./internal/control/compiler`): 3/3 PASS
- The websocket panic is the target: `NewProxyWithOrigin` leaves `CheckOrigin` as nil when `fn` is nil

## Scope
- `internal/gateway/websocket/proxy.go` — fix `NewProxyWithOrigin` to set safe default `CheckOrigin` when `fn` is nil
- `internal/gateway/websocket/proxy_test.go` — existing `TestNewProxy` validates the fix
- `ai/task-plans/108-websocket-nil-checkorigin.md` — this plan file

## Steps
1. Create this task plan (DONE)
2. Read `NewProxyWithOrigin` in `proxy.go` lines 98-128 to confirm the nil CheckOrigin path
3. Edit `NewProxyWithOrigin`: when `fn` is nil, set `CheckOrigin: DefaultCheckOrigin(nil)` instead of `CheckOrigin: fn` (which passes nil)
4. Also set `allowedOrigin: DefaultCheckOrigin(nil)` for consistency when `fn` is nil
5. Run focused test: `go test ./internal/gateway/websocket/... -count=1 -v -run "TestNewProxy"`
6. Run fast gate: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1`
7. Run broad gate: `go test ./... -count=1` and confirm websocket passes (ratelimit failure is pre-existing, out of scope)
8. Update this plan with actual results

## Verification
- Focused: `go test ./internal/gateway/websocket/... -count=1 -v -run "TestNewProxy"` — expect PASS
- Fast gate: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1` — expect 3/3 OK
- Vet: `go vet ./internal/gateway/websocket/...` — expect clean
- Broad gate: `go test ./... -count=1` — websocket should pass; ratelimit failure is pre-existing baseline

## Risks
1. Changing the default CheckOrigin from nil (panic) to reject-all could break callers that rely on nil meaning "allow all" — but gorilla's `Upgrade()` method treats nil as same-origin check, not allow-all, so this is actually safer
2. The ratelimit `TestMiddleware_NoAuthHeader` failure is a separate pre-existing defect, not addressed in this revision
3. No public API change — `CheckOrigin` field behavior is internal to the Proxy type

## Stop Conditions
- If the fix breaks other websocket tests, stop and report
- If changes require touching files outside `internal/`, stop
- Do not modify gorilla/websocket library; only our proxy.go

## Evidence
- Pre-fix: `TestNewProxy` panics with "nil pointer dereference" at `proxy_test.go:48` calling `CheckOrigin(nil)`
- Root cause: `NewProxyWithOrigin(nil, nil)` set `CheckOrigin: nil` at proxy.go:104. Calling a nil func value panics. Additionally, even with a non-nil CheckOrigin, `DefaultCheckOrigin` would dereference `r.Header` on a nil request.
- Fix (two changes, 7 lines total):
  1. `NewProxyWithOrigin` (proxy.go:100-102): when `fn` is nil, set `fn = DefaultCheckOrigin(nil)` before constructing the Proxy struct — prevents nil function panic
  2. `DefaultCheckOrigin` closure (proxy.go:46-49): add nil request guard `if r == nil { return true }` — treats nil as a non-browser client with no Origin header (consistent with the existing empty-Origin == true logic on line 50)
- Post-fix: `TestNewProxy` PASS (0.00s)
- Full websocket suite: `ok  ai-model-gateway/internal/gateway/websocket  3.857s` — all tests pass
- Fast gate: 3/3 PASS (api 0.766s, core 1.165s, compiler 1.118s)
- Vet: clean (exit 0)
- Broad gate: websocket went from FAIL to PASS. Ratelimit `TestMiddleware_NoAuthHeader` FAIL remains — pre-existing, unrelated to this change
- Changed files: `internal/gateway/websocket/proxy.go` (+7 lines), `ai/task-plans/108-websocket-nil-checkorigin.md` (this plan)
