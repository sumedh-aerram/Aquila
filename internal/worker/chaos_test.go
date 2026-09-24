package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const chaosWorkerEnv = "AQUILA_CHAOS_WORKER_ADDR"

// TestMain lets the chaos test re-exec this binary as a real worker process
// that can be SIGKILLed mid-task without running any tests.
func TestMain(m *testing.M) {
	if addr := os.Getenv(chaosWorkerEnv); addr != "" {
		os.Exit(runChaosWorker(addr))
	}
	os.Exit(m.Run())
}

func runChaosWorker(addr string) int {
	conn, err := Dial(addr, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer func() { _ = conn.Close() }()
	if err := RemoteOnceSlots(context.Background(), conn, "chaos-a", 1, ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func TestKilledWorkerTaskRecoversAndStaleCommitIsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a worker process")
	}
	var hang atomic.Bool
	hang.Store(true)
	leased := make(chan struct{}, 1)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && hang.Load() {
			select {
			case leased <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(gw.Close)

	st := jobs.NewMemory()
	job, err := st.Create(t.Context(), jobs.CreateOpts{
		Baseline: gw.URL,
		Patch:    gw.URL,
		Workload: replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/items"}}},
		Plan:     mustPlan(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, ln, st, "") }()
	t.Cleanup(func() {
		cancel()
		<-served
	})

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), chaosWorkerEnv+"="+ln.Addr().String())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-leased:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("worker process never reached the gateway")
	}
	held, err := st.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stale jobs.Task
	for _, task := range held.Tasks {
		if task.State == jobs.StateLeased {
			stale = task
		}
	}
	if stale.WorkerID != "chaos-a" || stale.AttemptID == "" {
		t.Fatalf("want task leased by chaos-a, got %+v", stale)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed worker must not exit cleanly")
	}
	hang.Store(false)

	// The reaper runs on wall time in production; advancing its clock past the
	// lease TTL is the same transition without a 15s sleep.
	if n, err := st.RequeueExpired(t.Context(), time.Now().UTC().Add(time.Minute)); err != nil || n != 1 {
		t.Fatalf("requeued=%d err=%v", n, err)
	}

	conn, err := Dial(ln.Addr().String(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for i := 0; i < 20; i++ {
		err := RemoteOnceSlots(t.Context(), conn, "chaos-b", 1, "")
		if errors.Is(err, jobs.ErrNoReady) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err = ClientCommit(t.Context(), conn, &CommitRequest{
		TaskID:  stale.ID,
		Attempt: stale.AttemptID,
		Result:  plan.StepResult{ID: stale.PlanID, Kind: stale.Kind, Verdict: replay.VerdictMatch},
	})
	if err == nil {
		t.Fatal("killed worker's attempt must not commit")
	}

	done, err := st.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != jobs.StatusComplete {
		t.Fatalf("status=%s tasks=%+v", done.Status, done.Tasks)
	}
	for _, task := range done.Tasks {
		if task.ID != stale.ID {
			continue
		}
		if task.Fence < 2 || task.AttemptID == stale.AttemptID || task.State != jobs.StateSucceeded {
			t.Fatalf("recovered task %+v", task)
		}
	}
}
