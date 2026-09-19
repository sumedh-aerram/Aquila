package envcfg

import (
	"os"
	"strings"
)

func Get(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
