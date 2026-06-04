package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// StreamStatusData is the cached stream status snapshot.
type StreamStatusData struct {
	Status    string `json:"status"`
	Bitrate   string `json:"bitrate"`
	Viewers   int64  `json:"viewers"`
	StartedAt string `json:"started_at,omitempty"`
}

// PlayURLData holds cached play URLs.
type PlayURLData struct {
	HlsURL    string `json:"hls_url"`
	FlvURL    string `json:"flv_url"`
	WebRTCURL string `json:"webrtc_url,omitempty"`
}

// ---------- Status cache ----------

// SetStreamStatus writes stream status to Redis with TTL.
func (c *Client) SetStreamStatus(ctx context.Context, streamID string, data StreamStatusData) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}
	return c.rdb.Set(ctx, statusKey(streamID), payload, c.ttl).Err()
}

// GetStreamStatus reads stream status from Redis. Returns nil if not found.
func (c *Client) GetStreamStatus(ctx context.Context, streamID string) (*StreamStatusData, error) {
	val, err := c.rdb.Get(ctx, statusKey(streamID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}
	var data StreamStatusData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, fmt.Errorf("unmarshal status: %w", err)
	}
	return &data, nil
}

// RefreshStreamTTL resets the TTL on the stream status key (e.g. on on_publish).
func (c *Client) RefreshStreamTTL(ctx context.Context, streamID string) error {
	return c.rdb.Expire(ctx, statusKey(streamID), c.ttl).Err()
}

// ---------- Play URL cache ----------

// SetPlayURLs writes play URLs to Redis with TTL.
func (c *Client) SetPlayURLs(ctx context.Context, streamID string, data PlayURLData) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal play urls: %w", err)
	}
	return c.rdb.Set(ctx, playURLsKey(streamID), payload, c.ttl).Err()
}

// GetPlayURLs reads play URLs from Redis. Returns nil if not found.
func (c *Client) GetPlayURLs(ctx context.Context, streamID string) (*PlayURLData, error) {
	val, err := c.rdb.Get(ctx, playURLsKey(streamID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get play urls: %w", err)
	}
	var data PlayURLData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, fmt.Errorf("unmarshal play urls: %w", err)
	}
	return &data, nil
}

// ---------- Viewer counting (method A: SRS callbacks) ----------

// luaDecrSafe atomically decrements a key but never below zero.
var luaDecrSafe = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if v and tonumber(v) > 0 then
    return redis.call('DECR', KEYS[1])
end
return 0
`)

// IncrViewers increments the viewer counter for a stream.
func (c *Client) IncrViewers(ctx context.Context, streamID string) (int64, error) {
	return c.rdb.Incr(ctx, viewersKey(streamID)).Result()
}

// DecrViewers decrements the viewer counter, never going below zero.
func (c *Client) DecrViewers(ctx context.Context, streamID string) (int64, error) {
	return luaDecrSafe.Run(ctx, c.rdb, []string{viewersKey(streamID)}).Int64()
}

// ResetViewers sets the viewer count to zero (called on unpublish).
func (c *Client) ResetViewers(ctx context.Context, streamID string) error {
	return c.rdb.Del(ctx, viewersKey(streamID)).Err()
}

// GetViewers reads the current viewer count.
func (c *Client) GetViewers(ctx context.Context, streamID string) (int64, error) {
	val, err := c.rdb.Get(ctx, viewersKey(streamID)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

// SetViewers sets the viewer count directly (used by polling method B).
func (c *Client) SetViewers(ctx context.Context, streamID string, count int64) error {
	return c.rdb.Set(ctx, viewersKey(streamID), count, 0).Err()
}

// ---------- Token blacklist ----------

// tokenHash returns a truncated SHA256 hex digest of the token.
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:8]) // 16 hex chars
}

// BlacklistToken adds a push token to the blacklist with TTL = remaining validity.
func (c *Client) BlacklistToken(ctx context.Context, token string, remaining time.Duration) error {
	if remaining <= 0 {
		return nil
	}
	return c.rdb.Set(ctx, blacklistKey(tokenHash(token)), "1", remaining).Err()
}

// IsTokenBlacklisted checks whether a push token is in the blacklist.
func (c *Client) IsTokenBlacklisted(ctx context.Context, token string) (bool, error) {
	exists, err := c.rdb.Exists(ctx, blacklistKey(tokenHash(token))).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// ---------- Cleanup ----------

// DeleteStreamCache removes all Redis keys for a stream (status, play_urls, viewers).
func (c *Client) DeleteStreamCache(ctx context.Context, streamID string) error {
	keys := []string{statusKey(streamID), playURLsKey(streamID), viewersKey(streamID)}
	return c.rdb.Del(ctx, keys...).Err()
}
