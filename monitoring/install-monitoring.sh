#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_ROOT="${TARGET_ROOT:-/opt/rtmp-UbuntuVM}"

log() { echo "[monitoring] $*"; }
die() { echo "[monitoring] fatal: $*" >&2; exit 1; }

if [ "$(id -u)" -ne 0 ]; then
    die "run as root: sudo bash monitoring/install-monitoring.sh"
fi

if [ "$ROOT_DIR" != "$TARGET_ROOT" ]; then
    log "repository is at $ROOT_DIR; expected deploy path is $TARGET_ROOT"
    log "copying monitoring files into $TARGET_ROOT/monitoring"
    mkdir -p "$TARGET_ROOT"
    mkdir -p "$TARGET_ROOT/monitoring"
    cp -a "$ROOT_DIR/monitoring/." "$TARGET_ROOT/monitoring/"
fi

log "installing packages"
apt-get update
apt-get install -y prometheus nginx apache2-utils

if ! command -v grafana-server >/dev/null 2>&1; then
    log "grafana-server not found; trying apt package grafana"
    apt-get install -y grafana || true
fi
if ! command -v grafana-server >/dev/null 2>&1; then
    die "grafana-server is not installed. Install Grafana first, then rerun this script."
fi

log "building hls-monitor"
cd "$TARGET_ROOT/go-live-server"
go build -o hls-monitor ./cmd/hls-monitor

log "installing Grafana provisioning"
mkdir -p /etc/grafana/provisioning/datasources
mkdir -p /etc/grafana/provisioning/dashboards
cp "$TARGET_ROOT/monitoring/grafana/provisioning/datasources/prometheus.yml" /etc/grafana/provisioning/datasources/prometheus.yml
cp "$TARGET_ROOT/monitoring/grafana/provisioning/dashboards/dashboards.yml" /etc/grafana/provisioning/dashboards/dashboards.yml
cp "$TARGET_ROOT/monitoring/grafana/grafana.ini" /etc/grafana/grafana.ini

log "installing systemd services"
cp "$TARGET_ROOT/monitoring/systemd/prometheus-live.service" /etc/systemd/system/prometheus-live.service
cp "$TARGET_ROOT/monitoring/systemd/hls-monitor.service" /etc/systemd/system/hls-monitor.service
cp "$TARGET_ROOT/monitoring/systemd/node-exporter-live.service" /etc/systemd/system/node-exporter-live.service

systemctl daemon-reload
systemctl disable --now prometheus 2>/dev/null || true
systemctl enable --now prometheus-live
systemctl enable --now hls-monitor
systemctl enable --now node-exporter-live
systemctl enable --now grafana-server

log "installed. Add monitoring/nginx/monitor.conf to the jfznbx.cn HTTPS server block, then reload nginx."
log "open https://jfznbx.cn/monitor/ after Nginx is configured."
