package handler

import (
	"net/http"

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
func (h *CallbackHandler) Publish(c *gin.Context) {
	var body srsCallback
	if err := c.ShouldBindJSON(&body); err != nil {
		// Even on parse error, return 200 with code 0 so SRS doesn't retry
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}

	if err := h.svc.OnPublish(body.Stream); err != nil {
		c.JSON(http.StatusOK, H{"code": 0})
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

	if err := h.svc.OnUnpublish(body.Stream); err != nil {
		c.JSON(http.StatusOK, H{"code": 0})
		return
	}

	c.JSON(http.StatusOK, H{"code": 0})
}
