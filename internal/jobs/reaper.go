package jobs

import (
	"context"
	"time"
)

// Reap expires leases. Safe to call on controller start and on a timer.
func Reap(ctx context.Context, store Store, now time.Time) (int, error) {
	if store == nil {
		return 0, nil
	}
	return store.RequeueExpired(ctx, now)
}

// Loop reaps expired leases until ctx is cancelled.
func Loop(ctx context.Context, store Store, interval time.Duration) {
	if store == nil {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	_, _ = Reap(ctx, store, time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_, _ = Reap(ctx, store, now.UTC())
		}
	}
}
