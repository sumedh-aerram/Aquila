package replay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxResponse = 64 << 10
	stepTimeout = 8 * time.Second
)

// Observation is one executed step against one target.
type Observation struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status,omitempty"`
	Body       []byte `json:"-"`
	DurationNS int64  `json:"duration_ns,omitempty"`
	Err        string `json:"error,omitempty"`
}

// Result is a full replay against one gateway. It is not a pass/fail verdict.
type Result struct {
	Target string        `json:"target"`
	Steps  []Observation `json:"steps"`
}

// Run executes w against target. It does not follow cross-host redirects.
func Run(ctx context.Context, target string, w Workload) (Result, error) {
	base, err := parseTarget(target)
	if err != nil {
		return Result{}, err
	}
	if len(w.Steps) == 0 {
		return Result{}, fmt.Errorf("replay: empty workload")
	}
	client := &http.Client{
		Timeout: stepTimeout,
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
	out := Result{Target: base, Steps: make([]Observation, 0, len(w.Steps))}
	for _, st := range w.Steps {
		out.Steps = append(out.Steps, runStep(ctx, client, base, st))
	}
	return out, nil
}

func runStep(ctx context.Context, client *http.Client, base string, st Step) Observation {
	obs := Observation{Method: st.Method, Path: st.Path}
	stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	url := base + st.Path
	req, err := http.NewRequestWithContext(stepCtx, st.Method, url, bytes.NewReader(st.Body))
	if err != nil {
		obs.Err = err.Error()
		return obs
	}
	if len(st.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	start := time.Now()
	resp, err := client.Do(req)
	obs.DurationNS = time.Since(start).Nanoseconds()
	if err != nil {
		obs.Err = err.Error()
		return obs
	}
	defer func() { _ = resp.Body.Close() }()
	obs.Status = resp.StatusCode
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		obs.Err = err.Error()
		return obs
	}
	if int64(len(raw)) > maxResponse {
		obs.Err = "response too large"
		return obs
	}
	obs.Body = raw
	return obs
}

// CheckTarget reports whether raw is an http or https gateway URL.
func CheckTarget(raw string) error {
	_, err := parseTarget(raw)
	return err
}

func parseTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("replay: empty target")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("replay: target: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("replay: target must be http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("replay: target missing host")
	}
	if u.User != nil {
		return "", fmt.Errorf("replay: target must not include userinfo")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}
