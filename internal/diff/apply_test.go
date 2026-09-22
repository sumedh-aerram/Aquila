package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyModifiesMatchingContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "internal", "payment", "handler.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "package payment\nfunc charge() {\n\tclient := old()\n\treturn\n}\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`--- a/internal/payment/handler.go
+++ b/internal/payment/handler.go
@@ -2,3 +2,3 @@ func charge() {
 func charge() {
-	client := old()
+	client := new()
 	return
`)
	if err := Apply(dir, raw); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "client := new()") || strings.Contains(string(got), "client := old()") {
		t.Fatalf("%s", got)
	}
}

func TestApplyRejectsContextMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`--- a/a.go
+++ b/a.go
@@ -1,1 +1,1 @@
-package b
+package c
`)
	if err := Apply(dir, raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyRejectsTraversal(t *testing.T) {
	t.Parallel()
	raw := []byte(`--- a/../../etc/passwd
+++ b/../../etc/passwd
@@ -1 +1 @@
-a
+b
`)
	if err := Apply(t.TempDir(), raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyRejectsRename(t *testing.T) {
	t.Parallel()
	raw := []byte(`diff --git a/old.go b/new.go
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -1 +1 @@
-a
+b
`)
	if err := Apply(t.TempDir(), raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyAddsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	raw := []byte(`diff --git a/internal/new.go b/internal/new.go
new file mode 100644
--- /dev/null
+++ b/internal/new.go
@@ -0,0 +1,2 @@
+package n
+
`)
	if err := Apply(dir, raw); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "internal", "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "package n") {
		t.Fatalf("%s", got)
	}
}
