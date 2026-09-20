package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"AQUILA_SERVER_ADDR",
		"AQUILA_SHUTDOWN_TIMEOUT",
		"AQUILA_LOG_LEVEL",
		"AQUILA_LOG_FORMAT",
		"AQUILA_POSTGRES_URL",
		"AQUILA_POSTGRES_MAX_CONNS",
		"AQUILA_POSTGRES_CONNECT_TIMEOUT",
		"AQUILA_INGEST_TOKEN",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func TestLoadFromDefaults(t *testing.T) {
	clearConfigEnv(t)
	cfg, err := LoadFrom("")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Server.Addr != DefaultServerAddr {
		t.Fatalf("addr = %q, want %q", cfg.Server.Addr, DefaultServerAddr)
	}
	if cfg.Log.Format != DefaultLogFormat {
		t.Fatalf("format = %q, want %q", cfg.Log.Format, DefaultLogFormat)
	}
}

func TestLoadFromYAMLAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	contents := []byte(`
server:
  addr: ":9090"
  shutdown_timeout: 3s
log:
  level: debug
  format: text
postgres:
  url: postgres://example/db
  max_conns: 4
  connect_timeout: 2s
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	clearConfigEnv(t)
	t.Setenv("AQUILA_SERVER_ADDR", ":7070")
	t.Setenv("AQUILA_LOG_LEVEL", "warn")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Server.Addr != ":7070" {
		t.Fatalf("env override failed: addr = %q", cfg.Server.Addr)
	}
	if cfg.Log.Level != "warn" {
		t.Fatalf("env override failed: level = %q", cfg.Log.Level)
	}
	if cfg.Server.ShutdownTimeout != 3*time.Second {
		t.Fatalf("yaml duration = %s", cfg.Server.ShutdownTimeout)
	}
	if cfg.Postgres.MaxConns != 4 {
		t.Fatalf("max_conns = %d", cfg.Postgres.MaxConns)
	}
	if cfg.Server.ReadTimeout != DefaultReadTimeout {
		t.Fatalf("read_timeout = %s", cfg.Server.ReadTimeout)
	}
}

func TestLoadIngestTokenFromEnv(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("AQUILA_INGEST_TOKEN", "local-demo")
	cfg, err := LoadFrom("")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Ingest.Token != "local-demo" {
		t.Fatalf("token = %q", cfg.Ingest.Token)
	}
}

func TestValidateRejectsUnknownLogLevel(t *testing.T) {
	t.Parallel()
	cfg := defaults()
	cfg.Log.Level = "verbose"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
