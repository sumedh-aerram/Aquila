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

// Server is a gRPC worker endpoint.
func Server(store jobs.Store) *grpc.Server {
	s := grpc.NewServer()
	Register(s, store)
	return s
}

// ListenAndServe serves the worker protocol until ctx is cancelled.
func ListenAndServe(ctx context.Context, addr string, store jobs.Store) error {
	if addr == "" {
		return fmt.Errorf("worker: listen addr required")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("worker: %w", err)
	}
	s := Server(store)
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

// Dial returns a gRPC client for addr using the JSON worker codec.
func Dial(addr string) (*grpc.ClientConn, error) {
	if addr == "" {
		return nil, fmt.Errorf("worker: grpc addr required")
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// ClientLease leases one task over gRPC.
func ClientLease(ctx context.Context, conn grpc.ClientConnInterface, workerID string) (*LeaseReply, error) {
	var out LeaseReply
	err := conn.Invoke(ctx, "/"+serviceName+"/Lease", &LeaseRequest{WorkerID: workerID}, &out, contentJSON)
	if err != nil {
		return nil, err
	}
	return &out, nil
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
	reply, err := ClientLease(ctx, conn, workerID)
	if err != nil {
		return err
	}
	if reply.Empty {
		return jobs.ErrNoReady
	}
	lease := jobs.Lease{Job: reply.Job, Task: reply.Task, N: reply.N, Attempt: reply.Attempt}
	res, execErr := Execute(ctx, lease)
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
