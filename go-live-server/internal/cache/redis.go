package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-live-server/internal/config"
	"go-live-server/internal/metrics"

	"github.com/redis/go-redis/v9"
)

// Client wraps a go-redis client with cache-specific helpers.
type Client struct {
	rdb *redis.Client
	ttl time.Duration
}

// metricsHook implements redis.Hook for Prometheus instrumentation.
type metricsHook struct{}

func (h *metricsHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *metricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		metrics.RedisCommandDuration.WithLabelValues(cmd.Name()).Observe(time.Since(start).Seconds())
		return err
	}
}

func (h *metricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		metrics.RedisCommandDuration.WithLabelValues("pipeline").Observe(time.Since(start).Seconds())
		return err
	}
}

// New creates a Redis client with connection pooling and validates connectivity.
func New(cfg config.RedisConfig) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     20,
		MinIdleConns: 5,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	// Attach Prometheus metrics hook
	rdb.AddHook(&metricsHook{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	client := &Client{rdb: rdb, ttl: cfg.CacheDuration()}

	// Start pool stats goroutine
	go client.reportPoolStats()

	log.Printf("[cache] redis connected: %s db=%d ttl=%s",
		cfg.Addr(), cfg.DB, cfg.CacheDuration())

	return client, nil
}

// Close shuts down the Redis connection pool.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// RDB returns the underlying go-redis client for advanced operations.
func (c *Client) RDB() *redis.Client {
	return c.rdb
}

// TTL returns the default cache TTL.
func (c *Client) TTL() time.Duration {
	return c.ttl
}

// reportPoolStats periodically updates Redis pool size gauges.
func (c *Client) reportPoolStats() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := c.rdb.PoolStats()
		metrics.RedisPoolSize.WithLabelValues("idle").Set(float64(stats.IdleConns))
		metrics.RedisPoolSize.WithLabelValues("active").Set(float64(stats.TotalConns - stats.IdleConns))
		metrics.RedisPoolSize.WithLabelValues("total").Set(float64(stats.TotalConns))
	}
}

// ---------- key helpers ----------

func statusKey(streamID string) string {
	return fmt.Sprintf("stream:%s:status", streamID)
}

func playURLsKey(streamID string) string {
	return fmt.Sprintf("stream:%s:play_urls", streamID)
}

func viewersKey(streamID string) string {
	return fmt.Sprintf("stream:%s:viewers", streamID)
}

func blacklistKey(tokenHash string) string {
	return fmt.Sprintf("blacklist:token:%s", tokenHash)
}
