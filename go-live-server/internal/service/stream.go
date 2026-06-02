package service

import (
	"fmt"
	"log"
	"time"

	"go-live-server/internal/adapter"
	"go-live-server/internal/model"
	"go-live-server/internal/store"

	"github.com/google/uuid"
)

// StreamService contains all the business logic for streams.
type StreamService struct {
	db    *store.DB
	srs   *adapter.SRSAPI
	nginxBase string
	rtmpBase  string
}

// NewStreamService creates a StreamService.
func NewStreamService(db *store.DB, srs *adapter.SRSAPI, nginxBase, rtmpBase string) *StreamService {
	return &StreamService{
		db:        db,
		srs:       srs,
		nginxBase: nginxBase,
		rtmpBase:  rtmpBase,
	}
}

// CreateStreamRequest is the payload for stream creation.
type CreateStreamRequest struct {
	ChannelID  string `json:"channel_id"`
	Protocol   string `json:"protocol"`
	Resolution string `json:"resolution"`
	Bitrate    string `json:"bitrate"`
}

// CreateStream generates a stream_key, builds URLs, and saves the stream.
func (s *StreamService) CreateStream(req *CreateStreamRequest) (*model.Stream, error) {
	streamKey := uuid.New().String()

	protocol := req.Protocol
	if protocol == "" {
		protocol = "rtmp"
	}

	var channelID *string
	if req.ChannelID != "" {
		channelID = &req.ChannelID
	}

	stream := &model.Stream{
		ChannelID:  channelID,
		StreamKey:  streamKey,
		Protocol:   protocol,
		Resolution: req.Resolution,
		Bitrate:    req.Bitrate,
		Status:     model.StatusCreated,
		PushURL:    fmt.Sprintf("%s/%s", s.rtmpBase, streamKey),
		HlsURL:     fmt.Sprintf("%s/%s.m3u8", s.nginxBase, streamKey),
		FlvURL:     fmt.Sprintf("%s/%s.flv", s.nginxBase, streamKey),
	}

	if err := s.db.CreateStream(stream); err != nil {
		return nil, fmt.Errorf("create stream: %w", err)
	}

	log.Printf("[service] stream created: id=%s key=%s", stream.ID, streamKey)
	return stream, nil
}

// GetStream returns a single stream by ID.
func (s *StreamService) GetStream(id string) (*model.Stream, error) {
	return s.db.GetStreamByID(id)
}

// ListStreams returns streams, optionally filtered by status.
func (s *StreamService) ListStreams(status string) ([]model.Stream, error) {
	return s.db.ListStreams(status)
}

// DeleteStream stops publishing (if active) and deletes the stream.
func (s *StreamService) DeleteStream(id string) error {
	stream, err := s.db.GetStreamByID(id)
	if err != nil {
		return err
	}

	// If currently publishing, kick the SRS client first
	if stream.Status == model.StatusPublishing {
		s.stopPublishing(stream)
	}

	// Create a stop task so the agent can clean up FFmpeg
	s.createAgentTask(stream, model.ActionStopPush)

	return s.db.DeleteStream(id)
}

// StartStream creates an agent task to begin pushing.
func (s *StreamService) StartStream(id string) error {
	stream, err := s.db.GetStreamByID(id)
	if err != nil {
		return err
	}

	if stream.Status == model.StatusPublishing {
		return fmt.Errorf("stream is already publishing")
	}

	s.createAgentTask(stream, model.ActionStartPush)
	return nil
}

// StopStream kicks the SRS publishing client and creates a stop task.
func (s *StreamService) StopStream(id string) error {
	stream, err := s.db.GetStreamByID(id)
	if err != nil {
		return err
	}

	if stream.Status == model.StatusPublishing {
		s.stopPublishing(stream)
	}

	s.createAgentTask(stream, model.ActionStopPush)
	return nil
}

// OnPublish handles the SRS on_publish callback.
// It looks up the stream by stream_key and marks it as "publishing".
func (s *StreamService) OnPublish(streamKey string) error {
	stream, err := s.db.GetStreamByKey(streamKey)
	if err != nil {
		// Unknown stream key — not ours, but still acknowledge SRS
		log.Printf("[service] on_publish: unknown stream key=%s", streamKey)
		return nil
	}

	now := time.Now()
	return s.db.UpdateStreamStatus(stream.ID, model.StatusPublishing, map[string]interface{}{
		"started_at": &now,
	})
}

// OnUnpublish handles the SRS on_unpublish callback.
// It marks the stream as "ended".
func (s *StreamService) OnUnpublish(streamKey string) error {
	stream, err := s.db.GetStreamByKey(streamKey)
	if err != nil {
		log.Printf("[service] on_unpublish: unknown stream key=%s", streamKey)
		return nil
	}

	now := time.Now()
	return s.db.UpdateStreamStatus(stream.ID, model.StatusEnded, map[string]interface{}{
		"ended_at": &now,
	})
}

// ---------- internal helpers ----------

// stopPublishing finds and kicks the SRS RTMP publisher for this stream.
func (s *StreamService) stopPublishing(stream *model.Stream) {
	client, err := s.srs.FindPublishingClient(stream.StreamKey)
	if err != nil {
		log.Printf("[service] find srs client error: %v", err)
		return
	}
	if client == nil {
		log.Printf("[service] no srs publisher found for key=%s", stream.StreamKey)
		return
	}

	if err := s.srs.KickClient(client.ID); err != nil {
		log.Printf("[service] kick srs client error: %v", err)
		return
	}
	log.Printf("[service] kicked srs client: id=%s stream=%s", client.ID, stream.StreamKey)
}

// createAgentTask inserts a new agent task for the given action.
func (s *StreamService) createAgentTask(stream *model.Stream, action string) {
	task := &model.AgentTask{
		StreamID:  stream.ID,
		Action:    action,
		Status:    model.TaskStatusPending,
		StreamKey: stream.StreamKey,
		PushURL:   stream.PushURL,
	}

	if err := s.db.CreateTask(task); err != nil {
		log.Printf("[service] create agent task error: %v", err)
		return
	}
	log.Printf("[service] agent task created: id=%s action=%s", task.ID, action)
}
