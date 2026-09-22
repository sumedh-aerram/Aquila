package diff

import (
	"strings"
	"testing"
)

func TestParseModifyHunkLines(t *testing.T) {
	t.Parallel()
	raw := []byte(`diff --git a/internal/payment/handler.go b/internal/payment/handler.go
--- a/internal/payment/handler.go
+++ b/internal/payment/handler.go
@@ -142,4 +142,4 @@ func (h *Handler) chargeProcessor(ctx context.Context, req authorizeReq) error {
 	// comment
-	client := svcclient.NewEphemeral()
+	client := svcclient.Shared()
 	return nil
`)
	d, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 {
		t.Fatalf("%+v", d)
	}
	f := d.Files[0]
	if f.Path != "internal/payment/handler.go" || f.Status != "modify" {
		t.Fatalf("%+v", f)
	}
	if len(f.Lines) == 0 {
		t.Fatal("expected changed lines")
	}
}

func TestParseStripsShopPrefix(t *testing.T) {
	t.Parallel()
	raw := []byte(`--- a/examples/shop/internal/users/store.go
+++ b/examples/shop/internal/users/store.go
@@ -1,1 +1,2 @@
 package users
+// x
`)
	d, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Files[0].Path != "internal/users/store.go" {
		t.Fatalf("%q", d.Files[0].Path)
	}
}

func TestParseRejectsTraversal(t *testing.T) {
	t.Parallel()
	raw := []byte(`--- a/../../etc/passwd
+++ b/../../etc/passwd
@@ -1 +1 @@
-a
+b
`)
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRejectsNonUTF8(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte{0xff, 0xfe, 'a'}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRejectsOversized(t *testing.T) {
	t.Parallel()
	if _, err := Parse(make([]byte, MaxBytes+1)); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseBinaryDoesNotRecordLines(t *testing.T) {
	t.Parallel()
	raw := []byte(`diff --git a/logo.png b/logo.png
Binary files a/logo.png and b/logo.png differ
`)
	d, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Files[0].Binary || len(d.Files[0].Lines) != 0 {
		t.Fatalf("%+v", d.Files[0])
	}
}

func TestParseEmpty(t *testing.T) {
	t.Parallel()
	d, err := Parse(nil)
	if err != nil || len(d.Files) != 0 {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestParseDoesNotKeepHunkText(t *testing.T) {
	t.Parallel()
	secret := "super-secret-token"
	raw := []byte(`--- a/internal/payment/handler.go
+++ b/internal/payment/handler.go
@@ -1,2 +1,2 @@
-` + secret + `
+ok
`)
	d, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(stringify(d), " ")
	if strings.Contains(got, secret) {
		t.Fatal("diff parser retained hunk body")
	}
}

func stringify(d Diff) []string {
	out := []string{d.Files[0].Path, d.Files[0].Status}
	return out
}

func TestCanonicalPath(t *testing.T) {
	t.Parallel()
	p, ok := CanonicalPath("b/examples/shop/internal/payment/handler.go")
	if !ok || p != "internal/payment/handler.go" {
		t.Fatalf("%q %v", p, ok)
	}
	if _, ok := CanonicalPath("../secret.go"); ok {
		t.Fatal("expected reject")
	}
}
