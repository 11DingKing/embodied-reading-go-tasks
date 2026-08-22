package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr         string
	DatabasePath       string
	TenantID           string
	TenantName         string
	BootstrapEmail     string
	BootstrapPassword  string
	SessionTTL         time.Duration
	SessionTouch       time.Duration
	WorkerPollInterval time.Duration
	WorkerLeaseTTL     time.Duration
	WorkerBatchSize    int
	LogLevel           string
	ShutdownTimeout    time.Duration
}

func Load() (Config, error) {
	config := Config{
		ListenAddr:         env("LISTEN_ADDR", ":8080"),
		DatabasePath:       env("DATABASE_PATH", "./data/embodied-reading.db"),
		TenantID:           env("TENANT_ID", "embodied-reading"),
		TenantName:         env("TENANT_NAME", "Embodied Reading Studio"),
		BootstrapEmail:     env("BOOTSTRAP_EMAIL", "coordinator@example.test"),
		BootstrapPassword:  env("BOOTSTRAP_PASSWORD", "change-this-bootstrap-password"),
		LogLevel:           env("LOG_LEVEL", "info"),
		SessionTTL:         12 * time.Hour,
		SessionTouch:       5 * time.Minute,
		WorkerPollInterval: 2 * time.Second,
		WorkerLeaseTTL:     30 * time.Second,
		WorkerBatchSize:    25,
		ShutdownTimeout:    15 * time.Second,
	}
	var err error
	if config.SessionTTL, err = durationEnv("SESSION_TTL", config.SessionTTL); err != nil {
		return Config{}, err
	}
	if config.SessionTouch, err = durationEnv("SESSION_TOUCH_INTERVAL", config.SessionTouch); err != nil {
		return Config{}, err
	}
	if config.WorkerPollInterval, err = durationEnv("WORKER_POLL_INTERVAL", config.WorkerPollInterval); err != nil {
		return Config{}, err
	}
	if config.WorkerLeaseTTL, err = durationEnv("WORKER_LEASE_TTL", config.WorkerLeaseTTL); err != nil {
		return Config{}, err
	}
	if config.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", config.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if config.WorkerBatchSize, err = intEnv("WORKER_BATCH_SIZE", config.WorkerBatchSize); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("LISTEN_ADDR must not be empty")
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		return fmt.Errorf("DATABASE_PATH must not be empty")
	}
	if c.TenantID == "" || c.TenantName == "" {
		return fmt.Errorf("TENANT_ID and TENANT_NAME must not be empty")
	}
	if !strings.Contains(c.BootstrapEmail, "@") || len(c.BootstrapPassword) < 12 {
		return fmt.Errorf("bootstrap credentials are invalid")
	}
	if c.SessionTTL <= 0 || c.SessionTouch <= 0 {
		return fmt.Errorf("session durations must be positive")
	}
	if c.WorkerPollInterval <= 0 || c.WorkerLeaseTTL <= c.WorkerPollInterval {
		return fmt.Errorf("worker lease must be longer than poll interval")
	}
	if c.WorkerBatchSize < 1 || c.WorkerBatchSize > 100 {
		return fmt.Errorf("WORKER_BATCH_SIZE must be between 1 and 100")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be positive")
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	return nil
}

func env(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

func intEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}
