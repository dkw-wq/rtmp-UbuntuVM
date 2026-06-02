package handler

import (
	"errors"
	"log"
	"net/http"

	"go-live-server/internal/model"
	"go-live-server/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// StreamHandler handles stream CRUD and lifecycle endpoints.
type StreamHandler struct {
	svc *service.StreamService
}

// NewStreamHandler creates a new StreamHandler.
func NewStreamHandler(svc *service.StreamService) *StreamHandler {
	return &StreamHandler{svc: svc}
}

// ----- request types -----

type createStreamReq struct {
	ChannelID  string `json:"channel_id"`
	Protocol   string `json:"protocol"`
	Resolution string `json:"resolution"`
	Bitrate    string `json:"bitrate"`
}

// ----- handlers -----

// Create handles POST /api/streams
func (h *StreamHandler) Create(c *gin.Context) {
	var req createStreamReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, CodeInvalidParam, "invalid request: "+err.Error())
		return
	}

	stream, err := h.svc.CreateStream(&service.CreateStreamRequest{
		ChannelID:  req.ChannelID,
		Protocol:   req.Protocol,
		Resolution: req.Resolution,
		Bitrate:    req.Bitrate,
	})
	if err != nil {
		log.Printf("[handler] create stream error: %v", err)
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to create stream")
		return
	}

	Success(c, stream)
}

// List handles GET /api/streams?status=xxx
func (h *StreamHandler) List(c *gin.Context) {
	status := c.Query("status")
	streams, err := h.svc.ListStreams(status)
	if err != nil {
		log.Printf("[handler] list streams error: %v", err)
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to list streams")
		return
	}

	if streams == nil {
		streams = []model.Stream{}
	}

	Success(c, streams)
}

// Get handles GET /api/streams/:id
func (h *StreamHandler) Get(c *gin.Context) {
	id := c.Param("id")

	stream, err := h.svc.GetStream(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, http.StatusNotFound, CodeNotFound, "stream not found")
			return
		}
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to get stream")
		return
	}

	Success(c, stream)
}

// Delete handles DELETE /api/streams/:id
func (h *StreamHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	_, err := h.svc.GetStream(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, http.StatusNotFound, CodeNotFound, "stream not found")
			return
		}
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to get stream")
		return
	}

	if err := h.svc.DeleteStream(id); err != nil {
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to delete stream")
		return
	}

	Success(c, nil)
}

// Start handles POST /api/streams/:id/start
func (h *StreamHandler) Start(c *gin.Context) {
	id := c.Param("id")

	if err := h.svc.StartStream(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, http.StatusNotFound, CodeNotFound, "stream not found")
			return
		}
		Error(c, http.StatusConflict, CodeConflict, err.Error())
		return
	}

	Success(c, H{"status": "start_requested"})
}

// Stop handles POST /api/streams/:id/stop
func (h *StreamHandler) Stop(c *gin.Context) {
	id := c.Param("id")

	if err := h.svc.StopStream(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, http.StatusNotFound, CodeNotFound, "stream not found")
			return
		}
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to stop stream")
		return
	}

	Success(c, H{"status": "stop_requested"})
}
