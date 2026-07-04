# CLAUDE.md — RTMP Live Streaming System (Ubuntu VM)

> Project specification reference. See `PROJECT_SPECIFICATION.md` for full details.

## Overview

Live streaming server running on Ubuntu 22.04 VM (4 vCPU / 4 GB RAM). Receives direct RTMP pushes from Raspberry Pi devices via FFmpeg using server-generated stream keys and publish tokens, serves HLS/FLV to viewers via SRS 5.0. Go management server provides REST API for stream lifecycle and SRS callback handling.

## Architecture

```
Raspberry Pi (FFmpeg) ──RTMP push──> SRS 5.0 :1935
                                         │
                                    HTTP callbacks (on_publish / on_unpublish)
                                         │
                                         ▼
Viewer <──HLS/FLV── SRS :8080 <──── Go API Server :9090 ────> PostgreSQL :5432
                                                     └─────────> SRS API :1985
```

## Directory Structure

```
rtmp-UbuntuVM/
├── CLAUDE.md                  # This file
├── PROJECT_SPECIFICATION.md   # Full spec with JSON schemas
├── minimal.conf               # SRS 5.0 configuration
├── srs.service                # systemd unit for production SRS install
├── srs-build/                 # SRS 5.0 source build tree
│   └── trunk/
│       ├── configure, Makefile
│       └── objs/srs           # Built SRS binary
└── go-live-server/            # Go management server
    ├── config.yaml
    ├── go.mod, go.sum
    ├── start.sh               # Dev launcher (builds + starts SRS + Go)
    ├── server                 # Compiled binary (ELF x86-64)
    ├── cmd/server/main.go     # Entry point
    ├── internal/
    │   ├── config/config.go   # YAML config loading
    │   ├── model/stream.go    # GORM models + status constants
    │   ├── store/db.go        # PostgreSQL via GORM v2
    │   ├── adapter/srs.go     # SRS HTTP API client (:1985)
    │   ├── adapter/agent.go   # Agent push-notify client (future use)
    │   ├── service/stream.go  # Business logic
    │   └── handler/
    │       ├── response.go    # Unified {code, msg, data} envelope
    │       ├── health.go      # GET /api/health
    │       ├── stream.go      # Stream CRUD + start/stop
    │       ├── callback.go    # SRS on_publish / on_unpublish
    │       └── agent.go       # Agent task polling + heartbeat
    └── migrations/001_init.sql
```

## Port Map

| Port | Service    | Access       | Purpose                        |
|------|------------|--------------|--------------------------------|
| 1935 | SRS        | LAN          | RTMP ingest                    |
| 8080 | SRS        | LAN          | HLS/FLV direct serving         |
| 1985 | SRS        | localhost    | SRS management HTTP API        |
| 9090 | Go Server  | localhost    | REST API + SRS callbacks       |
| 9091 | Go Server  | monitoring   | Prometheus metrics (planned)   |
| 80   | Nginx      | LAN/WAN      | Reverse proxy (planned)        |
| 5432 | PostgreSQL | localhost    | Database                       |
| 6379 | Redis      | localhost    | Cache (planned)                |

## Tech Stack

- **Go 1.25** with Gin, GORM v2, PostgreSQL driver, google/uuid, yaml.v3
- **SRS 5.0** compiled from source (branch `5.0release`)
- **PostgreSQL** with GORM auto-migration (manual SQL in migrations/ for reference)
- **Redis 7** (planned Step 2)
- **Nginx** (planned Step 1)

## How to Build & Run

```bash
# Development (one command):
cd go-live-server && bash start.sh

# Manual:
cd go-live-server && go build -o server ./cmd/server
cd srs-build/trunk && ./objs/srs -c ../../minimal.conf
cd go-live-server && ./server
```

## Configuration

`go-live-server/config.yaml`:
```yaml
server:  {port: 9090, mode: debug}
srs:     {api_url: "http://localhost:1985", rtmp_base_url: "rtmp://localhost:1935/live"}
database:{host: localhost, port: 5432, user: live, password: live_password, dbname: live, sslmode: disable, timezone: Asia/Shanghai}
nginx:   {hls_base_url: "http://localhost:8080/live"}
```

Override with env `CONFIG_PATH`. DSN built as: `host=X port=Y user=Z password=W dbname=V sslmode=disable TimeZone=Asia/Shanghai`

## Data Models (GORM)

**Stream** — status flow: `created` → `publishing` → `ended` / `error`
- UUID PK, stream_key (unique UUID v4), push_token, channel_id (FK), protocol, resolution, bitrate, status
- push_url, hls_url, flv_url, webrtc_url (auto-generated from base URLs)
- started_at (set by on_publish), ended_at (set by on_unpublish)

