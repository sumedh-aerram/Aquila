package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/sumedhaerram/aquila/internal/config"
)

// NewLogger builds a structured slog logger from Aquila log configuration.
func NewLogger(cfg config.LogConfig) *slog.Logger {
	return NewLoggerTo(os.Stdout, cfg)
}

// NewLoggerTo is the testable constructor.
func NewLoggerTo(w io.Writer, cfg config.LogConfig) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler).With("service", "aquila")
}
