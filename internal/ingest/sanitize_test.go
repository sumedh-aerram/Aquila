package ingest

import (
	"strings"
	"testing"
)

func TestSanitizeRouteStripsUserinfo(t *testing.T) {
	t.Parallel()
	got := sanitizeRoute("https://alice:hunter2@example.com/v1/orders?x=1#y")
	if got != "/v1/orders" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "hunter2") || strings.Contains(got, "alice") {
		t.Fatal("userinfo leaked")
	}
}

func TestSanitizeNameKeepsMethod(t *testing.T) {
	t.Parallel()
	got := sanitizeName("POST https://u:p@h/checkout?q=1")
	if got != "POST /checkout" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeFileRejectsTraversal(t *testing.T) {
	t.Parallel()
	if sanitizeFile("../../etc/passwd") != "" {
		t.Fatal("expected empty")
	}
	if sanitizeFile("internal/payment/handler.go") != "internal/payment/handler.go" {
		t.Fatal("relative source path must be kept")
	}
}

func TestClipRunesDoesNotSplitUTF8(t *testing.T) {
	t.Parallel()
	s := clipRunes("hé", 1)
	if s != "h" {
		t.Fatalf("got %q", s)
	}
	if strings.ToValidUTF8(s, "") != s {
		t.Fatal("split rune")
	}
}
