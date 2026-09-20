package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultServerAddr       = ":8080"
	DefaultShutdownTimeout  = 10 * time.Second
	DefaultLogLevel         = "info"
	DefaultLogFormat        = "json"
	DefaultPostgresURL      = "postgres://aquila:aquila@127.0.0.1:15432/aquila?sslmode=disable"
	DefaultPostgresMaxConns = int32(8)
	DefaultConnectTimeout   = 5 * time.Second
	DefaultReadTimeout      = 15 * time.Second
	DefaultWriteTimeout     = 30 * time.Second
	DefaultIdleTimeout      = 60 * time.Second
)

// Config is Aquila control-plane configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Log      LogConfig      `yaml:"log"`
	Postgres PostgresConfig `yaml:"postgres"`
	Ingest   IngestConfig   `yaml:"ingest"`
}

type ServerConfig struct {
	Addr            string        `yaml:"addr"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type IngestConfig struct {
	Token string `yaml:"token"`
}

type PostgresConfig struct {
	URL            string        `yaml:"url"`
	MaxConns       int32         `yaml:"max_conns"`
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
}

// Load reads configuration from optional YAML, then environment overrides.
func Load() (Config, error) {
	return LoadFrom(os.Getenv("AQUILA_CONFIG"))
}

// LoadFrom loads defaults, an optional YAML file, then process environment.
func LoadFrom(path string) (Config, error) {
	cfg := defaults()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %q: %w", path, err)
		}
	}
	applyEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Addr:            DefaultServerAddr,
			ShutdownTimeout: DefaultShutdownTimeout,
			ReadTimeout:     DefaultReadTimeout,
			WriteTimeout:    DefaultWriteTimeout,
			IdleTimeout:     DefaultIdleTimeout,
		},
		Log: LogConfig{
			Level:  DefaultLogLevel,
			Format: DefaultLogFormat,
		},
		Postgres: PostgresConfig{
			URL:            DefaultPostgresURL,
			MaxConns:       DefaultPostgresMaxConns,
			ConnectTimeout: DefaultConnectTimeout,
		},
	}
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("AQUILA_SERVER_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("AQUILA_SHUTDOWN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.ShutdownTimeout = d
		}
	}
	if v := os.Getenv("AQUILA_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("AQUILA_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v := os.Getenv("AQUILA_POSTGRES_URL"); v != "" {
		cfg.Postgres.URL = v
	}
	if v := os.Getenv("AQUILA_POSTGRES_MAX_CONNS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil && n > 0 {
			cfg.Postgres.MaxConns = int32(n)
		}
	}
	if v := os.Getenv("AQUILA_POSTGRES_CONNECT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Postgres.ConnectTimeout = d
		}
	}
	if v := os.Getenv("AQUILA_INGEST_TOKEN"); v != "" {
		cfg.Ingest.Token = v
	}
}

// Validate checks required fields and enumerations.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Server.Addr) == "" {
		return fmt.Errorf("server.addr is required")
	}
	if c.Server.ShutdownTimeout <= 0 {
		return fmt.Errorf("server.shutdown_timeout must be positive")
	}
	if c.Server.ReadTimeout <= 0 {
		return fmt.Errorf("server.read_timeout must be positive")
	}
	if c.Server.WriteTimeout <= 0 {
		return fmt.Errorf("server.write_timeout must be positive")
	}
	if c.Server.IdleTimeout <= 0 {
		return fmt.Errorf("server.idle_timeout must be positive")
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be debug, info, warn, or error")
	}
	switch strings.ToLower(c.Log.Format) {
	case "json", "text":
	default:
		return fmt.Errorf("log.format must be json or text")
	}
	if strings.TrimSpace(c.Postgres.URL) == "" {
		return fmt.Errorf("postgres.url is required")
	}
	if c.Postgres.MaxConns <= 0 {
		return fmt.Errorf("postgres.max_conns must be positive")
	}
	if c.Postgres.ConnectTimeout <= 0 {
		return fmt.Errorf("postgres.connect_timeout must be positive")
	}
	return nil
}
