package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"go-live-server/internal/adapter"
	"go-live-server/internal/auth"
	"go-live-server/internal/cache"
	"go-live-server/internal/metrics"
	"go-live-server/internal/model"
	"go-live-server/internal/store"

	"github.com/google/uuid"
)

// StreamService contains all the business logic for streams.
type StreamService struct {
	db         *store.DB
	srs        *adapter.SRSAPI
	cache      *cache.Client
	nginxBase  string
	rtmpBase   string
	pushSecret string // HMAC secret for push URL signing
	pushExpiry time.Duration
	playSecret string // HMAC secret for play URL signing
	playExpiry time.Duration
	jwtSecret  string // JWT secret for admin API tokens
}

// NewStreamService creates a StreamService.
func NewStreamService(
	db *store.DB, srs *adapter.SRSAPI, ch *cache.Client,
	nginxBase, rtmpBase string,
	pushSecret string, pushExpiry time.Duration,
	playSecret string, playExpiry time.Duration,
	jwtSecret string,
) *StreamService {
	return &StreamService{
		db:         db,
		srs:        srs,
		cache:      ch,
		nginxBase:  nginxBase,
		rtmpBase:   rtmpBase,
		pushSecret: pushSecret,
		pushExpiry: pushExpiry,
		playSecret: playSecret,
		playExpiry: playExpiry,
		jwtSecret:  jwtSecret,
	}
}

// CreateStreamRequest is the payload for stream creation.
type CreateStreamRequest struct {
	ChannelID  string `json:"channel_id"`
	Protocol   string `json:"protocol"`
	Resolution string `json:"resolution"`
	Bitrate    string `json:"bitrate"`
}

// CreateStream generates a stream_key, publish token, signed play URL, and saves the stream.
// Write-through cache: saves to Redis alongside PostgreSQL.
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

	pushToken, err := auth.GeneratePublishToken()
	if err != nil {
		return nil, err
	}

	pushURL := fmt.Sprintf("%s/%s?token=%s", s.rtmpBase, streamKey, pushToken)
	hlsURL := s.generatePlayURL(streamKey, "m3u8")
	flvURL := s.generatePlayURL(streamKey, "flv")

	stream := &model.Stream{
		ChannelID:  channelID,
		StreamKey:  streamKey,
		Protocol:   protocol,
		Resolution: req.Resolution,
		Bitrate:    req.Bitrate,
		Status:     model.StatusCreated,
		PushToken:  pushToken,
		PushURL:    pushURL,
		HlsURL:     hlsURL,
		FlvURL:     flvURL,
	}

	if err := s.db.CreateStream(stream); err != nil {
		return nil, fmt.Errorf("create stream: %w", err)
	}

	// Write-through cache
	ctx := context.Background()
	s.cacheStatus(ctx, stream)
	s.cachePlayURLs(ctx, stream)

	log.Printf("[service] stream created: id=%s key=%s", stream.ID, streamKey)
	return stream, nil
}

// GetStream returns a single stream by ID. Tries Redis cache first, falls back to DB.
func (s *StreamService) GetStream(id string) (*model.Stream, error) {
	// Try cache first
	if s.cache != nil {
		ctx := context.Background()
		cached, err := s.cache.GetStreamStatus(ctx, id)
		if err == nil && cached != nil {
			stream := &model.Stream{
				ID:     id,
				Status: cached.Status,
			}
			return stream, nil
		}
	}
	// Fallback to DB
	return s.db.GetStreamByID(id)
}

// ListStreams returns streams, optionally filtered by status.
func (s *StreamService) ListStreams(status string) ([]model.Stream, error) {
	return s.db.ListStreams(status)
}

// DeleteStream stops publishing (if active), cleans cache, blacklists token, deletes from DB.
func (s *StreamService) DeleteStream(id string) error {
	stream, err := s.db.GetStreamByID(id)
	if err != nil {
		return err
	}

	if stream.Status == model.StatusPublishing {
		s.stopPublishing(stream)
	}

	// Cleanup Redis cache
	if s.cache != nil {
		ctx := context.Background()
		s.cache.DeleteStreamCache(ctx, id)
	}

	return s.db.DeleteStream(id)
}

// StartStream returns stream push information. Publishers can push directly with the stored push URL.
func (s *StreamService) StartStream(id string) (*model.Stream, error) {
	stream, err := s.db.GetStreamByID(id)
	if err != nil {
		return nil, err
	}

	if stream.Status == model.StatusPublishing {
		return nil, fmt.Errorf("stream is already publishing")
	}

	return stream, nil
}

