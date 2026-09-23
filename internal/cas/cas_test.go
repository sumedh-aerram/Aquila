package cas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPutGetRoundTrip(t *testing.T) {
	t.Parallel()
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.Put(t.Context(), []byte("evidence"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := d.Put(t.Context(), []byte("evidence"))
	if err != nil || again != id {
		t.Fatalf("%s %v", again, err)
	}
	got, err := d.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "evidence" {
		t.Fatalf("%q", got)
	}
}

func TestGetMissing(t *testing.T) {
	t.Parallel()
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Get(t.Context(), Sum([]byte("nope")))
	if err != ErrNotFound {
		t.Fatalf("%v", err)
	}
}

func TestCorruptRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.Put(t.Context(), []byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "objects", string(id)[:2], string(id))
	if err := os.WriteFile(path, []byte("tamper"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = d.Get(t.Context(), id)
	if err != ErrCorrupt {
		t.Fatalf("%v", err)
	}
	if _, err := d.Put(t.Context(), []byte("ok")); err != ErrCorrupt {
		t.Fatalf("put over corrupt: %v", err)
	}
}
