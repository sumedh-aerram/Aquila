package replay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const healthzTimeout = 3 * time.Second

// Healthz checks GET /healthz on baseline and patch. It does not follow
// cross-host redirects and does not treat a 200 as a pass.
func Healthz(ctx context.Context, baseline, patch string) error {
	if err := oneHealthz(ctx, baseline); err != nil {
		return fmt.Errorf("replay: baseline healthz: %w", err)
	}
	if err := oneHealthz(ctx, patch); err != nil {
		return fmt.Errorf("replay: patch healthz: %w", err)
	}
	return nil
}

func oneHealthz(ctx context.Context, target string) error {
	base, err := parseTarget(target)
	if err != nil {
		return err
	}
	stepCtx, cancel := context.WithTimeout(ctx, healthzTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(stepCtx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: healthzTimeout,
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
