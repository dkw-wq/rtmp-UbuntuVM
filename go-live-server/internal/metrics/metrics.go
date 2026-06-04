package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// ---------- HTTP ----------

var (
	HttpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	HttpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: []float64{.01, .05, 0.1, 0.5, 1, 5},
		},
		[]string{"method", "path"},
	)
)

// ---------- Business ----------

var (
	LiveStreamsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "live_streams_active",
			Help: "Number of currently active (publishing) live streams.",
		},
		[]string{"stream_id"},
	)

	LiveViewersTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "live_viewers_total",
			Help: "Current viewer count per stream.",
		},
		[]string{"stream_id"},
	)

	StreamPublishDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "stream_publish_duration_seconds",
			Help: "Duration of the most recent publish session in seconds.",
		},
		[]string{"stream_id"},
	)

	FfmpegProcessesRunning = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ffmpeg_processes_running",
			Help: "Number of FFmpeg processes currently running across all agents.",
		},
	)

	FfmpegRestartTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ffmpeg_restart_total",
			Help: "Total number of FFmpeg process restarts.",
		},
		[]string{"stream_id"},
	)

	SrsCallbackErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "srs_callback_errors_total",
			Help: "Total SRS callback errors by event type.",
		},
		[]string{"event_type"},
	)

	AuthFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_failures_total",
			Help: "Total authentication failures by type.",
		},
		[]string{"type"},
	)
)

// ---------- Redis ----------

var (
	RedisCommandDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "redis_commands_duration_seconds",
			Help:    "Redis command latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"command"},
	)

	RedisPoolSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redis_connection_pool_size",
			Help: "Redis connection pool size by state.",
		},
		[]string{"state"},
	)
)

// ---------- DB ----------

var (
	DbConnectionsOpen = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "db_connections_open",
			Help: "Number of open database connections.",
		},
	)

	DbQueryDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)
)

func init() {
	prometheus.MustRegister(
		HttpRequestsTotal,
		HttpRequestDuration,

		LiveStreamsActive,
		LiveViewersTotal,
		StreamPublishDuration,
		FfmpegProcessesRunning,
		FfmpegRestartTotal,
		SrsCallbackErrorsTotal,
		AuthFailuresTotal,

		RedisCommandDuration,
		RedisPoolSize,

		DbConnectionsOpen,
		DbQueryDuration,
	)
}
