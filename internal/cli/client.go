package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAPI     = "http://127.0.0.1:8080"
	defaultTimeout = 10 * time.Second
	maxErrorBody   = 512
	maxTraces      = 200
	defaultTraces  = 20
)

// Client reads the local Aquila HTTP API.
type Client struct {
	base string
	http *http.Client
}

func newClient(raw string) (*Client, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultAPI
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("cli: api url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("cli: api url must be http or https")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("cli: api url missing host")
	}
	return &Client{
		base: strings.TrimRight(raw, "/"),
		http: &http.Client{Timeout: defaultTimeout},
	}, nil
}

func envAPI() string {
	if v := strings.TrimSpace(os.Getenv("AQUILA_API_URL")); v != "" {
		return v
	}
	return defaultAPI
}

type httpStatusError struct {
	method string
	path   string
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("cli: %s %s: status %d: %s", e.method, e.path, e.status, e.body)
	}
	return fmt.Sprintf("cli: %s %s: status %d", e.method, e.path, e.status)
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cli: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &httpStatusError{method: http.MethodGet, path: path, status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("cli: decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path, contentType string, body []byte, dest any) error {
	if contentType == "" {
		contentType = "text/plain"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cli: POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &httpStatusError{method: http.MethodPost, path: path, status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("cli: decode %s: %w", path, err)
	}
	return nil
}

func clipTraces(n int) int {
	if n <= 0 {
		return defaultTraces
	}
	if n > maxTraces {
		return maxTraces
	}
	return n
}

func unavailable(err error) bool {
	var hs *httpStatusError
	return errors.As(err, &hs) && hs.status == http.StatusServiceUnavailable
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
