#!/usr/bin/env bash
# Build the admin UI and Linux daemons from this repo, regenerate
# aigw-manifest.json, copy the verified bundle to ~/ai-gateway, and restart
# ai-gateway.service.
# Run inside WSL:  bash scripts/sync-bundle-to-home-ai-gateway.sh
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
INSTALL_DIR="${INSTALL_DIR:-$HOME/ai-gateway}"
DIST_DIR="$PROJECT_DIR/dist"
ADMIN_DIST="$PROJECT_DIR/web/admin/dist"
EMBEDDED_ADMIN_DIST="$PROJECT_DIR/internal/control/api/embedded_admin_dist"
PRODUCT_VERSION="$(tr -d '\r\n' < "$PROJECT_DIR/VERSION")"
GIT_COMMIT="$(git -C "$PROJECT_DIR" rev-parse HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X ai-model-gateway/internal/version.ProductVersion=${PRODUCT_VERSION} -X ai-model-gateway/internal/version.BuildCommit=${GIT_COMMIT} -X ai-model-gateway/internal/version.BuildDate=${BUILD_DATE}"

echo "Project: $PROJECT_DIR"
echo "Install: $INSTALL_DIR"
echo "Version: $PRODUCT_VERSION"
echo "Commit:  $GIT_COMMIT"
cd "$PROJECT_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "error: go is not in PATH" >&2
  exit 1
fi
if ! command -v npm >/dev/null 2>&1; then
  echo "error: npm is not in PATH" >&2
  exit 1
fi

echo "=== admin frontend build ==="
npm --prefix "$PROJECT_DIR/web/admin" run build

echo "=== sync embedded admin assets ==="
rm -rf "$EMBEDDED_ADMIN_DIST"
mkdir -p "$EMBEDDED_ADMIN_DIST"
cp -a "$ADMIN_DIST/." "$EMBEDDED_ADMIN_DIST/"

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR/bin"

echo "=== go build (trimpath, like CI) ==="
for bin in aigw gatewayd controld telemetryd gateway-cli; do
  go build -trimpath -ldflags="$LDFLAGS" -o "$DIST_DIR/bin/${bin}" "./cmd/${bin}"
done

echo "=== stage admin frontend dist ==="
mkdir -p "$DIST_DIR/web/admin"
cp -a "$ADMIN_DIST" "$DIST_DIR/web/admin/dist"

echo "=== bundle build + verify (root=dist, binaries in dist/bin) ==="
"$DIST_DIR/bin/aigw" bundle build -root dist -out dist/aigw-manifest.json
"$DIST_DIR/bin/aigw" bundle verify -root dist -manifest dist/aigw-manifest.json

echo "=== copy to $INSTALL_DIR ==="
mkdir -p "$INSTALL_DIR/bin"
echo "=== stop user service before replacing running binaries ==="
service_was_active=0
if systemctl --user is-active --quiet ai-gateway.service; then
  service_was_active=1
  systemctl --user stop ai-gateway.service
fi
restart_service() {
  if [[ "$service_was_active" -eq 1 ]]; then
    systemctl --user start ai-gateway.service || true
  fi
}
trap restart_service EXIT
cp -a "$DIST_DIR/bin/"* "$INSTALL_DIR/bin/"
chmod +x "$INSTALL_DIR/bin/"*
cp -a "$DIST_DIR/aigw-manifest.json" "$INSTALL_DIR/aigw-manifest.json"
if [[ -d "$DIST_DIR/web/admin/dist" ]]; then
  mkdir -p "$INSTALL_DIR/web/admin"
  rm -rf "$INSTALL_DIR/web/admin/dist"
  cp -a "$DIST_DIR/web/admin/dist" "$INSTALL_DIR/web/admin/dist"
fi
"$INSTALL_DIR/bin/aigw" bundle verify -root "$INSTALL_DIR" -manifest "$INSTALL_DIR/aigw-manifest.json"
trap - EXIT

echo "=== restart user service ==="
systemctl --user start ai-gateway.service
sleep 2

systemctl --user --no-pager --full status ai-gateway.service || true
echo ""
echo "=== listeners ==="
ss -tlnp 2>/dev/null | grep -E ':18080|:18081' || echo "(none — check logs)"
echo ""
echo "=== health ==="
curl -sS --max-time 5 "http://127.0.0.1:18080/-/health" && echo "" || echo "curl 18080 failed"
echo "Done."
