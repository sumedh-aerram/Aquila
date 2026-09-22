package fault

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHandlerInjectsStatusWithoutUpstream(t *testing.T) {
	t.Parallel()
	upstreamHits := 0
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits++
	}))
	t.Cleanup(up.Close)
	h, err := Handler(up.URL, Spec{Status: http.StatusBadGateway})
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(h)
	t.Cleanup(p.Close)
	resp, err := http.Get(p.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "injected") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if upstreamHits != 0 {
		t.Fatal("injected status must not call upstream")
	}
}

func TestHandlerDelayIsOnThePath(t *testing.T) {
	t.Parallel()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	t.Cleanup(up.Close)
	h, err := Handler(up.URL, Spec{Delay: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(h)
	t.Cleanup(p.Close)
	start := time.Now()
	resp, err := http.Get(p.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if time.Since(start) < 40*time.Millisecond {
		t.Fatal("delay was not applied")
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestHandlerProxiesWhenNoFault(t *testing.T) {
	t.Parallel()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/user-1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "user-1"})
	}))
	t.Cleanup(up.Close)
	h, err := Handler(up.URL, Spec{})
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(h)
	t.Cleanup(p.Close)
	resp, err := http.Get(p.URL + "/users/user-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "user-1") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestHandlerRejectsFileTarget(t *testing.T) {
	t.Parallel()
	if _, err := Handler("file:///etc/passwd", Spec{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestHandlerRejectsUserinfo(t *testing.T) {
	t.Parallel()
	if _, err := Handler("http://user:pass@127.0.0.1:9", Spec{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestHandlerRejectsLongDelay(t *testing.T) {
	t.Parallel()
	if _, err := Handler("http://127.0.0.1:18180", Spec{Delay: 31 * time.Second}); err == nil {
		t.Fatal("expected error")
	}
}

func TestHandlerInjectedStatusSkipsUpstreamAfterDelay(t *testing.T) {
	t.Parallel()
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	t.Cleanup(up.Close)
	h, err := Handler(up.URL, Spec{Delay: 5 * time.Millisecond, Status: http.StatusBadGateway})
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(h)
	t.Cleanup(p.Close)
	resp, err := http.Get(p.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if hits != 0 {
		t.Fatal("injected status must not call upstream")
	}
}

func TestHandlerProxiesPOSTBody(t *testing.T) {
	t.Parallel()
	var got []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	t.Cleanup(up.Close)
	h, err := Handler(up.URL, Spec{})
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(h)
	t.Cleanup(p.Close)
	resp, err := http.Post(p.URL+"/checkout", "application/json", strings.NewReader(`{"user_id":"user-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(got), "user-1") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, got)
	}
}

func TestHandlerUsesUpstreamHost(t *testing.T) {
	t.Parallel()
	var gotHost string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(up.Close)
	h, err := Handler(up.URL, Spec{})
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(h)
	t.Cleanup(p.Close)
	req, err := http.NewRequest(http.MethodGet, p.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	if gotHost != u.Host {
		t.Fatalf("host=%q want %q", gotHost, u.Host)
	}
}
