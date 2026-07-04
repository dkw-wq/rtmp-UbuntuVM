-- Enable UUID generation (required for PostgreSQL < 13)
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- -----------------------------------------------------------
-- 1. Channels
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS channels (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    description TEXT         DEFAULT '',
    status      VARCHAR(20)  DEFAULT 'inactive',
    created_at  TIMESTAMPTZ  DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  DEFAULT NOW()
);

-- -----------------------------------------------------------
-- 2. Streams
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS streams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id  UUID REFERENCES channels(id) ON DELETE SET NULL,
    stream_key  VARCHAR(128) UNIQUE NOT NULL,
    protocol    VARCHAR(20)  DEFAULT 'rtmp',
    resolution  VARCHAR(20)  DEFAULT '',
    bitrate     VARCHAR(10)  DEFAULT '',
    status      VARCHAR(20)  DEFAULT 'created',
    push_token  TEXT         DEFAULT '',
    push_url    TEXT         DEFAULT '',
    hls_url     TEXT         DEFAULT '',
    flv_url     TEXT         DEFAULT '',
    webrtc_url  TEXT         DEFAULT '',
    started_at  TIMESTAMPTZ,
    ended_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_streams_stream_key ON streams(stream_key);
CREATE INDEX IF NOT EXISTS idx_streams_status     ON streams(status);
CREATE INDEX IF NOT EXISTS idx_streams_channel_id ON streams(channel_id);

-- -----------------------------------------------------------
-- 3. Recordings
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS recordings (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stream_id    UUID REFERENCES streams(id) ON DELETE CASCADE,
    file_path    TEXT        NOT NULL,
    file_size    BIGINT      DEFAULT 0,
    duration_sec INT         DEFAULT 0,
    started_at   TIMESTAMPTZ,
    ended_at     TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recordings_stream_id ON recordings(stream_id);

-- -----------------------------------------------------------
-- 4. Agent tasks
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS agent_tasks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stream_id  UUID REFERENCES streams(id) ON DELETE CASCADE,
    agent_id   VARCHAR(64)  DEFAULT '',
    action     VARCHAR(20)  NOT NULL,             -- start_push | stop_push
    status     VARCHAR(20)  DEFAULT 'pending',    -- pending | running | completed | failed
    stream_key VARCHAR(128) NOT NULL,
    push_url   TEXT         DEFAULT '',
    error_msg  TEXT         DEFAULT '',
    created_at TIMESTAMPTZ  DEFAULT NOW(),
    updated_at TIMESTAMPTZ  DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_tasks_status   ON agent_tasks(status);
CREATE INDEX IF NOT EXISTS idx_agent_tasks_agent_id ON agent_tasks(agent_id);
