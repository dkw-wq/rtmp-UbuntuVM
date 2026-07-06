# Monitoring

This directory contains the monitoring stack for the live streaming server.

Public entrypoint:

```text
https://jfznbx.cn/monitor/
```

Internal components:

- Go live metrics: `127.0.0.1:9091/metrics`
- node_exporter: `127.0.0.1:9100/metrics`
- HLS monitor exporter: `127.0.0.1:9093/metrics`
- Prometheus: `127.0.0.1:9092`
- Grafana: `127.0.0.1:3000`, proxied by Nginx at `/monitor/`

## Install

From the deployed repository root on the server:

```bash
cd /opt/rtmp-UbuntuVM
sudo bash monitoring/install-monitoring.sh
```

Then add the Nginx snippet in `monitoring/nginx/monitor.conf` to the HTTPS
`server {}` block for `jfznbx.cn`, create a Basic Auth password file, and reload
Nginx:

```bash
sudo apt-get install -y apache2-utils
sudo htpasswd -c /etc/nginx/.htpasswd-monitor admin
sudo nginx -t
sudo systemctl reload nginx
```

Grafana credentials are managed by Grafana. Set the admin password on first
login or with:

```bash
sudo grafana-cli admin reset-admin-password '<new-password>'
```

## Services

```bash
sudo systemctl status prometheus-live
sudo systemctl status hls-monitor
sudo systemctl status node-exporter-live
sudo systemctl status grafana-server
```

## Validate

```bash
curl http://127.0.0.1:9091/metrics
curl http://127.0.0.1:9100/metrics
curl http://127.0.0.1:9093/metrics
curl http://127.0.0.1:9092/-/ready
curl http://127.0.0.1:3000/api/health
```

## Alerts

Prometheus loads rules from `monitoring/prometheus/alerts.yml`.
Alertmanager delivery is intentionally not enabled here; wire it later to email,
WeCom, Telegram, or another notification target when the desired channel is
chosen.
