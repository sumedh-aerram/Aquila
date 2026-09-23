package plan

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/fault"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func probeFault(ctx context.Context, s Step, base, patch string, w replay.Workload) StepResult {
	out := StepResult{ID: s.ID, Kind: s.Kind}
	status := s.Status
	if status < 1 {
		status = http.StatusBadGateway
	}
	step := probeStep(w)
	h, err := fault.Handler(patch, fault.Spec{Status: status})
	if err != nil {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{err.Error()}
		return out
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{err.Error()}
		return out
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 2 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		_ = ln.Close()
	}()
	injectURL := "http://" + ln.Addr().String()
	inj, err := replay.Run(ctx, injectURL, replay.Workload{Steps: []replay.Step{step}})
	if err != nil {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{err.Error()}
		return out
	}
	baseRun, err := replay.Run(ctx, base, replay.Workload{Steps: []replay.Step{step}})
	if err != nil {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{err.Error()}
		return out
	}
	if len(inj.Steps) == 0 || inj.Steps[0].Status != status {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{"inject did not return the requested status"}
		return out
	}
	if len(baseRun.Steps) == 0 || baseRun.Steps[0].Err != "" || baseRun.Steps[0].Status == status {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{"baseline also failed; inject is not distinguishable"}
		return out
	}
	out.Verdict = VerdictPrepared
	out.Notes = []string{fmt.Sprintf("one-sided %d", status)}
	return out
}

func probeStep(w replay.Workload) replay.Step {
	for _, s := range w.Steps {
		m := strings.ToUpper(strings.TrimSpace(s.Method))
		if m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions {
			return s
		}
	}
	if len(w.Steps) > 0 {
		return w.Steps[0]
	}
	return replay.Step{Method: http.MethodGet, Path: "/healthz"}
}

func compareBurst(base, patch []replay.Result) StepResult {
	out := StepResult{ID: "concurrency", Kind: KindConcurrency}
	if len(base) == 0 || len(patch) == 0 {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{"no samples"}
		return out
	}
	be, bn := burstErrors(base)
	pe, pn := burstErrors(patch)
	if bn == 0 || pn == 0 {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{"no observations"}
		return out
	}
	out.Notes = []string{fmt.Sprintf("baseline_errors=%d/%d patch_errors=%d/%d", be, bn, pe, pn)}
	if pe > be {
		out.Verdict = replay.VerdictDiffer
		return out
	}
	out.Verdict = replay.VerdictMatch
	return out
}

func burstErrors(runs []replay.Result) (errs, n int) {
	for _, r := range runs {
		for _, s := range r.Steps {
			n++
			if s.Err != "" || s.Status >= 500 || s.Status == 0 {
				errs++
			}
		}
	}
	return errs, n
}