**Channel** — status: `inactive` / `active`

**Recording** — FK → streams (CASCADE), file_path, file_size, duration_sec

**AgentTask** — legacy compatibility model for older polling-based Pi agents.

## API Reference (MVP)

All responses: `{"code":0, "msg":"ok", "data":...}` or `{"code":N, "msg":"error"}`

| Method | Path                      | Description                    |
|--------|---------------------------|--------------------------------|
| GET    | /api/health               | Health check                   |
| POST   | /api/streams              | Create stream (body: channel_id?, protocol?, resolution?, bitrate?) |
| GET    | /api/streams              | List streams (?status=filter)  |
| GET    | /api/streams/:id          | Get stream detail              |
| DELETE | /api/streams/:id          | Delete (kicks publisher if active) |
| POST   | /api/streams/:id/start    | Return direct push URL for compatibility |
| POST   | /api/streams/:id/stop     | Stop streaming by kicking publisher |
| POST   | /api/callback/publish     | SRS on_publish hook            |
| POST   | /api/callback/unpublish   | SRS on_unpublish hook          |
| GET    | /api/agent/tasks          | Get pending tasks (?agent_id=) |
| PUT    | /api/agent/tasks/:id      | Update task status             |
| POST   | /api/agent/heartbeat      | Agent heartbeat                |

## SRS Callback Contract

SRS POSTs to `/api/callback` with:
```json
{"action":"on_publish","stream":"<stream_key>","client_id":"...","ip":"...","vhost":"__defaultVhost__","app":"live","param":""}
```

Go server MUST always return HTTP 200 `{"code":0}` (non-200 causes SRS to reject/retry).

## Stream URL Formats

- **RTMP push:** `{rtmp_base_url}/{stream_key}` → `rtmp://localhost:1935/live/<uuid>`
- **HLS play:** `{hls_base_url}/{stream_key}.m3u8` → `http://localhost:8080/live/<uuid>.m3u8`
- **FLV play:** `{hls_base_url}/{stream_key}.flv` → `http://localhost:8080/live/<uuid>.flv`

## Error Codes

| Code | HTTP | Meaning           |
|------|------|-------------------|
| 0    | 200  | Success           |
| 1001 | 400  | Invalid parameter |
| 1002 | 404  | Not found         |
| 1003 | 500  | Internal error    |
| 1004 | 409  | Conflict          |

## Coding Conventions

- **Response helpers:** Use `handler.Success(c, data)` and `handler.Error(c, httpStatus, code, msg)` — never write raw `c.JSON` for API responses
- **Error wrapping:** `fmt.Errorf("context: %w", err)`
- **Logging:** `log.Printf("[component] message")` with component tag `[service]` / `[handler]`
- **DB status updates:** Use `db.UpdateStreamStatus(id, status, map[string]interface{}{...})` for partial updates, never direct GORM Save
- **SRS adapter:** `adapter.SRSAPI` wraps `http://localhost:1985` — use `FindPublishingClient(key)` and `KickClient(id)`
- **Business logic:** Lives in `service/` layer, handlers are thin wrappers
- **Config:** All URLs constructed from `config.yaml` base URLs, never hardcoded in logic

## Testing Quick Start

```bash
# 1. Create stream
STREAM=$(curl -s -X POST localhost:9090/api/streams -H 'Content-Type: application/json' -d '{"protocol":"rtmp","resolution":"1920x1080","bitrate":"2000k"}')
STREAM_ID=$(echo $STREAM | jq -r '.data.id')
STREAM_KEY=$(echo $STREAM | jq -r '.data.stream_key')
PUSH_URL=$(echo $STREAM | jq -r '.data.push_url')

# 2. Push test pattern
ffmpeg -re -f lavfi -i testsrc=size=1920x1080:rate=30 -f lavfi -i sine=frequency=1000 \
  -c:v libx264 -preset ultrafast -b:v 2000k -c:a aac -b:a 128k -f flv "$PUSH_URL"

# 3. Play
ffplay "http://localhost:8080/live/$STREAM_KEY.m3u8"

# 4. Verify status
curl -s localhost:9090/api/streams/$STREAM_ID | jq '.data.status'  # → "publishing"

# 5. Cleanup
curl -s -X DELETE localhost:9090/api/streams/$STREAM_ID
```

## Future Steps (see PROJECT_SPECIFICATION.md §11)

- **Step 2:** JWT publish auth, HMAC play auth, Redis cache, Prometheus metrics
- **Step 3:** DVR recording, admin login, rate limiting, graceful shutdown
- **Step 4:** Kernel tuning, load testing (500 concurrent HLS)
- **Step 5:** Docker Compose, Swagger docs, ops manual
