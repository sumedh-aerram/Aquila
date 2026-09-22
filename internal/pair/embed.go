package pair

import (
	"embed"
	"fmt"
	"os"
)

//go:embed compose.yaml otel.yaml
var bundled embed.FS

func writeEmbedded(dest, name string) error {
	raw, err := bundled.ReadFile(name)
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		return fmt.Errorf("pair: %w", err)
	}
	return nil
}