// StopStream kicks the SRS publishing client when it is currently publishing.
func (s *StreamService) StopStream(id string) error {
	stream, err := s.db.GetStreamByID(id)
	if err != nil {
		return err
	}

	if stream.Status == model.StatusPublishing {
		s.stopPublishing(stream)
	}

	return nil
}

// RefreshPushToken generates a new long-lived publish token and updates the DB.
func (s *StreamService) RefreshPushToken(id string) (*model.Stream, error) {
	stream, err := s.db.GetStreamByID(id)
	if err != nil {
		return nil, fmt.Errorf("get stream: %w", err)
	}

	pushToken, err := auth.GeneratePublishToken()
	if err != nil {
		return nil, err
	}
	pushURL := fmt.Sprintf("%s/%s?token=%s", s.rtmpBase, stream.StreamKey, pushToken)

	if err := s.db.UpdateStreamPushAuth(id, pushToken, pushURL); err != nil {
		return nil, fmt.Errorf("update push token: %w", err)
	}

	stream.PushToken = pushToken
	stream.PushURL = pushURL

	log.Printf("[service] push token refreshed: id=%s", id)
	return stream, nil
}

// ValidatePushToken validates a publisher token against the token stored for the stream.
func (s *StreamService) ValidatePushToken(stream *model.Stream, token string) error {
	if err := auth.ValidatePublishToken(stream.PushToken, token); err != nil {
		metrics.AuthFailuresTotal.WithLabelValues("publish").Inc()
		return err
	}
	return nil
}

// OnPublish handles the SRS on_publish callback. Validates stream key + publish token, updates DB + cache.
func (s *StreamService) OnPublish(streamKey string, token string) error {
	stream, err := s.db.GetStreamByKey(streamKey)
	if err != nil {
		log.Printf("[service] on_publish: unknown stream key=%s", streamKey)
		metrics.AuthFailuresTotal.WithLabelValues("publish").Inc()
		return fmt.Errorf("unknown stream key")
	}

	if err := s.ValidatePushToken(stream, token); err != nil {
		log.Printf("[service] on_publish: push token invalid for key=%s: %v", streamKey, err)
		return err
	}

	if stream.Status == model.StatusPublishing {
		metrics.AuthFailuresTotal.WithLabelValues("publish").Inc()
		return fmt.Errorf("stream is already publishing")
	}

	now := time.Now()
	if err := s.db.UpdateStreamStatus(stream.ID, model.StatusPublishing, map[string]interface{}{
		"started_at": &now,
		"ended_at":   nil,
	}); err != nil {
		return err
	}

	// Update cache with publishing status, refresh TTL
	if s.cache != nil {
		ctx := context.Background()
		viewers, _ := s.cache.GetViewers(ctx, stream.ID)
		s.cache.SetStreamStatus(ctx, stream.ID, cache.StreamStatusData{
			Status:    model.StatusPublishing,
			Bitrate:   stream.Bitrate,
			Viewers:   viewers,
			StartedAt: now.Format(time.RFC3339),
		})
	}

	metrics.LiveStreamsActive.WithLabelValues(stream.ID).Set(1)
	metrics.LiveStreamBitrateKbps.WithLabelValues(stream.ID).Set(parseBitrateKbps(stream.Bitrate))

	return nil
}

// OnUnpublish handles the SRS on_unpublish callback. Updates DB + cache, resets viewers.
func (s *StreamService) OnUnpublish(streamKey string) error {
	stream, err := s.db.GetStreamByKey(streamKey)
	if err != nil {
		log.Printf("[service] on_unpublish: unknown stream key=%s", streamKey)
		return nil
	}

	now := time.Now()
	if err := s.db.UpdateStreamStatus(stream.ID, model.StatusEnded, map[string]interface{}{
		"ended_at": &now,
	}); err != nil {
		return err
	}

	// Update cache, reset viewers
	if s.cache != nil {
		ctx := context.Background()
		s.cache.SetStreamStatus(ctx, stream.ID, cache.StreamStatusData{
			Status:  model.StatusEnded,
			Bitrate: stream.Bitrate,
			Viewers: 0,
		})
		s.cache.ResetViewers(ctx, stream.ID)
	}

	// Metrics
	if stream.StartedAt != nil {
		duration := now.Sub(*stream.StartedAt).Seconds()
		metrics.StreamPublishDuration.WithLabelValues(stream.ID).Set(duration)
	}
	metrics.LiveStreamsActive.DeleteLabelValues(stream.ID)
	metrics.LiveViewersTotal.DeleteLabelValues(stream.ID)
	metrics.LiveStreamBitrateKbps.DeleteLabelValues(stream.ID)

	return nil
}

