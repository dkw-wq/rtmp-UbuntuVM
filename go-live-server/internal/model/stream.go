package model

import (
	"time"
)

// Stream status constants
const (
	StatusCreated    = "created"
	StatusPublishing = "publishing"
	StatusEnded      = "ended"
	StatusError      = "error"
)

// Agent task status constants
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
)

// Agent task action constants
const (
	ActionStartPush = "start_push"
	ActionStopPush  = "stop_push"
)

// Channel represents a logical channel / room.
type Channel struct {
	ID          string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Name        string    `gorm:"size:255;not null"                  json:"name"`
	Description string    `gorm:"type:text"                          json:"description"`
	Status      string    `gorm:"size:20;default:inactive"           json:"status"`
	CreatedAt   time.Time `                                          json:"created_at"`
	UpdatedAt   time.Time `                                          json:"updated_at"`
}

// Stream represents a live stream session.
type Stream struct {
	ID         string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ChannelID  *string    `gorm:"type:uuid"                                       json:"channel_id,omitempty"`
	StreamKey  string     `gorm:"uniqueIndex;size:128;not null"                   json:"stream_key"`
	Protocol   string     `gorm:"size:20;default:rtmp"                            json:"protocol"`
	Resolution string     `gorm:"size:20"                                         json:"resolution,omitempty"`
	Bitrate    string     `gorm:"size:10"                                         json:"bitrate,omitempty"`
	Status     string     `gorm:"size:20;default:created"                         json:"status"`
	PushToken  string     `gorm:"type:text"                                       json:"push_token,omitempty"`
	PushURL    string     `gorm:"type:text"                                       json:"push_url,omitempty"`
	HlsURL     string     `gorm:"type:text"                                       json:"hls_url,omitempty"`
	FlvURL     string     `gorm:"type:text"                                       json:"flv_url,omitempty"`
	WebRTCURL  string     `gorm:"type:text;column:webrtc_url"                     json:"webrtc_url,omitempty"`
	StartedAt  *time.Time `                                                        json:"started_at,omitempty"`
	EndedAt    *time.Time `                                                        json:"ended_at,omitempty"`
	CreatedAt  time.Time  `                                                        json:"created_at"`
}

// Recording represents a recorded stream file.
type Recording struct {
	ID          string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StreamID    string     `gorm:"type:uuid;index"                                json:"stream_id"`
	FilePath    string     `gorm:"type:text;not null"                             json:"file_path"`
	FileSize    int64      `gorm:"default:0"                                      json:"file_size"`
	DurationSec int        `gorm:"default:0"                                      json:"duration_sec"`
	StartedAt   *time.Time `                                                        json:"started_at,omitempty"`
	EndedAt     *time.Time `                                                        json:"ended_at,omitempty"`
	CreatedAt   time.Time  `                                                        json:"created_at"`
}

// AgentTask represents a command dispatched to a Raspberry Pi agent.
type AgentTask struct {
	ID        string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StreamID  string    `gorm:"type:uuid;index"                                json:"stream_id"`
	AgentID   string    `gorm:"size:64;index"                                  json:"agent_id,omitempty"`
	Action    string    `gorm:"size:20;not null"                               json:"action"`
	Status    string    `gorm:"size:20;default:pending"                        json:"status"`
	StreamKey string    `gorm:"size:128;not null"                              json:"stream_key"`
	PushURL   string    `gorm:"type:text"                                      json:"push_url,omitempty"`
	ErrorMsg  string    `gorm:"type:text"                                      json:"error_msg,omitempty"`
	CreatedAt time.Time `                                                        json:"created_at"`
	UpdatedAt time.Time `                                                        json:"updated_at"`
}
