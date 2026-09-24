package fault

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/netguard"
)

const (
	maxCopy  = 64 << 10
	maxDelay = 30 * time.Second
)

// Spec is an injected delay and/or status. Status 0 means proxy the request.
type Spec struct {
	Delay  time.Duration
	Status int
}

// Handler returns a reverse proxy that delays, then either injects Status or
// forwards to target. It does not follow cross-host redirects.
func Handler(target string, spec Spec) (http.Handler, error) {
	raw := strings.TrimSpace(target)
	if raw == "" {
		return nil, fmt.Errorf("fault: empty target")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("fault: target: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("fault: target must be http or https")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("fault: target missing host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("fault: target must not include userinfo")
	}
	if spec.Delay < 0 {
		return nil, fmt.Errorf("fault: delay must be >= 0")
	}
	if spec.Delay > maxDelay {
		return nil, fmt.Errorf("fault: delay exceeds 30s")
	}
	if spec.Status < 0 || spec.Status > 599 {
		return nil, fmt.Errorf("fault: status out of range")
	}
	if err := netguard.CheckURL(u.String()); err != nil {
		return nil, fmt.Errorf("fault: %w", err)
	}
	client := &http.Client{
		Timeout:   8 * time.Second,
		Transport: netguard.Transport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Host != u.Host {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if spec.Delay > 0 {
			timer := time.NewTimer(spec.Delay)
			select {
			case <-r.Context().Done():
				timer.Stop()
				http.Error(w, "injected delay canceled", http.StatusGatewayTimeout)
				return
			case <-timer.C:
			}
		}
		if spec.Status > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(spec.Status)
			_, _ = io.WriteString(w, `{"error":"injected"}`)
			return
		}
		out := *r.URL
		out.Scheme = u.Scheme
		out.Host = u.Host
		out.User = nil
		out.Fragment = ""
		req, err := http.NewRequestWithContext(r.Context(), r.Method, out.String(), r.Body)
		if err != nil {
			http.Error(w, "fault proxy", http.StatusBadGateway)
			return
		}
		req.Host = u.Host
		copyHeader(req.Header, r.Header)
		req.Header.Del("Host")
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, maxCopy))
	}), nil
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		if skipHop(k) {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func skipHop(k string) bool {
	switch http.CanonicalHeaderKey(k) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}
