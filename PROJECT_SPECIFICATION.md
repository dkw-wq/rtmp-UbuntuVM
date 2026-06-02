# RTMP Live Streaming System — Project Specification

> **Version:** 1.0 (MVP)
> **Last Updated:** 2026-06-02
> **Target:** Ubuntu 22.04 VM (4 vCPU / 4 GB RAM / 40 GB Disk)

---

## Table of Contents

1. [System Architecture](#1-system-architecture)
2. [Network Topology & Port Map](#2-network-topology--port-map)
3. [Data Models](#3-data-models)
4. [API Reference](#4-api-reference)
5. [Push/Pull Stream Protocols & URL Formats](#5-pushpull-stream-protocols--url-formats)
6. [SRS Callback Protocol](#6-srs-callback-protocol)
7. [Agent Communication Protocol](#7-agent-communication-protocol)
8. [Configuration Reference](#8-configuration-reference)
9. [Error Codes](#9-error-codes)
10. [Testing Guide](#10-testing-guide)
11. [Future Expansion Plan](#11-future-expansion-plan)

---

## 1. System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Ubuntu VM (this server)                     │
│                                                                   │
│  ┌──────────────┐    RTMP push     ┌──────────────────┐          │
│  │ Raspberry Pi │ ────────────────> │ SRS 5.0          │          │
│  │ (FFmpeg)     │    :1935/live/   │ :1935 :8080 :1985│          │
│  └──────────────┘                  └───────┬──────────┘          │
│                                            │                      │
│                              HTTP Callbacks│                      │
│                              (on_publish,  │                      │
│                               on_unpublish,│                      │
│                               on_play,     │                      │
│                               on_stop,     │                      │
│                               on_dvr)      │                      │
│                                            ▼                      │
│  ┌──────────────┐    HTTP GET     ┌──────────────────┐          │
│  │ Viewer       │ <───────────── │ Go API Server     │          │
│  │ (Browser/App)│   HLS/FLV      │ :9090             │          │
│  └──────────────┘     :8080      └───┬───────────────┘          │
│                                       │                           │
│                         ┌─────────────┼─────────────┐            │
│                         ▼             ▼             ▼            │
│                   ┌──────────┐ ┌──────────┐ ┌──────────┐        │
│                   │PostgreSQL│ │  Redis   │ │  Nginx   │        │
│                   │  :5432   │ │  :6379   │ │   :80    │        │
│                   │(persist) │ │ (cache)  │ │(reverse  │        │
│                   │          │ │          │ │ proxy)   │        │
│                   └──────────┘ └──────────┘ └──────────┘        │
└─────────────────────────────────────────────────────────────────┘
```

**Component Roles:**

| Component     | Port(s)          | Role                                                    |
|---------------|------------------|---------------------------------------------------------|
| SRS 5.0       | 1935, 8080, 1985 | RTMP ingestion, HLS/FLV serving, management API         |
| Go API Server | 9090, 9091       | Stream CRUD, SRS callback handler, agent task queue     |
| Nginx         | 80               | Reverse proxy for HLS/FLV, play auth via `auth_request` |
| PostgreSQL    | 5432             | Persistent storage (channels, streams, recordings)      |
| Redis         | 6379             | Status cache, viewer counters, token blacklist          |

---

## 2. Network Topology & Port Map

| Port  | Service    | Protocol | Description                           | External Access |
|-------|------------|----------|---------------------------------------|-----------------|
| 1935  | SRS        | RTMP     | RTMP ingest from Raspberry Pi agents  | Yes (LAN)       |
| 8080  | SRS        | HTTP     | Direct HLS/FLV serving (SRS built-in) | Yes (LAN)       |
| 1985  | SRS        | HTTP     | SRS HTTP management API               | No (localhost)  |
| 9090  | Go Server  | HTTP     | REST API + SRS callback endpoint      | No (localhost)  |
| 9091  | Go Server  | HTTP     | Prometheus `/metrics` endpoint        | Yes (monitoring)|
| 80    | Nginx      | HTTP     | Public HLS/FLV reverse proxy          | Yes (LAN/WAN)   |
| 5432  | PostgreSQL | TCP      | Database                              | No (localhost)  |
| 6379  | Redis      | TCP      | Cache                                 | No (localhost)  |

---

## 3. Data Models

### 3.1 Channel (频道)

```json
{
  "id":          "550e8400-e29b-41d4-a716-446655440000",
  "name":        "My Channel",
  "description": "A test channel",
  "status":      "active",
  "created_at":  "2026-06-02T10:00:00Z",
  "updated_at":  "2026-06-02T10:00:00Z"
}
```

| Field       | Type       | Description                    | Values                        |
|-------------|------------|--------------------------------|-------------------------------|
| id          | UUID       | Primary key                    | auto-generated                |
| name        | string(255)| Channel name                   | required                      |
| description | text       | Channel description            | optional                      |
| status      | string(20) | Channel status                 | `inactive`, `active`          |
| created_at  | timestamptz| Creation timestamp             | auto                          |
| updated_at  | timestamptz| Last update timestamp          | auto                          |

### 3.2 Stream (流)

```json
{
  "id":          "660e8400-e29b-41d4-a716-446655440001",
  "channel_id":  "550e8400-e29b-41d4-a716-446655440000",
  "stream_key":  "a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
  "protocol":    "rtmp",
  "resolution":  "1920x1080",
  "bitrate":     "2000k",
  "status":      "created",
  "push_url":    "rtmp://192.168.1.100:1935/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
  "hls_url":     "http://192.168.1.100:8080/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c.m3u8",
  "flv_url":     "http://192.168.1.100:8080/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c.flv",
  "webrtc_url":  null,
  "started_at":  null,
  "ended_at":    null,
  "created_at":  "2026-06-02T10:05:00Z"
}
```

| Field       | Type       | Description                    | Values                                |
|-------------|------------|--------------------------------|---------------------------------------|
| id          | UUID       | Primary key                    | auto-generated                        |
| channel_id  | UUID       | FK → channels.id               | optional                              |
| stream_key  | string(128)| Unique push key (UUID format)  | auto-generated                        |
| protocol    | string(20) | Push protocol                  | `rtmp`, `rtsp`, `srt`                |
| resolution  | string(20) | Video resolution               | e.g. `1920x1080`, `1280x720`         |
| bitrate     | string(10) | Video bitrate                  | e.g. `2000k`, `4000k`                |
| status      | string(20) | Stream lifecycle state         | `created` → `publishing` → `ended` / `error` |
| push_url    | text       | Full RTMP push URL             | auto-generated                        |
| hls_url     | text       | HLS playback URL (.m3u8)       | auto-generated                        |
| flv_url     | text       | FLV playback URL (.flv)        | auto-generated                        |
| webrtc_url  | text       | WebRTC playback URL            | reserved for future use               |
| started_at  | timestamptz| When publishing started        | set by on_publish callback            |
| ended_at    | timestamptz| When publishing ended          | set by on_unpublish callback          |
| created_at  | timestamptz| Creation timestamp             | auto                                  |

### 3.3 Stream Status State Machine

```
┌─────────┐    on_publish     ┌─────────────┐    on_unpublish    ┌───────┐
│ created │ ─────────────────>│ publishing   │ ─────────────────>│ ended │
└─────────┘                   └─────────────┘                    └───────┘
                                    │
                                    │ (error condition)
                                    ▼
                              ┌───────┐
                              │ error │
                              └───────┘
```

### 3.4 Recording (录制文件)

```json
{
  "id":           "770e8400-e29b-41d4-a716-446655440002",
  "stream_id":    "660e8400-e29b-41d4-a716-446655440001",
  "file_path":    "/data/recordings/a3f2b8c1/2026-06-02/a3f2b8c1_1717300000.mp4",
  "file_size":    52428800,
  "duration_sec": 600,
  "started_at":   "2026-06-02T10:10:00Z",
  "ended_at":     "2026-06-02T10:20:00Z",
  "created_at":   "2026-06-02T10:20:01Z"
}
```

| Field        | Type       | Description                   |
|--------------|------------|-------------------------------|
| id           | UUID       | Primary key                   |
| stream_id    | UUID       | FK → streams.id (CASCADE)     |
| file_path    | text       | Full path to recording file   |
| file_size    | bigint     | File size in bytes            |
| duration_sec | int        | Duration in seconds           |
| started_at   | timestamptz| Recording start time          |
| ended_at     | timestamptz| Recording end time            |
| created_at   | timestamptz| Record creation timestamp     |

### 3.5 AgentTask (Agent 任务)

```json
{
  "id":         "880e8400-e29b-41d4-a716-446655440003",
  "stream_id":  "660e8400-e29b-41d4-a716-446655440001",
  "agent_id":   "raspi-01",
  "action":     "start_push",
  "status":     "pending",
  "stream_key": "a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
  "push_url":   "rtmp://192.168.1.100:1935/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
  "error_msg":  null,
  "created_at": "2026-06-02T10:06:00Z",
  "updated_at": "2026-06-02T10:06:00Z"
}
```

| Field      | Type       | Description                                | Values                                  |
|------------|------------|--------------------------------------------|-----------------------------------------|
| id         | UUID       | Primary key                                | auto-generated                          |
| stream_id  | UUID       | FK → streams.id (CASCADE)                  | required                                |
| agent_id   | string(64) | Target agent identifier                    | optional (null = any agent can claim)   |
| action     | string(20) | Task action                                | `start_push`, `stop_push`               |
| status     | string(20) | Task lifecycle                             | `pending` → `running` → `completed` / `failed` |
| stream_key | string(128)| Stream key for FFmpeg push                 | required                                |
| push_url   | text       | Full RTMP push target URL                  | auto-generated                          |
| error_msg  | text       | Error message if status = `failed`         | optional                                |
| created_at | timestamptz| Creation timestamp                         | auto                                    |
| updated_at | timestamptz| Last update timestamp                      | auto                                    |

---

## 4. API Reference

### 4.1 Universal Response Envelope

All API responses follow this format:

**Success:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": { ... }
}
```

**Error:**
```json
{
  "code": 1001,
  "msg":  "error description"
}
```

### 4.2 Health Check

```
GET /api/health
```

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": {
    "status": "ok"
  }
}
```

### 4.3 Stream CRUD

#### 4.3.1 Create Stream

```
POST /api/streams
Content-Type: application/json
```

**Request Body:**
```json
{
  "channel_id":  "550e8400-e29b-41d4-a716-446655440000",
  "protocol":    "rtmp",
  "resolution":  "1920x1080",
  "bitrate":     "2000k"
}
```

| Field      | Type   | Required | Default  | Description            |
|------------|--------|----------|----------|------------------------|
| channel_id | UUID   | no       | null     | Associated channel     |
| protocol   | string | no       | `rtmp`   | Push protocol          |
| resolution | string | no       | null     | Video resolution       |
| bitrate    | string | no       | null     | Video bitrate          |

**Response (201 Created):**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": {
    "id":          "660e8400-e29b-41d4-a716-446655440001",
    "channel_id":  "550e8400-e29b-41d4-a716-446655440000",
    "stream_key":  "a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
    "protocol":    "rtmp",
    "resolution":  "1920x1080",
    "bitrate":     "2000k",
    "status":      "created",
    "push_url":    "rtmp://192.168.1.100:1935/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
    "hls_url":     "http://192.168.1.100:8080/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c.m3u8",
    "flv_url":     "http://192.168.1.100:8080/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c.flv",
    "webrtc_url":  null,
    "started_at":  null,
    "ended_at":    null,
    "created_at":  "2026-06-02T10:05:00Z"
  }
}
```

#### 4.3.2 List Streams

```
GET /api/streams
GET /api/streams?status=publishing
```

**Query Parameters:**

| Parameter | Type   | Required | Description                         |
|-----------|--------|----------|-------------------------------------|
| status    | string | no       | Filter: `created`, `publishing`, `ended`, `error` |

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": [
    { "...stream object..." },
    { "...stream object..." }
  ]
}
```

Returns empty array `[]` when no streams exist.

#### 4.3.3 Get Stream

```
GET /api/streams/:id
```

**Path Parameters:**

| Parameter | Type | Description  |
|-----------|------|--------------|
| id        | UUID | Stream ID    |

**Response:** Single stream object (same shape as 4.3.1).

**Error (404):**
```json
{
  "code": 1002,
  "msg":  "stream not found"
}
```

#### 4.3.4 Delete Stream

```
DELETE /api/streams/:id
```

**Behavior:**
1. If stream is `publishing`, kicks the SRS RTMP publisher client
2. Creates a `stop_push` agent task for cleanup
3. Deletes the stream record from database

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": null
}
```

#### 4.3.5 Start Streaming

```
POST /api/streams/:id/start
```

**Request Body:** None required.

**Behavior:** Creates a `start_push` agent task. The Raspberry Pi agent will poll for this task and launch FFmpeg to push the RTMP stream.

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": {
    "status": "start_requested"
  }
}
```

**Error (409 — already publishing):**
```json
{
  "code": 1004,
  "msg":  "stream is already publishing"
}
```

#### 4.3.6 Stop Streaming

```
POST /api/streams/:id/stop
```

**Behavior:**
1. If stream is `publishing`, kicks the SRS RTMP publisher client
2. Creates a `stop_push` agent task

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": {
    "status": "stop_requested"
  }
}
```

### 4.4 SRS Callbacks (Internal)

These endpoints are called by SRS via its `http_hooks` configuration. They always return HTTP 200 with `{"code":0}` to prevent SRS from retrying.

#### 4.4.1 Publish Callback (on_publish)

```
POST /api/callback/publish
Content-Type: application/json
```

**Request Body (sent by SRS):**
```json
{
  "action":    "on_publish",
  "stream":    "a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
  "client_id": "12345",
  "ip":        "192.168.1.50",
  "vhost":     "__defaultVhost__",
  "app":       "live",
  "param":     ""
}
```

**Go Server Behavior:** Looks up stream by `stream` field (the stream_key), sets status to `publishing`, records `started_at`.

**Response:**
```json
{"code": 0}
```

#### 4.4.2 Unpublish Callback (on_unpublish)

```
POST /api/callback/unpublish
Content-Type: application/json
```

**Request Body:** Same shape as publish callback, with `action: "on_unpublish"`.

**Go Server Behavior:** Looks up stream by `stream` field, sets status to `ended`, records `ended_at`.

**Response:**
```json
{"code": 0}
```

### 4.5 Agent Endpoints

#### 4.5.1 Get Pending Tasks

```
GET /api/agent/tasks
GET /api/agent/tasks?agent_id=raspi-01
```

**Query Parameters:**

| Parameter | Type   | Required | Description              |
|-----------|--------|----------|--------------------------|
| agent_id  | string | no       | Filter tasks by agent    |

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": {
    "tasks": [
      {
        "id":         "880e8400-e29b-41d4-a716-446655440003",
        "stream_id":  "660e8400-e29b-41d4-a716-446655440001",
        "agent_id":   "raspi-01",
        "action":     "start_push",
        "status":     "running",
        "stream_key": "a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
        "push_url":   "rtmp://192.168.1.100:1935/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c",
        "error_msg":  null,
        "created_at": "2026-06-02T10:06:00Z",
        "updated_at": "2026-06-02T10:06:01Z"
      }
    ]
  }
}
```

**Side Effect:** Fetched tasks are automatically marked as `running`.

#### 4.5.2 Update Task Status

```
PUT /api/agent/tasks/:id
Content-Type: application/json
```

**Request Body:**
```json
{
  "status":    "completed",
  "error_msg": ""
}
```

```json
{
  "status":    "failed",
  "error_msg": "FFmpeg process crashed with signal 11"
}
```

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": null
}
```

#### 4.5.3 Agent Heartbeat

```
POST /api/agent/heartbeat
Content-Type: application/json
```

**Request Body:**
```json
{
  "agent_id": "raspi-01",
  "version":  "1.0.0",
  "status":   "online"
}
```

| Field    | Type   | Required | Description                    |
|----------|--------|----------|--------------------------------|
| agent_id | string | yes      | Agent identifier               |
| version  | string | no       | Agent software version         |
| status   | string | no       | `online`, `busy`, `offline`    |

**Response:**
```json
{
  "code": 0,
  "msg":  "ok",
  "data": {
    "agent_id": "raspi-01"
  }
}
```

---

## 5. Push/Pull Stream Protocols & URL Formats

### 5.1 URL Construction Rules

All URLs are constructed by the Go server using configurable base URLs stored in `config.yaml`.

| URL Type   | Format                                              | Example                                                                              |
|------------|-----------------------------------------------------|--------------------------------------------------------------------------------------|
| RTMP Push  | `{srs.rtmp_base_url}/{stream_key}`                  | `rtmp://192.168.1.100:1935/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c`               |
| HLS Play   | `{nginx.hls_base_url}/{stream_key}.m3u8`            | `http://192.168.1.100:8080/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c.m3u8`           |
| FLV Play   | `{nginx.hls_base_url}/{stream_key}.flv`             | `http://192.168.1.100:8080/live/a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c.flv`            |
| WebRTC     | (reserved)                                          | TBD                                                                                  |

### 5.2 Stream Key Format

- **Format:** UUID v4 (lowercase hex with hyphens)
- **Example:** `a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c`
- **Generation:** `uuid.New().String()` (Google UUID library)
- **Uniqueness:** Guaranteed by database unique index on `streams.stream_key`

### 5.3 Test Stream Naming Convention

For manual testing and development, use these predefined names:

| Purpose              | Stream Key (for API creation) | Push URL (for FFmpeg)                                              | Play URL (for VLC/ffplay)                                                |
|----------------------|-------------------------------|---------------------------------------------------------------------|--------------------------------------------------------------------------|
| Test stream 1        | auto-generated by API         | `rtmp://ubuntu-vm:1935/live/<key>`                                 | `http://ubuntu-vm:8080/live/<key>.m3u8`                                  |
| FFmpeg test push     | use from API response         | `rtmp://localhost:1935/live/<key>` (from same machine)              | `http://localhost:8080/live/<key>.flv`                                   |
| OBS test push        | use from API response         | Server: `rtmp://ubuntu-vm:1935/live` Stream Key: `<key>`           | Play in VLC: `http://ubuntu-vm:8080/live/<key>.m3u8`                     |

### 5.4 FFmpeg Test Push Commands

```bash
# Push a test pattern (color bars + tone)
ffmpeg -re -f lavfi -i testsrc=size=1920x1080:rate=30 \
       -f lavfi -i sine=frequency=1000 \
       -c:v libx264 -preset ultrafast -b:v 2000k \
       -c:a aac -b:a 128k \
       -f flv rtmp://ubuntu-vm:1935/live/<stream_key>

# Push a video file loop
ffmpeg -re -stream_loop -1 -i /path/to/video.mp4 \
       -c copy \
       -f flv rtmp://ubuntu-vm:1935/live/<stream_key>

# Push from webcam (/dev/video0)
ffmpeg -re -f v4l2 -i /dev/video0 \
       -c:v libx264 -preset ultrafast -b:v 2000k \
       -f flv rtmp://ubuntu-vm:1935/live/<stream_key>
```

### 5.5 Playback Test Commands

```bash
# HLS playback
ffplay http://ubuntu-vm:8080/live/<stream_key>.m3u8
vlc http://ubuntu-vm:8080/live/<stream_key>.m3u8

# FLV playback
ffplay http://ubuntu-vm:8080/live/<stream_key>.flv

# Via Nginx (production path)
ffplay http://ubuntu-vm/live/<stream_key>.m3u8
```

---

## 6. SRS Callback Protocol

### 6.1 Callback Flow

```
1. FFmpeg pushes RTMP to SRS :1935/live/<stream_key>
2. SRS validates RTMP handshake, accepts stream
3. SRS fires on_publish  → POST http://localhost:9090/api/callback/publish
4. Go server looks up stream by stream_key, marks status = "publishing"
5. Go server returns {"code":0} to SRS
6. Streaming continues...
7. FFmpeg disconnects (or is kicked)
8. SRS fires on_unpublish → POST http://localhost:9090/api/callback/unpublish
9. Go server marks status = "ended"
```

### 6.2 SRS Callback Payload

All SRS HTTP hooks share the same JSON body format:

```json
{
  "action":    "on_publish",
  "stream":    "<stream_key>",
  "client_id": "<srs-internal-client-id>",
  "ip":        "<publisher-ip>",
  "vhost":     "__defaultVhost__",
  "app":       "live",
  "param":     "?token=xxx"
}
```

| Field     | Type   | Description                                               |
|-----------|--------|-----------------------------------------------------------|
| action    | string | Hook type: `on_publish`, `on_unpublish`, `on_play`, `on_stop`, `on_dvr` |
| stream    | string | The stream key (URL path component after `/live/`)         |
| client_id | string | SRS internal client connection ID                         |
| ip        | string | Client IP address                                         |
| vhost     | string | SRS virtual host (default: `__defaultVhost__`)            |
| app       | string | RTMP application name (default: `live`)                   |
| param     | string | Query string from the RTMP URL (e.g., `?token=jwt`)       |

### 6.3 Go Server Response to SRS

**Always return HTTP 200 with `{"code":0}`.** Any non-200 response will cause SRS to reject the stream or retry.

```json
{"code": 0}
```

In future versions (Step 2), publish auth will return HTTP 403 to reject unauthorized streams.

---

## 7. Agent Communication Protocol

### 7.1 Agent Workflow (Polling Model)

```
 ┌───────────┐                      ┌───────────────┐
 │  Agent    │                      │  Go Server    │
 │(Raspberry │                      │  :9090        │
 │   Pi)     │                      │               │
 └─────┬─────┘                      └───────┬───────┘
       │                                    │
       │  GET /api/agent/tasks?agent_id=X   │
       │───────────────────────────────────>│  (every 5 seconds)
       │                                    │
       │  [pending tasks]                   │
       │<───────────────────────────────────│  (tasks auto-marked "running")
       │                                    │
       │  Execute task:                     │
       │  start_push → launch FFmpeg        │
       │  stop_push  → kill FFmpeg          │
       │                                    │
       │  PUT /api/agent/tasks/:id          │
       │  {"status":"completed"}            │
       │───────────────────────────────────>│
       │                                    │
       │  POST /api/agent/heartbeat         │
       │  {"agent_id":"X","status":"online"}│  (every 30 seconds)
       │───────────────────────────────────>│
```

### 7.2 Task Actions

| Action      | Agent Behavior                                            |
|-------------|-----------------------------------------------------------|
| `start_push`| Launch FFmpeg: `ffmpeg -i <source> -f flv <push_url>`     |
| `stop_push` | Kill the FFmpeg process associated with this stream_key   |

### 7.3 Recommended Polling Intervals

| Endpoint               | Interval     | Purpose                              |
|------------------------|--------------|--------------------------------------|
| `GET /api/agent/tasks` | 5 seconds    | Quick task pickup                    |
| `POST /api/agent/heartbeat` | 30 seconds | Liveness signal                     |

---

## 8. Configuration Reference

### 8.1 Go Server (`config.yaml`)

```yaml
server:
  port: 9090          # HTTP API listen port
  mode: debug         # debug | release | test

srs:
  api_url: "http://localhost:1985"       # SRS HTTP management API
  rtmp_base_url: "rtmp://localhost:1935/live"  # Base URL for RTMP push addresses

database:
  host: localhost
  port: 5432
  user: live
  password: live_password
  dbname: live
  sslmode: disable
  timezone: Asia/Shanghai

nginx:
  hls_base_url: "http://localhost:8080/live"  # Base URL for HLS/FLV playback addresses
```

### 8.2 SRS (`minimal.conf`)

See `/home/dkw/dir1/rtmp-UbuntuVM/minimal.conf` for the current configuration.

Key settings:

| Directive             | Value                        | Purpose                                 |
|-----------------------|------------------------------|-----------------------------------------|
| `listen`              | 1935                         | RTMP ingest port                        |
| `http_api.listen`     | 1985                         | Management API port                     |
| `http_server.listen`   | 8080                        | Direct HLS/FLV serving                  |
| `hls.hls_path`        | `/dev/shm/hls`               | HLS segment storage (tmpfs)             |
| `hls.hls_fragment`    | 3                            | Segment duration in seconds             |
| `hls.hls_window`      | 15                           | Playlist window in seconds (5 segments) |
| `http_hooks.on_publish`| `http://localhost:9090/api/callback` | Publish callback             |
| `http_hooks.on_unpublish`| `http://localhost:9090/api/callback` | Unpublish callback           |

---

## 9. Error Codes

| Code | HTTP Status | Meaning                      | Typical Scenario                         |
|------|-------------|------------------------------|------------------------------------------|
| 0    | 200         | Success                      | Normal operation                         |
| 1001 | 400         | Invalid parameter            | Bad JSON, missing required field, validation failure |
| 1002 | 404         | Resource not found           | Stream/channel ID does not exist         |
| 1003 | 500         | Internal server error        | Database failure, unexpected panic       |
| 1004 | 409         | Resource conflict            | Attempting to start already-publishing stream |

**Future Error Codes (planned for Steps 2-3):**

| Code | HTTP Status | Meaning                      | Scenario                                 |
|------|-------------|------------------------------|------------------------------------------|
| 1005 | 401         | Unauthorized                 | Missing/invalid JWT token                |
| 1006 | 403         | Forbidden                    | Publish/play auth failed                 |
| 1007 | 429         | Rate limited                 | Too many requests                        |

---

## 10. Testing Guide

### 10.1 End-to-End Test Script

Below is the complete test flow for verifying the system.

```bash
# ─── Step 0: Verify all services are running ───
curl http://localhost:9090/api/health
# → {"code":0,"msg":"ok","data":{"status":"ok"}}

curl http://localhost:1985/api/v1/versions
# → {"code":0,"data":{"version":"5.0.x"}}

ss -tlnp | grep -E '1935|8080|1985|9090'
# → All four ports should be LISTEN

# ─── Step 1: Create a stream ───
curl -s -X POST http://localhost:9090/api/streams \
  -H "Content-Type: application/json" \
  -d '{
    "protocol": "rtmp",
    "resolution": "1920x1080",
    "bitrate": "2000k"
  }' | jq .

# Save the stream_key and push_url from response:
# STREAM_KEY="<from response>"
# STREAM_ID="<from response>"
# PUSH_URL="<from response>"

# ─── Step 2: Start the stream (create agent task) ───
curl -s -X POST http://localhost:9090/api/streams/$STREAM_ID/start | jq .
# → {"code":0,"msg":"ok","data":{"status":"start_requested"}}

# ─── Step 3: Simulate agent polling for tasks ───
curl -s http://localhost:9090/api/agent/tasks | jq .
# → Should show the start_push task

# ─── Step 4: Push a test stream with FFmpeg ───
ffmpeg -re -f lavfi -i testsrc=size=1920x1080:rate=30 \
       -f lavfi -i sine=frequency=1000 \
       -c:v libx264 -preset ultrafast -b:v 2000k \
       -c:a aac -b:a 128k \
       -f flv "$PUSH_URL"

# Keep FFmpeg running, open another terminal...

# ─── Step 5: Verify stream status changed to "publishing" ───
curl -s http://localhost:9090/api/streams/$STREAM_ID | jq .
# → status should be "publishing", started_at should be set

# ─── Step 6: Play the stream ───
# Get HLS URL from the stream response:
HLS_URL=$(curl -s http://localhost:9090/api/streams/$STREAM_ID | jq -r '.data.hls_url')
ffplay "$HLS_URL"

# Or via browser / VLC:
# http://localhost:8080/live/$STREAM_KEY.m3u8

# ─── Step 7: Stop FFmpeg (Ctrl+C in the FFmpeg terminal) ───

# ─── Step 8: Verify stream status changed to "ended" ───
curl -s http://localhost:9090/api/streams/$STREAM_ID | jq .
# → status should be "ended", ended_at should be set

# ─── Step 9: Clean up ───
curl -s -X DELETE http://localhost:9090/api/streams/$STREAM_ID | jq .
# → {"code":0,"msg":"ok","data":null}
```

### 10.2 Test Names / Identifiers Convention

For testing, use these naming patterns:

| Resource   | Pattern                          | Example                                  |
|------------|----------------------------------|------------------------------------------|
| Stream Key | UUID (auto)                      | `a3f2b8c1-9d4e-5f6a-8b7c-1d2e3f4a5b6c`  |
| Agent ID   | `test-agent-{NN}`                | `test-agent-01`                          |
| Channel    | `test-channel-{description}`     | `test-channel-living-room`               |
| Test Push  | `rtmp://localhost:1935/live/{key}`| `rtmp://localhost:1935/live/a3f2b8c1...` |

---

## 11. Future Expansion Plan

This section documents features planned for future implementation steps (as defined in the master plan).

### 11.1 Step 2 — Core Features (Auth + Monitoring)

#### Publish Authentication (推流鉴权)

Stream creation will generate a JWT token:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

Push URL format with auth:
```
rtmp://srs:1935/live/<stream_key>?token=<jwt_token>
```

Go server validates token in `on_publish` callback:
- **Valid** → HTTP 200 → SRS allows publish
- **Invalid/Expired** → HTTP 403 → SRS rejects publish

Token refresh API:
```
POST /api/streams/:id/refresh-token
```

#### Play Authentication (播放鉴权)

Play URL with HMAC signature:
```
http://nginx/live/stream.m3u8?sign=<hmac_sha256>&expire=<unix_ts>
```

```
sign = HMAC-SHA256(stream_id + expire, secret_key)
```

Nginx `auth_request` directive proxies `.m3u8` and `.flv` requests to Go server:
```
GET /api/auth/play?stream_id=X&sign=Y&expire=Z
```

#### Redis Integration

Stream status cache:
```
stream:<stream_id>:status  →  {"status":"publishing","viewers":5,"bitrate":"2000k","started_at":"..."}
TTL: 3600s (refreshed on publish/play events)
```

Viewer counters (two modes):
- **Mode A (callback):** `INCR stream:<id>:viewers` on `on_play`, `DECR` on `on_stop` (Lua script prevents negative)
- **Mode B (polling):** Poll `GET /api/v1/clients` every 60s
- Config: `viewer_count_method: callback|polling`

Play URL cache:
```
stream:<stream_id>:play_urls  →  {"hls_url":"...","flv_url":"..."}
```

Token blacklist:
```
blacklist:token:<token_id>  →  "1"
TTL: remaining validity of original token
```

#### Prometheus Metrics

| Metric                                   | Type      | Labels                          |
|------------------------------------------|-----------|---------------------------------|
| `http_requests_total`                    | Counter   | method, path, status            |
| `http_request_duration_seconds`          | Histogram | method, path                    |
| `live_streams_active`                    | Gauge     | —                               |
| `live_viewers_total`                     | Gauge     | stream_id                       |
| `stream_publish_duration_seconds`        | Gauge     | stream_id                       |
| `ffmpeg_processes_running`               | Gauge     | —                               |
| `ffmpeg_restart_total`                   | Counter   | stream_id                       |
| `srs_callback_errors_total`              | Counter   | event_type                      |
| `auth_failures_total`                    | Counter   | type (publish/play/admin)       |
| `redis_commands_duration_seconds`        | Histogram | —                               |
| `redis_connection_pool_size`             | Gauge     | —                               |
| `db_connections_open`                    | Gauge     | —                               |
| `db_query_duration_seconds`              | Histogram | —                               |

Endpoint: `GET :9091/metrics`

### 11.2 Step 3 — Production Hardening

#### Recording (DVR)

SRS DVR configuration:
```nginx
dvr {
    enabled on;
    dvr_path /data/recordings/[stream]_[timestamp].mp4;
    dvr_plan segment;
    dvr_duration 600;
    dvr_wait_keyframe on;
}
```

New recording management APIs:
```
GET    /api/recordings                      # list (with pagination & filters)
GET    /api/recordings/:id                  # detail
DELETE /api/recordings/:id                  # delete file + record
GET    /api/recordings/:id/download         # file download
```

Recording file path pattern:
```
/data/recordings/<stream_id>/<YYYY-MM-DD>/<stream_id>_<unix_timestamp>.mp4
```

Daily cleanup cron: `0 3 * * *` — delete recordings older than `recording.retention_days` (default 30).

#### Admin Authentication

```
POST /api/auth/login
Body: {"username": "admin", "password": "xxx"}

Response: {"token": "<jwt_token>"}
```

All management APIs require `Authorization: Bearer <jwt_token>` header (via `AuthRequired()` middleware).

#### Rate Limiting

| Endpoint Group      | Limit            |
|---------------------|------------------|
| Management APIs     | 10 req/s per IP  |
| Play auth           | 30 req/s per IP  |
| SRS callbacks       | Unlimited        |

#### Graceful Shutdown

- 30-second timeout for in-flight HTTP requests
- Ordered teardown: HTTP → DB → Redis
- Signal handlers for SIGINT / SIGTERM

### 11.3 Step 4 — Performance Tuning

#### System Kernel Parameters

```ini
fs.file-max = 655350
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 5000
net.ipv4.tcp_max_syn_backlog = 8192
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 10
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
```

#### tmpfs for HLS

```
tmpfs /dev/shm/hls tmpfs defaults,size=512M 0 0
```

#### SRS Tuning Targets

- HLS fragment: 2-3 seconds (low latency) vs 5-10 seconds (stability)
- Workers: `auto` (matches CPU count)
- Max connections: 2000+

### 11.4 Step 5 — Deployment

#### Docker Compose Services

| Service    | Image              | Ports                     | Volumes                    |
|------------|--------------------|---------------------------|----------------------------|
| srs        | ossrs/srs:5        | 1935, 8080, 1985          | config, hls tmpfs          |
| go-server  | custom Dockerfile  | 9090, 9091                | config.yaml                |
| nginx      | nginx:alpine       | 80                        | nginx.conf                 |
| postgres   | postgres:15-alpine | 5432                      | data volume, init SQL      |
| redis      | redis:7-alpine     | 6379                      | data volume                |

---

## Appendix A: Quick Reference — All API Endpoints

### Current (MVP / Step 1)

| Method | Path                         | Auth | Description                    |
|--------|------------------------------|------|--------------------------------|
| GET    | `/api/health`                | No   | Health check                   |
| POST   | `/api/streams`               | No   | Create stream                  |
| GET    | `/api/streams`               | No   | List streams (optional `?status=`) |
| GET    | `/api/streams/:id`           | No   | Get stream detail              |
| DELETE | `/api/streams/:id`           | No   | Delete stream                  |
| POST   | `/api/streams/:id/start`     | No   | Start streaming (create task)  |
| POST   | `/api/streams/:id/stop`      | No   | Stop streaming                 |
| POST   | `/api/callback/publish`      | No   | SRS on_publish hook            |
| POST   | `/api/callback/unpublish`    | No   | SRS on_unpublish hook          |
| GET    | `/api/agent/tasks`           | No   | Get pending agent tasks        |
| PUT    | `/api/agent/tasks/:id`       | No   | Update agent task status       |
| POST   | `/api/agent/heartbeat`       | No   | Agent heartbeat                |

### Planned (Steps 2-3)

| Method | Path                              | Auth     | Description                   |
|--------|-----------------------------------|----------|-------------------------------|
| POST   | `/api/auth/login`                 | No       | Admin login → JWT             |
| POST   | `/api/streams/:id/refresh-token`  | Admin    | Refresh publish token         |
| GET    | `/api/auth/play`                  | No       | Play auth (Nginx auth_request)|
| GET    | `/api/recordings`                 | Admin    | List recordings               |
| GET    | `/api/recordings/:id`             | Admin    | Get recording detail          |
| DELETE | `/api/recordings/:id`             | Admin    | Delete recording              |
| GET    | `/api/recordings/:id/download`    | Admin    | Download recording file       |
| GET    | `/metrics`                        | No       | Prometheus metrics            |

## Appendix B: Environment Variables

| Variable      | Default                        | Description                          |
|---------------|--------------------------------|--------------------------------------|
| `CONFIG_PATH` | `./config.yaml`                | Path to Go server config file        |
| `DB_HOST`     | `localhost`                    | PostgreSQL host                      |
| `DB_PORT`     | `5432`                         | PostgreSQL port                      |
| `DB_USER`     | `live`                         | PostgreSQL user                      |
| `DB_PASSWORD` | `live_password`                | PostgreSQL password                  |
| `DB_NAME`     | `live`                         | PostgreSQL database name             |
| `SRS_API_URL` | `http://localhost:1985`         | SRS management API URL               |
| `REDIS_ADDR`  | `localhost:6379`               | Redis address (Step 2+)              |

