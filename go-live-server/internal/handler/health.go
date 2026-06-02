package handler

import (
	"github.com/gin-gonic/gin"
)

// HealthCheck returns a simple status response.
func HealthCheck(c *gin.Context) {
	Success(c, H{
		"status": "ok",
	})
}
