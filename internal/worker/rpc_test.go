package worker

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestGRPCRejectsStaleAttempt(t *testing.T) {
	t.Parallel()
	st := jobs.NewMemory()
	if _, err := st.Create(t.Context(), jobs.CreateOpts{
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		Workload: replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz"}}},
		Plan:     mustPlan(t),
	}); err != nil {
		t.Fatal(err)
	}
	conn, stop := grpcPipe(t, st)
	t.Cleanup(stop)

	a, err := ClientLease(t.Context(), conn, "worker-a")
	if err != nil || a.Empty {
		t.Fatalf("%v %+v", err, a)
	}
	_, err = st.RequeueExpired(t.Context(), time.Now().UTC().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ClientLease(t.Context(), conn, "worker-b")
	if err != nil || b.Empty {
		t.Fatalf("%v %+v", err, b)
	}
	_, err = ClientCommit(t.Context(), conn, &CommitRequest{
		TaskID:  a.Task.ID,
		Attempt: a.Attempt,
		Result:  plan.StepResult{ID: a.Task.PlanID, Kind: a.Task.Kind, Verdict: replay.VerdictMatch},
	})
	if err == nil {
		t.Fatal("stale attempt must be rejected")
	}
	_, err = ClientCommit(t.Context(), conn, &CommitRequest{
		TaskID:  b.Task.ID,
		Attempt: b.Attempt,
		Result:  plan.StepResult{ID: b.Task.PlanID, Kind: b.Task.Kind, Verdict: replay.VerdictMatch},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func grpcPipe(t *testing.T, st jobs.Store) (*grpc.ClientConn, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	s := Server(st)
	go func() { _ = s.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return conn, func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}
}
