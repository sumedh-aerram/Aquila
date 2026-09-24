package replay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadFileExpandsEnvHeaderAndReplaySendsIt(t *testing.T) {
	t.Setenv("AQUILA_TEST_PAT", "secret-token")
	p := writeWorkload(t, `{"steps":[{"method":"GET","path":"/user/me","headers":{"authorization":"Bearer ${AQUILA_TEST_PAT}","X-Device-Id":"dev-1","X-Pair":"${AQUILA_TEST_PAT}:${AQUILA_TEST_PAT}"}}]}`)
	w, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Steps[0].Headers.Get("X-Pair"); got != "secret-token:secret-token" {
		t.Fatalf("x-pair=%q", got)
	}
	if got := w.Steps[0].Headers.Get("Authorization"); got != "Bearer secret-token" {
		t.Fatalf("authorization=%q", got)
	}
	if names := HeaderNames(w.Steps[0]); strings.Join(names, ",") != "Authorization,X-Device-Id,X-Pair" {
		t.Fatalf("names=%v", names)
	}
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
	}))
	t.Cleanup(srv.Close)
	if _, err := Run(t.Context(), srv.URL, w); err != nil {
		t.Fatal(err)
	}
	if seen != "Bearer secret-token" {
		t.Fatalf("server saw %q", seen)
	}
}

func TestReadFileRejectsUnsafeHeaders(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"host", `{"steps":[{"method":"GET","path":"/","headers":{"Host":"evil.example"}}]}`},
		{"proxy", `{"steps":[{"method":"GET","path":"/","headers":{"Proxy-Authorization":"x"}}]}`},
		{"framing", `{"steps":[{"method":"GET","path":"/","headers":{"Transfer-Encoding":"chunked"}}]}`},
		{"crlf", `{"steps":[{"method":"GET","path":"/","headers":{"X-A":"a\r\nX-B: b"}}]}`},
		{"unset env", `{"steps":[{"method":"GET","path":"/","headers":{"Authorization":"${AQUILA_TEST_UNSET_VAR}"}}]}`},
		{"unset env inside value", `{"steps":[{"method":"GET","path":"/","headers":{"Authorization":"Bearer ${AQUILA_TEST_UNSET_VAR}"}}]}`},
		{"bad name", `{"steps":[{"method":"GET","path":"/","headers":{"X A":"1"}}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ReadFile(writeWorkload(t, tc.doc)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestHealthzPathProbesNamedRoute(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	if err := Healthz(t.Context(), srv.URL, srv.URL); err == nil {
		t.Fatal("default /healthz must fail on an app that serves /health")
	}
	if err := HealthzPath(t.Context(), srv.URL, srv.URL, "/health"); err != nil {
		t.Fatal(err)
	}
	if err := HealthzPath(t.Context(), srv.URL, srv.URL, "//evil.example/x/../"); err == nil {
		t.Fatal("unsafe health path must be rejected")
	}
}
