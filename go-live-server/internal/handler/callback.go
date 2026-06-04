package handler

import (
	"log"
	"net/http"
	"net/url"
	"strconv"
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
// Extracts HMAC token + expire from the RTMP URL and validates them.
//
// Two formats are supported:
//  1. SRS passes query params via the "param" field (e.g., OBS, SRS API)
//  2. librtmp/FFmpeg appends "?token=xxx&expire=123" to the stream name directly
//
// Returns 200 (allow) on success, 403 (reject) on auth failure.
func (h *CallbackHandler) Publish(c *gin.Context) {
	var body srsCallback
	if err := c.ShouldBindJSON(&body); err != nil {
		// Parse error — always return 200 so SRS doesn't retry
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}

	streamKey, token, expire := h.parsePushAuth(body.Stream, body.Param)

	if err := h.svc.OnPublish(streamKey, token, expire); err != nil {
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

	streamKey, _, _ := h.parsePushAuth(body.Stream, body.Param)

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
	streamKey, _, _ := h.parsePushAuth(body.Stream, body.Param)
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
	streamKey, _, _ := h.parsePushAuth(body.Stream, body.Param)
	h.svc.OnStop(streamKey)
	c.JSON(http.StatusOK, H{"code": 0})
}

// parsePushAuth extracts stream_key, token, and expire from SRS callback data.
//
// Priority 1: SRS "param" field (query string like "token=xxx&expire=123").
// Priority 2: librtmp/FFmpeg appends "?token=xxx&expire=123" to the stream name.
// Priority 3: No auth params — return stream key as-is with empty token/expire.
func (h *CallbackHandler) parsePushAuth(stream, param string) (streamKey string, token string, expire int64) {
	streamKey = stream

	// Try format 1: SRS param field (semicolon or ampersand separated)
	if param != "" {
		token, expire = parseTokenParams(param)
		if token != "" {
			return
		}
	}

	// Try format 2: librtmp/FFmpeg — query string appended to stream name
	if idx := strings.Index(streamKey, "?token="); idx >= 0 {
		queryPart := streamKey[idx+1:] // strip leading "?"
		streamKey = streamKey[:idx]
		t, e := parseTokenParams(queryPart)
		if t != "" {
			token = t
			expire = e
			return
		}
	}

	return
}

// parseTokenParams extracts token and expire from a query-string-like param value.
// Handles: "token=xxx&expire=123", "?token=xxx&expire=123", "token=xxx"
func parseTokenParams(raw string) (token string, expire int64) {
	raw = strings.TrimPrefix(raw, "?")
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", 0
	}
	token = values.Get("token")
	if expireStr := values.Get("expire"); expireStr != "" {
		if e, err := strconv.ParseInt(expireStr, 10, 64); err == nil {
			expire = e
		}
	}
	return
}
