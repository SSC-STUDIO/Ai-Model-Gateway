#!/bin/bash
#
# AI Model Gateway - Unified Startup Script
#
# Usage:
#   ./start.sh [command] [options]
#
# Commands:
#   start       Start unified aigw supervisor
#   stop        Stop unified aigw supervisor
#   restart     Restart unified aigw supervisor
#   status      Show service status
#   build       Build binaries
#   logs        Show logs (follow mode)
#   health      Run health check
#
# Environment:
#   AUTHORING_CONFIG   Path to operator authoring YAML (default: ./configs/config.yaml)
#   BIN_DIR        Path to binary directory (default: ./bin)
#   LOG_DIR        Path to log directory (default: ./logs)

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
CONFIG_DIR="${CONFIG_DIR:-$PROJECT_ROOT/configs}"
RUNTIME_ROOT="${RUNTIME_ROOT:-$PROJECT_ROOT/.gateway-runtime}"
BIN_DIR="${BIN_DIR:-$PROJECT_ROOT/bin}"
LOG_DIR="${LOG_DIR:-$PROJECT_ROOT/logs}"
PID_DIR="$PROJECT_ROOT/.run"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_dependencies() {
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed"
        exit 1
    fi
}

ensure_directories() {
    mkdir -p \
        "$BIN_DIR" \
        "$LOG_DIR" \
        "$PID_DIR" \
        "$RUNTIME_ROOT/telemetry" \
        "$RUNTIME_ROOT/gateway" \
        "$RUNTIME_ROOT/control"
}

build_binaries() {
    log_info "Building binaries..."
    cd "$PROJECT_ROOT"

    for binary in aigw gatewayd controld telemetryd gateway-cli; do
        log_info "Building $binary..."
        go build -o "$BIN_DIR/$binary" "./cmd/$binary"
    done

    log_info "Build complete"
}

start_service() {
    local name=$1
    local pid_file="$PID_DIR/$name.pid"

    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            log_warn "$name is already running (PID: $pid)"
            return 0
        fi
    fi

    log_info "Starting $name..."

    local log_file="$LOG_DIR/$name.log"
    local args=()
    case "$name" in
        aigw)
            args=(supervise -runtime-root "$RUNTIME_ROOT" -config-dir "$CONFIG_DIR" -bin-dir "$BIN_DIR")
            ;;
        *)
            log_error "Unknown service: $name"
            return 1
            ;;
    esac

    (
        cd "$PROJECT_ROOT"
        "$BIN_DIR/$name" "${args[@]}"
    ) >> "$log_file" 2>&1 &
    local pid=$!
    echo $pid > "$pid_file"

    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
        log_info "$name started (PID: $pid)"
    else
        log_error "$name failed to start"
        return 1
    fi
}

stop_service() {
    local name=$1
    local pid_file="$PID_DIR/$name.pid"

    if [ ! -f "$pid_file" ]; then
        log_warn "$name is not running"
        return 0
    fi

    local pid=$(cat "$pid_file")

    if kill -0 "$pid" 2>/dev/null; then
        log_info "Stopping $name (PID: $pid)..."
        kill "$pid"

        # Wait for graceful shutdown
        for i in {1..10}; do
            if ! kill -0 "$pid" 2>/dev/null; then
                break
            fi
            sleep 1
        done

        # Force kill if still running
        if kill -0 "$pid" 2>/dev/null; then
            log_warn "Force killing $name..."
            kill -9 "$pid"
        fi

        rm -f "$pid_file"
        log_info "$name stopped"
    else
        log_warn "$name is not running (stale PID file)"
        rm -f "$pid_file"
    fi
}

show_status() {
    local name=$1
    local pid_file="$PID_DIR/$name.pid"

    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            echo -e "$name: ${GREEN}running${NC} (PID: $pid)"
            return 0
        fi
    fi
    echo -e "$name: ${RED}stopped${NC}"
    return 1
}

check_health() {
    log_info "Running health check..."

    # Check control plane
    if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:18081/api/admin/status | grep -q "200"; then
        echo -e "Control Plane: ${GREEN}healthy${NC}"
    else
        echo -e "Control Plane: ${RED}unhealthy${NC}"
    fi

    # Check data plane
    if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:18080/-/health | grep -q "200"; then
        echo -e "Data Plane:    ${GREEN}healthy${NC}"
    else
        echo -e "Data Plane:    ${RED}unhealthy${NC}"
    fi
}

show_logs() {
    local name=$1
    if [ -n "$name" ]; then
        tail -f "$LOG_DIR/$name.log"
    else
        tail -f "$LOG_DIR"/*.log
    fi
}

# Main command dispatcher
case "${1:-help}" in
    start)
        ensure_directories
        start_service aigw
        log_info "AI Model Gateway started"
        ;;
    stop)
        stop_service aigw
        log_info "AI Model Gateway stopped"
        ;;
    restart)
        $0 stop
        sleep 2
        $0 start
        ;;
    status)
        status_code=0
        show_status aigw || status_code=1
        exit "$status_code"
        ;;
    build)
        check_dependencies
        build_binaries
        ;;
    logs)
        show_logs "$2"
        ;;
    health)
        check_health
        ;;
    *)
        echo "AI Model Gateway - Startup Script"
        echo ""
        echo "Usage: $0 <command> [options]"
        echo ""
        echo "Commands:"
        echo "  start       Start aigw supervisor"
        echo "  stop        Stop aigw supervisor"
        echo "  restart     Restart aigw supervisor"
        echo "  status      Show service status"
        echo "  build       Build binaries"
        echo "  logs [svc]  Show logs (optional: service name)"
        echo "  health      Run health check"
        echo ""
        echo "Environment Variables:"
        echo "  CONFIG_DIR    Config directory (default: ./configs)"
        echo "  RUNTIME_ROOT  Runtime directory (default: ./.gateway-runtime)"
        echo "  BIN_DIR       Binary directory (default: ./bin)"
        echo "  LOG_DIR       Log directory (default: ./logs)"
        exit 1
        ;;
esac
