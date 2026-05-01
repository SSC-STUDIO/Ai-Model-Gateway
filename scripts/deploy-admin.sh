#!/bin/bash
set -euo pipefail

PROJECT_DIR="/home/chenrunsen/workspace/My-Project/Active/Software/AI-Model-Gateway"
CONFIG="configs/test-controld.json"
RUNTIME_ROOT="/tmp/aigw-18086-runtime"
LIVE_TELEMETRY_DIR="$PROJECT_DIR/.gateway-runtime/telemetry-live-18080"
TELEMETRY_DATA_DIR="$RUNTIME_ROOT/telemetry"
CONTROL_LOG="/tmp/controld-deploy.log"
TELEMETRY_LOG="/tmp/telemetryd-18086.log"
GATEWAY_SOCKET="/run/user/1000/test-gateway-control.sock"
TELEMETRY_INGEST_SOCKET="/run/user/1000/test-telemetry-ingest.sock"
TELEMETRY_QUERY_SOCKET="/run/user/1000/test-telemetry-query.sock"

echo "=== Step 1: Build frontend ==="
cd "$PROJECT_DIR/web/admin"
npm run build

echo "=== Step 2: Clean old embedded assets ==="
rm -rf "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets"
mkdir -p "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets"

echo "=== Step 3: Copy new assets ==="
cp "$PROJECT_DIR/web/admin/dist/assets/"* "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets/"
cp "$PROJECT_DIR/web/admin/dist/index.html" "$PROJECT_DIR/internal/control/api/embedded_admin_dist/"

echo "=== Step 4: Verify filenames match ==="
JS_FILE=$(grep -oP 'src="/admin/assets/[^"]+"' "$PROJECT_DIR/internal/control/api/embedded_admin_dist/index.html" | head -1 | sed 's/.*\/assets\///;s/"//')
echo "index.html references: $JS_FILE"
ls "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets/$JS_FILE" && echo "OK: File exists" || { echo "ERROR: File not found!"; exit 1; }

echo "=== Step 5: Stop old dev/test daemons ==="
pkill -f "[c]ontrold.*configs/test-controld.json" 2>/dev/null || true
pkill -f "[t]elemetryd.*configs/test-telemetryd.json" 2>/dev/null || true
pkill -f "[t]elemetryd.*test-telemetry-ingest.sock" 2>/dev/null || true
pkill -f "[g]atewayd.*configs/test-gatewayd.json" 2>/dev/null || true
sleep 1
rm -f "$GATEWAY_SOCKET" "$TELEMETRY_INGEST_SOCKET" "$TELEMETRY_QUERY_SOCKET"
rm -rf "$RUNTIME_ROOT"
mkdir -p "$RUNTIME_ROOT/control" "$RUNTIME_ROOT/telemetry"
if [ -f "$LIVE_TELEMETRY_DIR/events.db" ] && [ -f "$LIVE_TELEMETRY_DIR/query.db" ]; then
  TELEMETRY_DATA_DIR="$LIVE_TELEMETRY_DIR"
  echo "Using live 18080 telemetry snapshot: $TELEMETRY_DATA_DIR"
fi
cat > "$RUNTIME_ROOT/config.yaml" <<'YAML'
server:
  listen: "127.0.0.1:0"
providers:
  - name: local-diagnostic
    base_url: http://127.0.0.1:9
    api_key: test-key
    models:
      - gpt-4
    enabled: true
YAML

echo "=== Step 6: Rebuild runtime binaries ==="
cd "$PROJECT_DIR"
go build -o bin/telemetryd ./cmd/telemetryd
go build -o bin/controld ./cmd/controld

echo "=== Step 7: Start telemetryd and controld ==="
export ADMIN_BOOTSTRAP_TOKEN=test-bootstrap-123
export COOKIE_SIGNING_KEY=test-cookie-key-32bytes-long-xxx
export ADMIN_TOKEN=admin-test-token
export VIEWER_TOKEN=viewer-test-token
setsid -f ./bin/telemetryd \
  -ingest "$TELEMETRY_INGEST_SOCKET" \
  -query "$TELEMETRY_QUERY_SOCKET" \
  -data-dir "$TELEMETRY_DATA_DIR" \
  > "$TELEMETRY_LOG" 2>&1
sleep 1
TELEMETRY_PID=$(pgrep -f "[b]in/telemetryd.*$TELEMETRY_INGEST_SOCKET" | head -1 || true)
echo "telemetryd PID: ${TELEMETRY_PID:-unknown}"
setsid -f ./bin/controld \
  -config "$CONFIG" \
  -gateway "$GATEWAY_SOCKET" \
  -telemetry "$TELEMETRY_QUERY_SOCKET" \
  -data-dir "$RUNTIME_ROOT/control" \
  -authoring-config "$RUNTIME_ROOT/config.yaml" \
  > "$CONTROL_LOG" 2>&1
sleep 1
CONTROL_PID=$(pgrep -f "[b]in/controld.*configs/test-controld.json" | head -1 || true)
echo "controld PID: ${CONTROL_PID:-unknown}"

echo "=== Step 8: Verify ==="
for _ in $(seq 1 30); do
  CODE=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:18086/admin/ || true)
  if [ "$CODE" = "200" ]; then
    echo "Admin UI: 200"
    echo "Done! Open http://127.0.0.1:18086/admin/"
    exit 0
  fi
  sleep 1
done

echo "Admin UI: ${CODE:-000}"
echo "controld log:"
tail -n 80 "$CONTROL_LOG" || true
echo "telemetryd log:"
tail -n 80 "$TELEMETRY_LOG" || true
exit 1
