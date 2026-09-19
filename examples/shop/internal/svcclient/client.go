package svcclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const maxResp = 1 << 20

// Shared returns a connection-pooling, traced client with timeouts.
func Shared() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: otelhttp.NewTransport(&http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 16,
			IdleConnTimeout:     90 * time.Second,
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// NewEphemeral is the intentional payment defect: a new client and transport
// per call, so TCP connections are not reused across authorize requests.
func NewEphemeral() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: otelhttp.NewTransport(&http.Transport{
			DisableKeepAlives: true,
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func GetJSON(ctx context.Context, client *http.Client, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	return doJSON(client, req, dest)
}

func PostJSON(ctx context.Context, client *http.Client, rawURL string, payload, dest any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doJSON(client, req, dest)
}

func doJSON(client *http.Client, req *http.Request, dest any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	limited := io.LimitReader(resp.Body, maxResp)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upstream %s: status %d", req.URL.Path, resp.StatusCode)
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(raw, dest)
}
