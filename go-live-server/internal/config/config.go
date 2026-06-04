package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration struct.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	SRS      SRSConfig      `yaml:"srs"`
	Database DatabaseConfig `yaml:"database"`
	Nginx    NginxConfig    `yaml:"nginx"`
	Auth     AuthConfig     `yaml:"auth"`
	Redis    RedisConfig    `yaml:"redis"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

// SRSConfig holds SRS connection info.
type SRSConfig struct {
	APIURL      string `yaml:"api_url"`
	RtmpBaseURL string `yaml:"rtmp_base_url"`
}

// DatabaseConfig holds PostgreSQL connection info.
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
	Timezone string `yaml:"timezone"`
}

// DSN builds a PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode, d.Timezone,
	)
}

// NginxConfig holds the base URL for HLS/FLV playback.
type NginxConfig struct {
	HlsBaseURL string `yaml:"hls_base_url"`
}

// AuthConfig holds JWT, HMAC, and admin login settings.
type AuthConfig struct {
	JWTSecret        string `yaml:"jwt_secret"`
	PushSecret       string `yaml:"push_secret"`
	PushTokenExpiry  string `yaml:"push_token_expiry"`
	PlaySecret       string `yaml:"play_secret"`
	PlayTokenExpiry  string `yaml:"play_token_expiry"`
	AdminUsername    string `yaml:"admin_username"`
	AdminPassword    string `yaml:"admin_password"`
	AdminTokenExpiry string `yaml:"admin_token_expiry"`
}

// PushExpiry parses the push token expiry duration.
func (a AuthConfig) PushExpiry() time.Duration {
	d, err := time.ParseDuration(a.PushTokenExpiry)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// PlayExpiry parses the play token expiry duration.
func (a AuthConfig) PlayExpiry() time.Duration {
	d, err := time.ParseDuration(a.PlayTokenExpiry)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// AdminExpiry parses the admin token expiry duration.
func (a AuthConfig) AdminExpiry() time.Duration {
	d, err := time.ParseDuration(a.AdminTokenExpiry)
	if err != nil || d <= 0 {
		return 8 * time.Hour
	}
	return d
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Host              string `yaml:"host"`
	Port              int    `yaml:"port"`
	Password          string `yaml:"password"`
	DB                int    `yaml:"db"`
	CacheTTL          string `yaml:"cache_ttl"`
	ViewerCountMethod string `yaml:"viewer_count_method"`
	PollingInterval   string `yaml:"polling_interval"`
}

// CacheDuration returns the Redis key TTL as a duration. Default 1 hour.
func (r RedisConfig) CacheDuration() time.Duration {
	d, err := time.ParseDuration(r.CacheTTL)
	if err != nil || d <= 0 {
		return 1 * time.Hour
	}
	return d
}

// PollDuration returns the polling interval. Default 60 seconds.
func (r RedisConfig) PollDuration() time.Duration {
	d, err := time.ParseDuration(r.PollingInterval)
	if err != nil || d <= 0 {
		return 60 * time.Second
	}
	return d
}

// Addr returns the Redis address string.
func (r RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 9090
	}
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "debug"
	}
	if cfg.Database.SSLMode == "" {
		cfg.Database.SSLMode = "disable"
	}
	if cfg.Database.Timezone == "" {
		cfg.Database.Timezone = "Asia/Shanghai"
	}
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = "change-me-in-production"
	}
	if cfg.Auth.PushSecret == "" {
		cfg.Auth.PushSecret = "change-me-push-secret"
	}
	if cfg.Auth.PlaySecret == "" {
		cfg.Auth.PlaySecret = "change-me-play-secret"
	}
	if cfg.Auth.AdminUsername == "" {
		cfg.Auth.AdminUsername = "admin"
	}
	if cfg.Auth.AdminPassword == "" {
		cfg.Auth.AdminPassword = "admin123"
	}
	if cfg.Redis.Host == "" {
		cfg.Redis.Host = "localhost"
	}
	if cfg.Redis.Port == 0 {
		cfg.Redis.Port = 6379
	}
	if cfg.Redis.ViewerCountMethod == "" {
		cfg.Redis.ViewerCountMethod = "callback"
	}

	return cfg, nil
}
