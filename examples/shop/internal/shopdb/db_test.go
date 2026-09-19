package shopdb

import (
	"context"
	"testing"
	"time"
)

func TestOpenRejectsInvalidURL(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Open(ctx, "://bad")
	if err == nil {
		t.Fatal("expected error")
	}
}
