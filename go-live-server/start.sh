#!/usr/bin/env bash
set -euo pipefail

# ── Config ──────────────────────────────────────────
BASE_DIR="$(cd "$(dirname "$0")" && pwd)"
SRS_DIR="$BASE_DIR/../srs-build/trunk"
SRS_BIN="$SRS_DIR/objs/srs"
SRS_CONF="$BASE_DIR/../minimal.conf"
SRS_HTML="$SRS_DIR/objs/nginx/html"
REDIS_BIN="$BASE_DIR/../redis-bin/redis-7.2.5/src/redis-server"
NODE_EXPORTER_BIN="$BASE_DIR/../node_exporter/node_exporter"
GO_BIN="$BASE_DIR/server"
GO_BIN_SRC="./cmd/server"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[start]${NC} $*"; }
warn() { echo -e "${YELLOW}[warn]${NC}  $*"; }
die()  { echo -e "${RED}[fatal]${NC} $*"; exit 1; }

# ── 1. Unset proxy for localhost ────────────────────
log "Unsetting proxy variables ..."
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
export no_proxy="localhost,127.0.0.1,::1"

# ── 2. Ensure directories exist ─────────────────────
log "Creating directories ..."
mkdir -p "$SRS_HTML"
mkdir -p /dev/shm/hls/live

# ── 3. Build Go server if needed ────────────────────
cd "$BASE_DIR"
if [ ! -f "$GO_BIN" ] || [ "$GO_BIN_SRC" -nt "$GO_BIN" ]; then
    log "Building Go server ..."
    go build -o "$GO_BIN" ./cmd/server
fi

# ── 4. Check SRS binary ─────────────────────────────
if [ ! -f "$SRS_BIN" ]; then
    die "SRS binary not found at $SRS_BIN"
    echo "  Run: cd $SRS_DIR && ./configure && make -j\$(nproc)"
fi

# ── 5. Kill stale processes on needed ports ─────────
log "Killing any old instances ..."
# kill leftover processes by name (no-op if none)
pkill -f "objs/srs" 2>/dev/null || true
pkill -f "go-live-server/server" 2>/dev/null || true
pkill -f "redis-server" 2>/dev/null || true
sleep 1

log "Releasing ports (1935 8082 1985 9090 9091 9100) ..."
for port in 1935 8082 1985 9090 9091 9100; do
    if ss -lntp 2>/dev/null | grep -q ":$port "; then
        warn "  Port $port in use — killing stale process ..."
        # try user-owned first, then escalate to sudo
        fuser -k "$port/tcp" 2>/dev/null && sleep 0.5 || {
            warn "  Port $port needs sudo — password prompt may appear ..."
            sudo fuser -k "$port/tcp" 2>/dev/null || true
            sleep 0.5
        }
        if ss -lntp 2>/dev/null | grep -q ":$port "; then
            die "  Port $port still in use — manual cleanup needed"
        fi
    fi
done
log "All ports free"

# ── 6. Start Redis ──────────────────────────────────
REDIS_PID=""
if [ -x "$REDIS_BIN" ]; then
    log "Starting Redis (:6379) ..."
    "$REDIS_BIN" --daemonize yes --bind 127.0.0.1 --port 6379 --loglevel notice
    sleep 1
    if ss -lntp 2>/dev/null | grep -q ":6379 "; then
        log "  Redis :6379 — OK"
    else
        warn "  Redis :6379 — failed to start"
    fi
else
    warn "Redis binary not found at $REDIS_BIN — skipping"
fi

# ── 7. Start node_exporter ──────────────────────────
if [ -x "$NODE_EXPORTER_BIN" ]; then
    log "Starting node_exporter (:9100) ..."
    pkill -f "node_exporter" 2>/dev/null || true
    sleep 0.5
    nohup "$NODE_EXPORTER_BIN" --web.listen-address=127.0.0.1:9100 > /tmp/node_exporter.log 2>&1 &
    sleep 1
    if ss -lntp 2>/dev/null | grep -q ":9100 "; then
        log "  node_exporter :9100 — OK"
    else
        warn "  node_exporter :9100 — failed to start"
    fi
else
    warn "node_exporter binary not found at $NODE_EXPORTER_BIN — skipping"
fi

# ── 8. Start SRS ────────────────────────────────────
log "Starting SRS (RTMP :1935, HTTP :8082, API :1985) ..."
cd "$SRS_DIR"
"$SRS_BIN" -c "$SRS_CONF" &
SRS_PID=$!
sleep 2

# Check SRS didn't die immediately
if ! kill -0 $SRS_PID 2>/dev/null; then
    die "SRS failed to start — check log: tail -30 $SRS_DIR/objs/srs.log"
fi

for port in 1935 8082 1985; do
    if ss -lntp 2>/dev/null | grep -q ":$port "; then
        log "  SRS :$port — OK"
    else
        warn "  SRS :$port — not listening"
    fi
done

# ── 9. Start Go server ──────────────────────────────
log "Starting Go API (:9090) ..."
cd "$BASE_DIR"
"$GO_BIN" &
GO_PID=$!
sleep 1

if ss -lntp 2>/dev/null | grep -q ":9090 "; then
    log "  Go API :9090 — OK"
else
    warn "  Go API :9090 — not listening (check above)"
fi

# ── 10. Quick health checks ─────────────────────────
log "Health checks ..."
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY

for port in 9090 8082 1985; do
    if curl -s --noproxy '*' "http://127.0.0.1:$port/" >/dev/null 2>&1; then
        log "  :$port — reachable"
    else
        warn "  :$port — unreachable (check proxy: env | grep -i proxy)"
    fi
done

# ── 11. Summary ─────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  All services started${NC}"
echo ""
echo -e "  SRS RTMP:       rtmp://127.0.0.1:1935/live"
echo -e "  HLS / FLV:      http://127.0.0.1:8082/live"
echo -e "  SRS API:        http://127.0.0.1:1985"
echo -e "  Go Management:  http://127.0.0.1:9090"
echo ""
echo -e "  Ctrl+C to stop all services"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# ── 12. Wait for Ctrl+C, then cleanup ──────────────
trap 'log "Stopping ..."; kill $SRS_PID $GO_PID 2>/dev/null; pkill -f "redis-server" 2>/dev/null; wait 2>/dev/null; log "Done."' EXIT INT TERM
wait
