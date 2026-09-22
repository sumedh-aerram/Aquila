package replay

import (
	"context"
	"fmt"
)

const maxRepeats = 100

// Repeat runs the workload n times against target. Behavior compare uses the
// first run; later runs only add latency samples.
func Repeat(ctx context.Context, target string, w Workload, n int) ([]Result, error) {
	n, err := clipRepeats(n)
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0, n)
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		r, err := Run(ctx, target, w)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func clipRepeats(n int) (int, error) {
	if n < 1 {
		return 0, fmt.Errorf("replay: n must be >= 1")
	}
	if n > maxRepeats {
		return 0, fmt.Errorf("replay: n exceeds %d", maxRepeats)
	}
	return n, nil
}
