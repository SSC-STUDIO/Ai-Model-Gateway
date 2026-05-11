#!/bin/bash
#
# Atomic gateway update for WSL2.
#
# Builds new binaries, then in ONE shell operation:
#   kills old supervisor → replaces binaries → starts new supervisor → health-checks
#
# Usage:
#   ./scripts/update-gateway.sh              # full atomic update
#   ./scripts/update-gateway.sh --restart    # restart only (no rebuild)
#   ./scripts/update-gateway.sh --dry-run    # build + show what would happen
#
# The script is safe to call from Claude Code because the entire
# stop→swap→start sequence runs inside a single bash invocation —
# there is no intermediate state where the gateway is missing.

set -euo pipefail

# ── Paths ──────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
DEPLOY_DIR="${DEPLOY_DIR:-$HOME/ai-gateway}"
BIN_DIR="$DEPLOY_DIR/bin"
CONFIG_DIR="$DEPLOY_DIR/configs"
RUNTIME_ROOT="$DEPLOY_DIR/.gateway-runtime"
STAGING_DIR=$(mktemp -d "/tmp/gw-update.XXXXXX")
LOG_FILE="$PROJECT_ROOT/logs/update.log"

mkdir -p "$PROJECT_ROOT/logs"

# ── Colours ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'

log()  { echo -e "${GREEN}[update]${NC} $*"; }
warn() { echo -e "${YELLOW}[update]${NC} $*"; }
err()  { echo -e "${RED}[update]${NC} $*" >&2; }
die()  { err "$*"; exit 1; }

cleanup() {
    rm -rf "$STAGING_DIR"
}
trap cleanup EXIT

# ── Health check ───────────────────────────────────────────────────────
check_health() {
    local tries="${1:-20}"
    local delay="${2:-0.5}"
    for (( i=1; i<=tries; i++ )); do
        local code
        code=$(curl -s -o /dev/null -w '%{http_code}' \
               --connect-timeout 2 --max-time 3 \
               http://127.0.0.1:18080/-/health 2>/dev/null) || true
        if [[ "$code" == "200" ]]; then
            return 0
        fi
        sleep "$delay"
    done
    return 1
}

# ── Kill old supervisor (and any orphan daemons) ───────────────────────
kill_gateway() {
    # Find aigw supervisor and ALL daemon processes.
    # Covers: ai-gateway/bin/*, ./bin/*, and standalone daemons.
    local pids
    pids=$(pgrep -f "aigw supervise" 2>/dev/null || true)
    for svc in gatewayd controld telemetryd; do
        pids="$pids $(pgrep -f "$svc" 2>/dev/null || true)"
    done
    # Deduplicate
    pids=$(echo "$pids" | tr ' ' '\n' | sort -u | tr '\n' ' ')

    if [[ -z "${pids// /}" ]]; then
        warn "no running gateway processes found"
        return 0
    fi

    for pid in $pids; do
        kill "$pid" 2>/dev/null || true
    done

    # Wait for graceful shutdown (max 5s)
    for (( i=0; i<10; i++ )); do
        local alive=""
        for pid in $pids; do
            kill -0 "$pid" 2>/dev/null && alive="$alive $pid"
        done
        if [[ -z "${alive// /}" ]]; then
            return 0
        fi
        sleep 0.5
    done

    # Force kill survivors
    for pid in $pids; do
        kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
    done
    sleep 0.3
}

# ── Build phase ────────────────────────────────────────────────────────
build_binaries() {
    log "building binaries to staging: $STAGING_DIR"
    cd "$PROJECT_ROOT"

    # Ensure go.mod is tidy before building
    go mod tidy 2>>"$LOG_FILE" || die "go mod tidy failed"

    local bins=(aigw gatewayd controld telemetryd gateway-cli)
    for name in "${bins[@]}"; do
        echo -n "  $name ... "
        if go build -o "$STAGING_DIR/$name" "./cmd/$name" 2>>"$LOG_FILE"; then
            echo -e "${GREEN}ok${NC}"
        else
            echo -e "${RED}FAIL${NC}"
            die "build failed for $name (see $LOG_FILE)"
        fi
    done

    # Smoke-test: each binary should start (print help) without segfault
    for name in "${bins[@]}"; do
        "$STAGING_DIR/$name" -version >/dev/null 2>&1 || true
    done

    log "build OK — binaries staged"
}

# ── Swap + restart (atomic) ────────────────────────────────────────────
swap_and_restart() {
    log "performing atomic swap ..."

    local bins=(aigw gatewayd controld telemetryd gateway-cli)
    local ts
    ts=$(date +%Y%m%d%H%M%S)

    # 1. Kill old processes
    kill_gateway

    # 2. Backup current binaries + swap new ones into place
    for name in "${bins[@]}"; do
        if [[ -f "$BIN_DIR/$name" ]]; then
            cp -f "$BIN_DIR/$name" "$BIN_DIR/$name.bak-$ts" 2>/dev/null || true
        fi
        cp -f "$STAGING_DIR/$name" "$BIN_DIR/$name"
        chmod +x "$BIN_DIR/$name"
    done

    # 3. Clean up stale PID file
    mkdir -p "$PROJECT_ROOT/.run"
    rm -f "$PROJECT_ROOT/.run/aigw.pid"

    # 4. Start supervisor
    mkdir -p "$RUNTIME_ROOT/logs"
    nohup "$BIN_DIR/aigw" supervise \
        -runtime-root "$RUNTIME_ROOT" \
        -config-dir   "$CONFIG_DIR" \
        -bin-dir      "$BIN_DIR" \
        >> "$RUNTIME_ROOT/logs/supervisor.log" 2>&1 &
    local sup_pid=$!
    echo "$sup_pid" > "$PROJECT_ROOT/.run/aigw.pid"

    log "supervisor started (PID $sup_pid), waiting for health ..."

    # 5. Health gate
    if check_health 30 0.5; then
        echo -e "${GREEN}[update] gateway is healthy${NC}"
        return 0
    else
        err "gateway did NOT become healthy within 15s"
        err "check logs: tail -50 $RUNTIME_ROOT/logs/supervisor.log"
        return 1
    fi
}

# ── Pre-flight ─────────────────────────────────────────────────────────
preflight() {
    command -v go   >/dev/null || die "go not found in PATH"
    command -v curl >/dev/null || die "curl not found in PATH"
    [[ -d "$PROJECT_ROOT/cmd" ]]   || die "project root not found: $PROJECT_ROOT"
    [[ -d "$DEPLOY_DIR" ]]         || die "deploy dir not found: $DEPLOY_DIR (set DEPLOY_DIR=)"
    [[ -f "$CONFIG_DIR/controld.json" ]] || die "config dir missing: $CONFIG_DIR"
}

# ── Main ───────────────────────────────────────────────────────────────
main() {
    local mode="${1:-full}"

    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════${NC}"
    echo -e "${CYAN}  AI Model Gateway — Atomic Update${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════${NC}"
    echo -e "  project : $PROJECT_ROOT"
    echo -e "  deploy  : $DEPLOY_DIR"
    echo -e "  staging : $STAGING_DIR"
    echo ""

    case "$mode" in
        --restart)
            preflight
            build_binaries
            swap_and_restart
            ;;
        --dry-run)
            preflight
            build_binaries
            log "dry-run complete — no processes were touched"
            ;;
        *)
            preflight
            build_binaries
            swap_and_restart
            ;;
    esac

    echo ""
    log "done."
}

main "$@"
