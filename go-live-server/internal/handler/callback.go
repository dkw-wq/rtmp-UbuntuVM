package handler

import (
	"log"
	"net/http"
	"net/url"
	"strings"

	"go-live-server/internal/metrics"
	"go-live-server/internal/service"

	"github.com/gin-gonic/gin"
)

// CallbackHandler handles SRS HTTP callbacks.
type CallbackHandler struct {
	svc *service.StreamService
}

// NewCallbackHandler creates a new CallbackHandler.
func NewCallbackHandler(svc *service.StreamService) *CallbackHandler {
	return &CallbackHandler{svc: svc}
}

// srsCallback is the JSON body sent by SRS for any HTTP hook.
type srsCallback struct {
	Action   string `json:"action"`
	Stream   string `json:"stream"`
	ClientID string `json:"client_id"`
	IP       string `json:"ip"`
	Vhost    string `json:"vhost"`
	App      string `json:"app"`
	Param    string `json:"param"`
}

// Publish handles SRS on_publish callback.
// Extracts the JWT token from the RTMP URL and validates it.
// Handles two formats:
//  1. SRS passes query params via the "param" field (e.g., OBS, SRS API)
//  2. librtmp/FFmpeg appends "?token=xxx" to the stream name directly
//
// Returns 200 (allow) on success, 403 (reject) on auth failure.
func (h *CallbackHandler) Publish(c *gin.Context) {
	var body srsCallback
	if err := c.ShouldBindJSON(&body); err != nil {
		// Parse error — always return 200 with code 0 so SRS doesn't retry
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}

	streamKey := body.Stream
	token := ""

	// Prefer the SRS "param" field (query string from RTMP URL).
	// Works when SRS correctly separates params (e.g., OBS).
	token = extractToken(body.Param)

	// Fallback: librtmp/FFmpeg treats "?" as part of the stream name.
	// The stream name becomes "key?token=xxx" — split it manually.
	if token == "" && strings.Contains(streamKey, "?token=") {
		idx := strings.Index(streamKey, "?token=")
		token = streamKey[idx+7:] // skip "?token="
		streamKey = streamKey[:idx]
	}

	if err := h.svc.OnPublish(streamKey, token); err != nil {
		log.Printf("[callback] on_publish rejected: key=%s err=%v", streamKey, err)
		metrics.SrsCallbackErrorsTotal.WithLabelValues("on_publish").Inc()
		c.JSON(http.StatusForbidden, H{"code": 1005, "msg": "unauthorized"})
		return
	}

	c.JSON(http.StatusOK, H{"code": 0})
}

// Unpublish handles SRS on_unpublish callback.
func (h *CallbackHandler) Unpublish(c *gin.Context) {
	var body srsCallback
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}

	streamKey := body.Stream
	// Strip ?token= suffix if present (librtmp compatibility)
	if idx := strings.Index(streamKey, "?token="); idx >= 0 {
		streamKey = streamKey[:idx]
	}

	if err := h.svc.OnUnpublish(streamKey); err != nil {
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}

	c.JSON(http.StatusOK, H{"code": 0})
}

// Play handles SRS on_play callback — records viewer join for counting.
func (h *CallbackHandler) Play(c *gin.Context) {
	var body srsCallback
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}
	streamKey := cleanStreamKey(body.Stream)
	h.svc.OnPlay(streamKey)
	c.JSON(http.StatusOK, H{"code": 0})
}

// Stop handles SRS on_stop callback — records viewer leave for counting.
func (h *CallbackHandler) Stop(c *gin.Context) {
	var body srsCallback
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}
	streamKey := cleanStreamKey(body.Stream)
	h.svc.OnStop(streamKey)
	c.JSON(http.StatusOK, H{"code": 0})
}

// cleanStreamKey strips librtmp-style ?token= suffix from the stream name.
func cleanStreamKey(stream string) string {
	if idx := strings.Index(stream, "?token="); idx >= 0 {
		return stream[:idx]
	}
	return stream
}

// extractToken parses the token from an SRS param field.
// SRS may include a leading "?" (e.g., "?token=xxx" or "token=xxx").
func extractToken(param string) string {
	if param == "" {
		return ""
	}
	// Strip leading "?" if present
	if strings.HasPrefix(param, "?") {
		param = param[1:]
	}
	values, err := url.ParseQuery(param)
	if err != nil {
		return ""
	}
	return values.Get("token")
}
