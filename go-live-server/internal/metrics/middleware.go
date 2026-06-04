package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPMetrics returns a Gin middleware that records HTTP request metrics.
// Uses Gin's c.FullPath() for normalized path labels (/:id instead of /abc-123).
func HTTPMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		// Use Gin's normalized path template (e.g. /api/streams/:id)
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		HttpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		HttpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}
