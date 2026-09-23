package cas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotFound is returned when a digest is missing.
var ErrNotFound = errors.New("cas: not found")

// ErrCorrupt is returned when stored bytes do not match the digest.
var ErrCorrupt = errors.New("cas: corrupt")

// Digest is a lowercase SHA-256 hex string.
type Digest string

// Dir is an immutable SHA-256 object store on disk.
type Dir struct {
	root string
}

// Open returns a directory-backed CAS at root, creating it if needed.
func Open(root string) (*Dir, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("cas: root required")
	}
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o755); err != nil {
		return nil, fmt.Errorf("cas: %w", err)
	}
	return &Dir{root: root}, nil
}

// Sum returns the SHA-256 digest of data.
func Sum(data []byte) Digest {
	sum := sha256.Sum256(data)
	return Digest(hex.EncodeToString(sum[:]))
}

func (d Digest) valid() bool {
	if len(d) != 64 {
		return false
	}
	for i := 0; i < len(d); i++ {
		c := d[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (d *Dir) path(id Digest) string {
	s := string(id)
	return filepath.Join(d.root, "objects", s[:2], s)
}

// Put stores data and returns its digest. A second put of the same bytes is a no-op.
func (d *Dir) Put(ctx context.Context, data []byte) (Digest, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("cas: not configured")
	}
	id := Sum(data)
	dest := d.path(id)
	if _, err := os.Stat(dest); err == nil {
		got, err := os.ReadFile(dest)
		if err != nil {
			return "", fmt.Errorf("cas: %w", err)
		}
		if Sum(got) != id {
			return "", ErrCorrupt
		}
		return id, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".put-*")
	if err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("cas: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("cas: %w", err)
	}
	return id, nil
}

// Get returns the bytes for id after verifying the digest.
func (d *Dir) Get(ctx context.Context, id Digest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("cas: not configured")
	}
	if !id.valid() {
		return nil, fmt.Errorf("cas: invalid digest")
	}
	raw, err := os.ReadFile(d.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("cas: %w", err)
	}
	if Sum(raw) != id {
		return nil, ErrCorrupt
	}
	return raw, nil
}
