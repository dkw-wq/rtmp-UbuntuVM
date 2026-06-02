package main

import (
	"fmt"
	"log"
	"os"

	"go-live-server/internal/adapter"
	"go-live-server/internal/config"
	"go-live-server/internal/handler"
	"go-live-server/internal/service"
	"go-live-server/internal/store"

	"github.com/gin-gonic/gin"
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

	// ---- services ----
	streamSvc := service.NewStreamService(
		db,
		srsAPI,
		cfg.Nginx.HlsBaseURL,
		cfg.SRS.RtmpBaseURL,
	)

	// ---- handlers ----
	streamH := handler.NewStreamHandler(streamSvc)
	callbackH := handler.NewCallbackHandler(streamSvc)
	agentH := handler.NewAgentHandler(db)

	// ---- router ----
	r := gin.Default()

	api := r.Group("/api")
	{
		api.GET("/health", handler.HealthCheck)

		streams := api.Group("/streams")
		{
			streams.POST("", streamH.Create)
			streams.GET("", streamH.List)
			streams.GET("/:id", streamH.Get)
			streams.DELETE("/:id", streamH.Delete)
			streams.POST("/:id/start", streamH.Start)
			streams.POST("/:id/stop", streamH.Stop)
		}

		callback := api.Group("/callback")
		{
			callback.POST("/publish", callbackH.Publish)
			callback.POST("/unpublish", callbackH.Unpublish)
		}

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
