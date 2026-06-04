package store

import (
	"fmt"
	"log"
	"time"

	"go-live-server/internal/config"
	"go-live-server/internal/metrics"
	"go-live-server/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database instance.
type DB struct {
	*gorm.DB
}

// New opens a PostgreSQL connection, configures the pool, and runs auto-migration.
func New(cfg config.DatabaseConfig) (*DB, error) {
	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	conn, err := gorm.Open(postgres.Open(cfg.DSN()), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	sqlDB, err := conn.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// ---- DB metrics: query timing via GORM callbacks ----
	startCb := func(_ *gorm.DB) {}
	endCb := func(_ *gorm.DB) {}
	startCb = func(d *gorm.DB) { d.InstanceSet("metrics:start", time.Now()) }
	endCb = func(d *gorm.DB) {
		if start, ok := d.InstanceGet("metrics:start"); ok {
			metrics.DbQueryDuration.Observe(time.Since(start.(time.Time)).Seconds())
		}
	}
	conn.Callback().Query().Before("*").Register("metrics:query_start", startCb)
	conn.Callback().Query().After("*").Register("metrics:query_end", endCb)
	conn.Callback().Create().Before("*").Register("metrics:create_start", startCb)
	conn.Callback().Create().After("*").Register("metrics:create_end", endCb)
	conn.Callback().Update().Before("*").Register("metrics:update_start", startCb)
	conn.Callback().Update().After("*").Register("metrics:update_end", endCb)
	conn.Callback().Delete().Before("*").Register("metrics:delete_start", startCb)
	conn.Callback().Delete().After("*").Register("metrics:delete_end", endCb)
	conn.Callback().Row().Before("*").Register("metrics:row_start", startCb)
	conn.Callback().Row().After("*").Register("metrics:row_end", endCb)

	// ---- DB metrics: connection pool stats ----
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			stats := sqlDB.Stats()
			metrics.DbConnectionsOpen.Set(float64(stats.OpenConnections))
		}
	}()

	// auto-migrate
	if err := conn.AutoMigrate(
		&model.Channel{},
		&model.Stream{},
		&model.Recording{},
		&model.AgentTask{},
	); err != nil {
		return nil, fmt.Errorf("auto-migrate: %w", err)
	}

	log.Println("[store] database connected and migrated")
	return &DB{conn}, nil
}

// ---------- Stream operations ----------

// CreateStream inserts a new stream.
func (db *DB) CreateStream(s *model.Stream) error {
	return db.Create(s).Error
}

// GetStreamByID fetches a stream by primary key.
func (db *DB) GetStreamByID(id string) (*model.Stream, error) {
	var s model.Stream
	if err := db.First(&s, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// GetStreamByKey fetches a stream by its stream_key.
func (db *DB) GetStreamByKey(key string) (*model.Stream, error) {
	var s model.Stream
	if err := db.First(&s, "stream_key = ?", key).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// ListStreams returns all streams, optionally filtered by status.
func (db *DB) ListStreams(status string) ([]model.Stream, error) {
	var streams []model.Stream
	q := db.Model(&model.Stream{}).Order("created_at DESC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Find(&streams).Error; err != nil {
		return nil, err
	}
	return streams, nil
}

// UpdateStreamStatus updates the status column (and optional extra fields).
func (db *DB) UpdateStreamStatus(id string, status string, extra map[string]interface{}) error {
	updates := map[string]interface{}{"status": status}
	for k, v := range extra {
		updates[k] = v
	}
	return db.Model(&model.Stream{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateStreamToken updates the push_token column for a stream.
func (db *DB) UpdateStreamToken(id string, token string) error {
	return db.Model(&model.Stream{}).Where("id = ?", id).
		Update("push_token", token).Error
}

// DeleteStream removes a stream by ID.
func (db *DB) DeleteStream(id string) error {
	return db.Delete(&model.Stream{}, "id = ?", id).Error
}

// ---------- Agent task operations ----------

// CreateTask inserts a new agent task.
func (db *DB) CreateTask(t *model.AgentTask) error {
	return db.Create(t).Error
}

// GetPendingTasks returns tasks with status "pending" (optionally for a specific agent).
func (db *DB) GetPendingTasks(agentID string) ([]model.AgentTask, error) {
	var tasks []model.AgentTask
	q := db.Where("status = ?", model.TaskStatusPending).Order("created_at ASC")
	if agentID != "" {
		q = q.Where("agent_id = ?", agentID)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// UpdateTaskStatus updates a task's status (and optional error message).
func (db *DB) UpdateTaskStatus(id string, status string, errMsg string) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}
	if errMsg != "" {
		updates["error_msg"] = errMsg
	}
	return db.Model(&model.AgentTask{}).Where("id = ?", id).Updates(updates).Error
}

// GetTaskByID fetches a single task.
func (db *DB) GetTaskByID(id string) (*model.AgentTask, error) {
	var t model.AgentTask
	if err := db.First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}
