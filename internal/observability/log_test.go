package observability

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/config"
)

func TestNewLoggerJSONIncludesService(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewLoggerTo(&buf, config.LogConfig{Level: "info", Format: "json"})
	log.Info("control plane starting")
	out := buf.String()
	if !strings.Contains(out, `"service":"aquila"`) {
		t.Fatalf("expected service attribute, got %s", out)
	}
	if !strings.Contains(out, "control plane starting") {
		t.Fatalf("expected message, got %s", out)
	}
}
