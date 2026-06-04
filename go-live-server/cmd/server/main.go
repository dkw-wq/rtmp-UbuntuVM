package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"go-live-server/internal/adapter"
	"go-live-server/internal/cache"
	"go-live-server/internal/config"
	"go-live-server/internal/handler"
	"go-live-server/internal/metrics"
	"go-live-server/internal/middleware"
	"go-live-server/internal/service"
	"go-live-server/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// ---- config ----
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	gin.SetMode(cfg.Server.Mode)

	// ---- database ----
	db, err := store.New(cfg.Database)
	if err != nil {
		log.Fatalf("init database: %v", err)
	}

	// ---- adapters ----
	srsAPI := adapter.NewSRSAPI(cfg.SRS.APIURL)

	// ---- redis cache ----
	cacheClient, err := cache.New(cfg.Redis)
	if err != nil {
		log.Printf("[main] WARNING: redis unavailable, caching disabled: %v", err)
		cacheClient = nil
	} else {
		defer cacheClient.Close()
	}

	// ---- services ----
	streamSvc := service.NewStreamService(
		db,
		srsAPI,
		cacheClient,
		cfg.Nginx.HlsBaseURL,
		cfg.SRS.RtmpBaseURL,
		cfg.Auth.PushSecret,
		cfg.Auth.PushExpiry(),
		cfg.Auth.PlaySecret,
		cfg.Auth.PlayExpiry(),
		cfg.Auth.JWTSecret,
	)

	// ---- handlers ----
	streamH := handler.NewStreamHandler(streamSvc)
	callbackH := handler.NewCallbackHandler(streamSvc)
	agentH := handler.NewAgentHandler(db)
	authH := handler.NewAuthHandler(streamSvc, cfg.Auth)

	// ---- viewer polling (method B: SRS API) ----
	if cacheClient != nil && cfg.Redis.ViewerCountMethod == "polling" {
		go runViewerPolling(srsAPI, cacheClient, cfg.Redis.PollDuration())
	}

	// ---- metrics HTTP server (port 9091) ----
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("[metrics] serving on :9091")
		if err := http.ListenAndServe(":9091", mux); err != nil {
			log.Printf("[metrics] server error: %v", err)
		}
	}()

	// ---- router ----
	r := gin.Default()
	r.Use(metrics.HTTPMetrics())

	api := r.Group("/api")
	{
		// Public endpoints
		api.GET("/health", handler.HealthCheck)
		api.GET("/playback/:stream_key", authH.GetPlaybackInfo)

		// Auth endpoints (no JWT required)
		auth := api.Group("/auth")
		{
			auth.POST("/login", authH.Login)
			auth.GET("/play", authH.PlayAuth)
		}

		// Stream management — requires JWT admin token
		streams := api.Group("/streams")
		streams.Use(middleware.JWTAuth(cfg.Auth.JWTSecret))
		{
			streams.POST("", streamH.Create)
			streams.GET("", streamH.List)
			streams.GET("/:id", streamH.Get)
			streams.DELETE("/:id", streamH.Delete)
			streams.POST("/:id/start", streamH.Start)
			streams.POST("/:id/stop", streamH.Stop)
			streams.POST("/:id/refresh-token", authH.RefreshToken)
		}

		// SRS callbacks — no auth (called by SRS on localhost)
		callback := api.Group("/callback")
		{
			callback.POST("/publish", callbackH.Publish)
			callback.POST("/unpublish", callbackH.Unpublish)
			callback.POST("/play", callbackH.Play)
			callback.POST("/stop", callbackH.Stop)
		}

		// Agent endpoints — no JWT (agents identify by agent_id)
		agent := api.Group("/agent")
		{
			agent.GET("/tasks", agentH.GetTasks)
			agent.PUT("/tasks/:id", agentH.UpdateTask)
			agent.POST("/heartbeat", agentH.Heartbeat)
		}
	}

	// ---- start ----
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("[main] starting server on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runViewerPolling periodically fetches SRS client list and updates viewer counts in Redis.
// This is viewer counting method B (alternative to on_play/on_stop callbacks).
func runViewerPolling(srs *adapter.SRSAPI, ch *cache.Client, interval time.Duration) {
	log.Printf("[poll] viewer polling started, interval=%s", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		clients, err := srs.ListAllClients()
		if err != nil {
			log.Printf("[poll] srs client fetch error: %v", err)
			continue
		}

		// Count viewers per stream (play-type clients only)
		viewerCount := make(map[string]int64)
		for _, c := range clients {
			if c.Stream == "" {
				continue
			}
			// Count all non-publisher clients as viewers
			if c.Type != "RTMP publisher" {
				viewerCount[c.Stream]++
			}
		}

		ctx := context.Background()
		for streamKey, count := range viewerCount {
			if err := ch.SetViewers(ctx, streamKey, count); err != nil {
				log.Printf("[poll] set viewers error: stream=%s err=%v", streamKey, err)
			}
		}
	}
}