// OnPlay handles the SRS on_play callback — increments viewer count.
func (s *StreamService) OnPlay(streamKey string) {
	if s.cache == nil {
		return
	}
	stream, err := s.db.GetStreamByKey(streamKey)
	if err != nil {
		return
	}
	ctx := context.Background()
	count, err := s.cache.IncrViewers(ctx, stream.ID)
	if err != nil {
		log.Printf("[service] incr viewers error: %v", err)
		return
	}
	metrics.LiveViewersTotal.WithLabelValues(stream.ID).Set(float64(count))
	log.Printf("[service] viewer joined: id=%s viewers=%d", stream.ID, count)
}

// OnStop handles the SRS on_stop callback — decrements viewer count.
func (s *StreamService) OnStop(streamKey string) {
	if s.cache == nil {
		return
	}
	stream, err := s.db.GetStreamByKey(streamKey)
	if err != nil {
		return
	}
	ctx := context.Background()
	count, err := s.cache.DecrViewers(ctx, stream.ID)
	if err != nil {
		log.Printf("[service] decr viewers error: %v", err)
		return
	}
	metrics.LiveViewersTotal.WithLabelValues(stream.ID).Set(float64(count))
	log.Printf("[service] viewer left: id=%s viewers=%d", stream.ID, count)
}

// ValidatePlayAuth validates HMAC play URL signature and expiry.
func (s *StreamService) ValidatePlayAuth(streamKey string, expire int64, sign string) error {
	if expire <= time.Now().Unix() {
		return fmt.Errorf("play URL has expired")
	}
	return auth.ValidatePlaySign(streamKey, expire, sign, s.playSecret)
}

// GetPlaybackInfo looks up a stream by stream_key and returns fresh signed play URLs.
// This is a public endpoint — no JWT required. The URLs themselves have HMAC expiry.
func (s *StreamService) GetPlaybackInfo(streamKey string) (*model.Stream, error) {
	stream, err := s.db.GetStreamByKey(streamKey)
	if err != nil {
		return nil, fmt.Errorf("stream not found: %w", err)
	}
	// Return a copy with freshly-signed play URLs
	result := *stream
	result.HlsURL = s.generatePlayURL(stream.StreamKey, "m3u8")
	result.FlvURL = s.generatePlayURL(stream.StreamKey, "flv")
	result.PushURL = ""   // never leak push URL
	result.PushToken = "" // never leak push token
	return &result, nil
}

// GetStreamByKey returns a stream by its public stream_key.

// generatePlayURL creates a signed play URL with HMAC sign+expire query params.
func (s *StreamService) generatePlayURL(streamKey string, ext string) string {
	expire := time.Now().Add(s.playExpiry).Unix()
	sign := auth.GeneratePlaySign(streamKey, expire, s.playSecret)
	return fmt.Sprintf("%s/%s.%s?sign=%s&expire=%d", s.nginxBase, streamKey, ext, sign, expire)
}

// ---------- cache helpers ----------

func (s *StreamService) cacheStatus(ctx context.Context, stream *model.Stream) {
	if s.cache == nil {
		return
	}
	startedAt := ""
	if stream.StartedAt != nil {
		startedAt = stream.StartedAt.Format(time.RFC3339)
	}
	if err := s.cache.SetStreamStatus(ctx, stream.ID, cache.StreamStatusData{
		Status:    stream.Status,
		Bitrate:   stream.Bitrate,
		Viewers:   0,
		StartedAt: startedAt,
	}); err != nil {
		log.Printf("[service] cache status error: %v", err)
	}
}

func (s *StreamService) cachePlayURLs(ctx context.Context, stream *model.Stream) {
	if s.cache == nil {
		return
	}
	if err := s.cache.SetPlayURLs(ctx, stream.ID, cache.PlayURLData{
		HlsURL:    stream.HlsURL,
		FlvURL:    stream.FlvURL,
		WebRTCURL: stream.WebRTCURL,
	}); err != nil {
		log.Printf("[service] cache play urls error: %v", err)
	}
}

// ---------- internal helpers ----------

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

// parseBitrateKbps converts a bitrate string like "2000k" to a float64 kbps value.
// Returns 0 for empty or unparseable strings.
func parseBitrateKbps(s string) float64 {
	if s == "" {
		return 0
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "k"), "K")
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return val
}
