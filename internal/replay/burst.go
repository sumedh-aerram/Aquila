package replay

import (
	"context"
	"fmt"
	"sync"
)

const maxBurst = 32

// Burst runs the workload n times in parallel against target.
func Burst(ctx context.Context, target string, w Workload, n int) ([]Result, error) {
	if n < 1 {
		return nil, fmt.Errorf("replay: n must be >= 1")
	}
	if n > maxBurst {
		return nil, fmt.Errorf("replay: n exceeds %d", maxBurst)
	}
	if err := CheckTarget(target); err != nil {
		return nil, err
	}
	if len(w.Steps) == 0 {
		return nil, fmt.Errorf("replay: empty workload")
	}
	out := make([]Result, n)
	var mu sync.Mutex
	var first error
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
				return
			}
			r, err := Run(ctx, target, w)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			out[i] = r
		}(i)
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return out, nil
}
