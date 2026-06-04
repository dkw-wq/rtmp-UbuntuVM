package handler

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"go-live-server/internal/auth"
	"go-live-server/internal/config"
	"go-live-server/internal/metrics"
	"go-live-server/internal/service"

	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	svc        *service.StreamService
	authCfg    config.AuthConfig
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(svc *service.StreamService, authCfg config.AuthConfig) *AuthHandler {
	return &AuthHandler{svc: svc, authCfg: authCfg}
}

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /api/auth/login — admin username/password login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, CodeInvalidParam, "username and password are required")
		return
	}

	if req.Username != h.authCfg.AdminUsername || req.Password != h.authCfg.AdminPassword {
		metrics.AuthFailuresTotal.WithLabelValues("admin").Inc()
		Error(c, http.StatusUnauthorized, 1005, "invalid credentials")
		return
	}

	token, err := auth.GenerateAdminToken(req.Username, h.authCfg.JWTSecret, h.authCfg.AdminExpiry())
	if err != nil {
		log.Printf("[handler] generate admin token error: %v", err)
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to generate token")
		return
	}

	Success(c, H{
		"token":      token,
		"expires_in": int(h.authCfg.AdminExpiry().Seconds()),
	})
}

// PlayAuth handles GET /api/auth/play — validates HMAC play auth for Nginx auth_request.
// Query params: stream_key, sign, expire
func (h *AuthHandler) PlayAuth(c *gin.Context) {
	streamKey := c.Query("stream_key")
	sign := c.Query("sign")
	expireStr := c.Query("expire")

	if streamKey == "" || sign == "" || expireStr == "" {
		metrics.AuthFailuresTotal.WithLabelValues("play").Inc()
		c.JSON(http.StatusForbidden, H{"code": 1005, "msg": "missing auth params"})
		return
	}

	expire, err := strconv.ParseInt(expireStr, 10, 64)
	if err != nil {
		metrics.AuthFailuresTotal.WithLabelValues("play").Inc()
		c.JSON(http.StatusForbidden, H{"code": 1005, "msg": "invalid expire"})
		return
	}

	if expire <= time.Now().Unix() {
		metrics.AuthFailuresTotal.WithLabelValues("play").Inc()
		c.JSON(http.StatusForbidden, H{"code": 1005, "msg": "play URL has expired"})
		return
	}

	if err := auth.ValidatePlaySign(streamKey, expire, sign, h.authCfg.PlaySecret); err != nil {
		log.Printf("[handler] play auth failed: key=%s err=%v", streamKey, err)
		metrics.AuthFailuresTotal.WithLabelValues("play").Inc()
		c.JSON(http.StatusForbidden, H{"code": 1005, "msg": "invalid signature"})
		return
	}

	c.JSON(http.StatusOK, H{"code": 0})
}

// GetPlaybackInfo handles GET /api/playback/:stream_key — public endpoint for viewers.
// Returns stream status and freshly-signed HLS/FLV URLs. No JWT required.
func (h *AuthHandler) GetPlaybackInfo(c *gin.Context) {
	streamKey := c.Param("stream_key")

	stream, err := h.svc.GetPlaybackInfo(streamKey)
	if err != nil {
		Error(c, http.StatusNotFound, CodeNotFound, "stream not found")
		return
	}

	Success(c, H{
		"id":         stream.ID,
		"stream_key": stream.StreamKey,
		"status":     stream.Status,
		"resolution": stream.Resolution,
		"bitrate":    stream.Bitrate,
		"hls_url":    stream.HlsURL,
		"flv_url":    stream.FlvURL,
	})
}

// RefreshToken handles POST /api/streams/:id/refresh-token
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	id := c.Param("id")

	stream, err := h.svc.RefreshPushToken(id)
	if err != nil {
		log.Printf("[handler] refresh token error: %v", err)
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to refresh push token")
		return
	}

	Success(c, stream)
}
