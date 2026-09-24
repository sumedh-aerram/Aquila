package replay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sumedhaerram/aquila/internal/netguard"
)

const healthzTimeout = 3 * time.Second

// DefaultHealthPath is probed when the operator names no health route.
const DefaultHealthPath = "/healthz"

// Healthz checks GET /healthz on baseline and patch. It does not follow
// cross-host redirects and does not treat a 200 as a pass.
func Healthz(ctx context.Context, baseline, patch string) error {
	return HealthzPath(ctx, baseline, patch, DefaultHealthPath)
}

// HealthzPath is Healthz against an operator-named route such as /health.
func HealthzPath(ctx context.Context, baseline, patch, path string) error {
	if path == "" {
		path = DefaultHealthPath
	}
	if !safeReplay(http.MethodGet, path) {
		return fmt.Errorf("replay: health path %q is not a local GET path", path)
	}
	if err := oneHealthz(ctx, baseline, path); err != nil {
		return fmt.Errorf("replay: baseline GET %s: %w", path, err)
	}
	if err := oneHealthz(ctx, patch, path); err != nil {
		return fmt.Errorf("replay: patch GET %s: %w", path, err)
	}
	return nil
}

func oneHealthz(ctx context.Context, target, path string) error {
	base, err := parseTarget(target)
	if err != nil {
		return err
	}
	stepCtx, cancel := context.WithTimeout(ctx, healthzTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(stepCtx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout:   healthzTimeout,
		Transport: netguard.Transport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect to different host")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
