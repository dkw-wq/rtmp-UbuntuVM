package handler

import (
	"net/http"

	"go-live-server/internal/metrics"
	"go-live-server/internal/model"
	"go-live-server/internal/store"

	"github.com/gin-gonic/gin"
)

// AgentHandler handles communication with Raspberry Pi agents.
type AgentHandler struct {
	db *store.DB
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(db *store.DB) *AgentHandler {
	return &AgentHandler{db: db}
}

// ----- request / response types -----

type taskUpdateReq struct {
	Status   string `json:"status" binding:"required"`
	ErrorMsg string `json:"error_msg"`
}

type heartbeatReq struct {
	AgentID string `json:"agent_id" binding:"required"`
	Version string `json:"version"`
	Status  string `json:"status"` // online / busy / offline
}

// ----- handlers -----

// GetTasks returns pending tasks for the agent.
// Query parameter: ?agent_id=xxx  (optional, filters by agent)
func (h *AgentHandler) GetTasks(c *gin.Context) {
	agentID := c.Query("agent_id")
	tasks, err := h.db.GetPendingTasks(agentID)
	if err != nil {
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to fetch tasks")
		return
	}

	if tasks == nil {
		tasks = []model.AgentTask{}
	}

	// Mark fetched tasks as "running"
	for i := range tasks {
		_ = h.db.UpdateTaskStatus(tasks[i].ID, model.TaskStatusRunning, "")
	}

	Success(c, H{"tasks": tasks})
}

// Heartbeat receives periodic heartbeat from an agent.
func (h *AgentHandler) Heartbeat(c *gin.Context) {
	var req heartbeatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, CodeInvalidParam, "invalid request: "+err.Error())
		return
	}

	// Track FFmpeg processes via agent heartbeat
	if req.Status == "busy" {
		metrics.FfmpegProcessesRunning.Inc()
	} else {
		metrics.FfmpegProcessesRunning.Dec()
	}

	Success(c, H{
		"agent_id": req.AgentID,
	})
}

// UpdateTask lets the agent report task completion.
// PUT /api/agent/tasks/:id
func (h *AgentHandler) UpdateTask(c *gin.Context) {
	taskID := c.Param("id")

	var req taskUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, CodeInvalidParam, "invalid request: "+err.Error())
		return
	}

	if err := h.db.UpdateTaskStatus(taskID, req.Status, req.ErrorMsg); err != nil {
		Error(c, http.StatusInternalServerError, CodeInternal, "failed to update task")
		return
	}

	// Track FFmpeg restarts on failed tasks
	if req.Status == model.TaskStatusFailed {
		task, err := h.db.GetTaskByID(taskID)
		if err == nil && task != nil {
			metrics.FfmpegRestartTotal.WithLabelValues(task.StreamID).Inc()
		}
	}

	Success(c, nil)
}
