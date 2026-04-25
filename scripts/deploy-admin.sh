#!/bin/bash
set -e

PROJECT_DIR="/home/chenrunsen/workspace/My-Project/Active/Software/AI-Model-Gateway"
CONFIG="configs/test-controld.json"

echo "=== Step 1: Build frontend ==="
cd "$PROJECT_DIR/web/admin"
npm run build

echo "=== Step 2: Clean old embedded assets ==="
rm -f "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets/"*.js
rm -f "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets/"*.css

echo "=== Step 3: Copy new assets ==="
cp "$PROJECT_DIR/web/admin/dist/assets/"*.js "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets/"
cp "$PROJECT_DIR/web/admin/dist/assets/"*.css "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets/"
cp "$PROJECT_DIR/web/admin/dist/index.html" "$PROJECT_DIR/internal/control/api/embedded_admin_dist/"

echo "=== Step 4: Verify filenames match ==="
JS_FILE=$(grep -oP 'src="/admin/assets/[^"]+"' "$PROJECT_DIR/internal/control/api/embedded_admin_dist/index.html" | head -1 | sed 's/.*\/assets\///;s/"//')
echo "index.html references: $JS_FILE"
ls "$PROJECT_DIR/internal/control/api/embedded_admin_dist/assets/$JS_FILE" && echo "OK: File exists" || { echo "ERROR: File not found!"; exit 1; }

echo "=== Step 5: Kill old test controld ==="
pkill -f "controld.*test-controld" 2>/dev/null || true
sleep 1

echo "=== Step 6: Rebuild controld ==="
cd "$PROJECT_DIR"
go build -o bin/controld ./cmd/controld

echo "=== Step 7: Start controld ==="
export ADMIN_BOOTSTRAP_TOKEN=test-bootstrap-123
export COOKIE_SIGNING_KEY=test-cookie-key-32bytes-long-xxx
export ADMIN_TOKEN=admin-test-token
export VIEWER_TOKEN=viewer-test-token
nohup ./bin/controld -config "$CONFIG" > /tmp/controld-deploy.log 2>&1 &
echo "controld PID: $!"
sleep 2

echo "=== Step 8: Verify ==="
curl -s -o /dev/null -w "Admin UI: %{http_code}\n" http://127.0.0.1:18085/admin/
echo "Done! Open http://127.0.0.1:18085/admin/"
