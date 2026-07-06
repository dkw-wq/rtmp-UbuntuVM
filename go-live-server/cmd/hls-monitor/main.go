package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type config struct {
	listen     string
	interval   time.Duration
	goHealth   string
	srsAPI     string
	rtmpAddr   string
	hlsBase    string
	httpTimout time.Duration
}

type srsClient struct {
	Stream string `json:"stream"`
	Type   string `json:"type"`
}

type srsClientsData struct {
	Clients []srsClient `json:"clients"`
}

type srsResponse struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

var (
	componentUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "live_component_up",
			Help: "Component health from hls-monitor. 1 means healthy.",
		},
		[]string{"component"},
	)

	publishingStreams = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "hls_monitor_publishing_streams",
			Help: "Number of RTMP publisher streams discovered from SRS.",
		},
	)

	playlistUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hls_playlist_up",
			Help: "Whether the HLS playlist can be fetched and parsed.",
		},
		[]string{"stream_key"},
	)

	playlistSegments = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hls_playlist_segments",
			Help: "Number of media segments currently present in the HLS playlist.",
		},
		[]string{"stream_key"},
	)

	playlistTargetDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hls_playlist_target_duration_seconds",
			Help: "HLS playlist target duration in seconds.",
		},
		[]string{"stream_key"},
	)

	latestSegmentUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hls_latest_segment_up",
			Help: "Whether the latest HLS TS segment can be fetched.",
		},
		[]string{"stream_key"},
	)

	checkDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "hls_monitor_last_check_duration_seconds",
			Help: "Duration of the last monitor check in seconds.",
		},
	)

	lastSuccess = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "hls_monitor_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last completed monitor check.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		componentUp,
		publishingStreams,
		playlistUp,
		playlistSegments,
		playlistTargetDuration,
		latestSegmentUp,
		checkDuration,
		lastSuccess,
	)
}

func main() {
	cfg := parseFlags()
	client := &http.Client{Timeout: cfg.httpTimout}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	monitor := &monitorState{
		cfg:    cfg,
		client: client,
		seen:   map[string]struct{}{},
	}

	go monitor.loop(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("[hls-monitor] serving metrics on %s", cfg.listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.listen, "listen", "127.0.0.1:9093", "metrics listen address")
	flag.DurationVar(&cfg.interval, "interval", 15*time.Second, "check interval")
	flag.StringVar(&cfg.goHealth, "go-health", "http://127.0.0.1:9090/api/health", "Go API health URL")
	flag.StringVar(&cfg.srsAPI, "srs-api", "http://127.0.0.1:1985", "SRS API base URL")
	flag.StringVar(&cfg.rtmpAddr, "rtmp-addr", "127.0.0.1:1935", "RTMP TCP address")
	flag.StringVar(&cfg.hlsBase, "hls-base", "http://127.0.0.1:8082/live", "internal HLS base URL")
	flag.DurationVar(&cfg.httpTimout, "http-timeout", 5*time.Second, "HTTP request timeout")
	flag.Parse()
	return cfg
}

type monitorState struct {
	cfg    config
	client *http.Client
	mu     sync.Mutex
	seen   map[string]struct{}
}

func (m *monitorState) loop(ctx context.Context) {
	m.checkOnce(ctx)

	ticker := time.NewTicker(m.cfg.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkOnce(ctx)
		}
	}
}

func (m *monitorState) checkOnce(ctx context.Context) {
	start := time.Now()

	componentUp.WithLabelValues("go_api").Set(boolFloat(m.checkHTTP(ctx, m.cfg.goHealth)))
	componentUp.WithLabelValues("srs_api").Set(boolFloat(m.checkHTTP(ctx, strings.TrimRight(m.cfg.srsAPI, "/")+"/api/v1/versions")))
	componentUp.WithLabelValues("rtmp").Set(boolFloat(m.checkTCP(ctx, m.cfg.rtmpAddr)))

	streams, err := m.publishingStreamKeys(ctx)
	if err != nil {
		log.Printf("[hls-monitor] list SRS publishers: %v", err)
		componentUp.WithLabelValues("srs_publishers").Set(0)
		streams = nil
	} else {
		componentUp.WithLabelValues("srs_publishers").Set(1)
	}

	publishingStreams.Set(float64(len(streams)))
	m.resetMissingStreams(streams)

	for _, streamKey := range streams {
		if err := m.checkHLS(ctx, streamKey); err != nil {
			log.Printf("[hls-monitor] hls check failed stream=%s err=%v", streamKey, err)
			playlistUp.WithLabelValues(streamKey).Set(0)
			latestSegmentUp.WithLabelValues(streamKey).Set(0)
		}
	}

	checkDuration.Set(time.Since(start).Seconds())
	lastSuccess.Set(float64(time.Now().Unix()))
}

func (m *monitorState) checkHTTP(ctx context.Context, rawURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func (m *monitorState) checkTCP(ctx context.Context, addr string) bool {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *monitorState) publishingStreamKeys(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(m.cfg.srsAPI, "/")+"/api/v1/clients", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("srs clients status=%d", resp.StatusCode)
	}

	var out srsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("srs code=%d", out.Code)
	}

	var data srsClientsData
	if err := json.Unmarshal(out.Data, &data); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	for _, c := range data.Clients {
		if c.Type == "RTMP publisher" && c.Stream != "" {
			seen[c.Stream] = struct{}{}
		}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys, nil
}

func (m *monitorState) checkHLS(ctx context.Context, streamKey string) error {
	playlistURL := strings.TrimRight(m.cfg.hlsBase, "/") + "/" + url.PathEscape(streamKey) + ".m3u8"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playlistURL, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("playlist status=%d", resp.StatusCode)
	}

	segments, targetDuration, latestSegment, err := parsePlaylist(resp.Body)
	if err != nil {
		return err
	}

	playlistUp.WithLabelValues(streamKey).Set(1)
	playlistSegments.WithLabelValues(streamKey).Set(float64(segments))
	playlistTargetDuration.WithLabelValues(streamKey).Set(targetDuration)
	latestSegmentUp.WithLabelValues(streamKey).Set(boolFloat(m.checkHTTP(ctx, resolveReference(playlistURL, latestSegment))))

	m.markSeen(streamKey)
	return nil
}

func parsePlaylist(r io.Reader) (segments int, targetDuration float64, latestSegment string, err error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			value := strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:")
			if f, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
				targetDuration = f
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		segments++
		latestSegment = line
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, "", err
	}
	if segments == 0 {
		return 0, targetDuration, "", fmt.Errorf("playlist contains no media segments")
	}
	return segments, targetDuration, latestSegment, nil
}

func resolveReference(baseURL, ref string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return u.ResolveReference(r).String()
}

func (m *monitorState) markSeen(streamKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen[streamKey] = struct{}{}
}

func (m *monitorState) resetMissingStreams(current []string) {
	currentSet := make(map[string]struct{}, len(current))
	for _, k := range current {
		currentSet[k] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for streamKey := range m.seen {
		if _, ok := currentSet[streamKey]; ok {
			continue
		}
		playlistUp.WithLabelValues(streamKey).Set(0)
		playlistSegments.WithLabelValues(streamKey).Set(0)
		playlistTargetDuration.WithLabelValues(streamKey).Set(0)
		latestSegmentUp.WithLabelValues(streamKey).Set(0)
		delete(m.seen, streamKey)
	}
}

func boolFloat(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}
