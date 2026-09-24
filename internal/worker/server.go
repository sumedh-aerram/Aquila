package worker

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/sumedhaerram/aquila/internal/jobs"
)

func nowUTC() time.Time { return time.Now().UTC() }

var contentJSON = grpc.CallContentSubtype("json")

// Server is a gRPC worker endpoint. A non-empty token is required on every call.
func Server(store jobs.Store, token string) *grpc.Server {
	s := grpc.NewServer(grpc.UnaryInterceptor(tokenInterceptor(token)))
	Register(s, store)
	return s
}

// Listen binds the worker gRPC address. Bind failures are returned immediately
// so the API process cannot look healthy with a dead worker port.
func Listen(addr string) (net.Listener, error) {
	if addr == "" {
		return nil, fmt.Errorf("worker: listen addr required")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("worker: %w", err)
	}
	return ln, nil
}

// Serve serves the worker protocol on ln until ctx is cancelled.
func Serve(ctx context.Context, ln net.Listener, store jobs.Store, token string) error {
	if ln == nil {
		return fmt.Errorf("worker: listener required")
	}
	s := Server(store, token)
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ln) }()
	select {
	case <-ctx.Done():
		s.GracefulStop()
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

// ListenAndServe binds addr and serves until ctx is cancelled.
func ListenAndServe(ctx context.Context, addr string, store jobs.Store, token string) error {
	ln, err := Listen(addr)
	if err != nil {
		return err
	}
	return Serve(ctx, ln, store, token)
}

// Dial returns a gRPC client for addr using the JSON worker codec. A non-empty
// token is sent as a bearer credential on every call.
func Dial(addr, token string) (*grpc.ClientConn, error) {
	if addr == "" {
		return nil, fmt.Errorf("worker: grpc addr required")
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if token != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(bearer(token)))
	}
	return grpc.NewClient(addr, opts...)
}

// ClientLease leases one task over gRPC.
func ClientLease(ctx context.Context, conn grpc.ClientConnInterface, workerID string) (*LeaseReply, error) {
	return ClientLeaseSlots(ctx, conn, workerID, jobs.DefaultSlots, "")
}

// ClientLeaseSlots leases one task with a slot limit.
func ClientLeaseSlots(ctx context.Context, conn grpc.ClientConnInterface, workerID string, slots int, jobID string) (*LeaseReply, error) {
	var out LeaseReply
	err := conn.Invoke(ctx, "/"+serviceName+"/Lease", &LeaseRequest{WorkerID: workerID, Slots: slots, JobID: jobID}, &out, contentJSON)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ClientHeartbeat extends a lease.
func ClientHeartbeat(ctx context.Context, conn grpc.ClientConnInterface, workerID, taskID, attempt string) error {
	var out HeartbeatReply
	return conn.Invoke(ctx, "/"+serviceName+"/Heartbeat", &HeartbeatRequest{
		WorkerID: workerID, TaskID: taskID, Attempt: attempt,
	}, &out, contentJSON)
}

// ClientCommit commits a leased task over gRPC.
func ClientCommit(ctx context.Context, conn grpc.ClientConnInterface, req *CommitRequest) (*CommitReply, error) {
	var out CommitReply
	err := conn.Invoke(ctx, "/"+serviceName+"/Commit", req, &out, contentJSON)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoteOnce leases, executes, and commits via gRPC.
func RemoteOnce(ctx context.Context, conn grpc.ClientConnInterface, workerID string) error {
	return RemoteOnceSlots(ctx, conn, workerID, jobs.DefaultSlots, "")
}

// RemoteOnceSlots leases with a slot limit.
func RemoteOnceSlots(ctx context.Context, conn grpc.ClientConnInterface, workerID string, slots int, jobID string) error {
	reply, err := ClientLeaseSlots(ctx, conn, workerID, slots, jobID)
	if err != nil {
		return err
	}
	if reply.Empty {
		return jobs.ErrNoReady
	}
	lease := jobs.Lease{Job: reply.Job, Task: reply.Task, N: reply.N, Attempt: reply.Attempt}
	beatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(heartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-beatCtx.Done():
				return
			case <-ticker.C:
				_ = ClientHeartbeat(beatCtx, conn, workerID, lease.Task.ID, lease.Attempt)
			}
		}
	}()
	res, execErr := Execute(ctx, lease)
	cancel()
	fail := ""
	if execErr != nil {
		fail = execErr.Error()
		res = lease.Task.Result
		res.ID = lease.Task.PlanID
		res.Kind = lease.Task.Kind
		res.Verdict = "incomplete"
	}
	_, err = ClientCommit(ctx, conn, &CommitRequest{
		TaskID:  lease.Task.ID,
		Attempt: lease.Attempt,
		Result:  res,
		Error:   fail,
	})
	return err
}
