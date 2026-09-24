package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw string
		ok  bool
	}{
		{"http://127.0.0.1:18180", true},
		{"http://10.0.0.5:8080", true},
		{"http://gateway:8080", true},
		{"http://169.254.169.254/latest/meta-data/", false},
		{"http://[fe80::1]:80", false},
		{"http://0.0.0.0:80", false},
		{"http://metadata.google.internal/computeMetadata/v1/", false},
		{"http://user:pass@127.0.0.1:18180", false},
		{"file:///etc/passwd", false},
		{"http://[::ffff:169.254.169.254]/", false},
	}
	for _, tc := range tests {
		if err := CheckURL(tc.raw); (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", tc.raw, err)
		}
	}
}

func TestTransportAllowsLoopback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	resp, err := (&http.Client{Transport: Transport()}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestTransportRefusesLinkLocalAtDial(t *testing.T) {
	t.Parallel()
	_, err := (&http.Client{Transport: Transport()}).Get("http://169.254.169.254/")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
}
