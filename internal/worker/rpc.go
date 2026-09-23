package worker

import (
	"context"
	"encoding/json"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"

	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
)

const serviceName = "aquila.worker.v1.Worker"

// LeaseRequest is a worker claim.
type LeaseRequest struct {
	WorkerID string `json:"worker_id"`
	Slots    int    `json:"slots,omitempty"`
	JobID    string `json:"job_id,omitempty"`
}

// HeartbeatRequest extends a lease. Attempt must match.
type HeartbeatRequest struct {
	WorkerID string `json:"worker_id"`
	TaskID   string `json:"task_id"`
	Attempt  string `json:"attempt"`
}

// HeartbeatReply is an empty success.
type HeartbeatReply struct{}

// LeaseReply is one leased task, or empty.
type LeaseReply struct {
	Empty   bool      `json:"empty"`
	Job     jobs.Job  `json:"job,omitempty"`
	Task    jobs.Task `json:"task,omitempty"`
	Attempt string    `json:"attempt,omitempty"`
	N       int       `json:"n,omitempty"`
}

// CommitRequest is a worker completion. Attempt must match the lease.
type CommitRequest struct {
	TaskID  string          `json:"task_id"`
	Attempt string          `json:"attempt"`
	Result  plan.StepResult `json:"result"`
	Error   string          `json:"error,omitempty"`
}

// CommitReply is the stored task after commit.
type CommitReply struct {
	Task jobs.Task `json:"task"`
}

// GRPC serves Lease and Commit. Payloads are JSON on the gRPC codec.
type GRPC struct {
	store jobs.Store
}

// NewGRPC returns a worker service backed by store.
func NewGRPC(store jobs.Store) *GRPC {
	return &GRPC{store: store}
}

// Register attaches the worker service to s.
func Register(s *grpc.Server, store jobs.Store) {
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: serviceName,
		HandlerType: (*workerServer)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Lease", Handler: leaseHandler},
			{MethodName: "Heartbeat", Handler: heartbeatHandler},
			{MethodName: "Commit", Handler: commitHandler},
		},
		Streams: []grpc.StreamDesc{},
	}, NewGRPC(store))
}

type workerServer interface {
	Lease(ctx context.Context, req *LeaseRequest) (*LeaseReply, error)
	Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatReply, error)
	Commit(ctx context.Context, req *CommitRequest) (*CommitReply, error)
}

// Lease implements workerServer.
func (g *GRPC) Lease(ctx context.Context, req *LeaseRequest) (*LeaseReply, error) {
	if g.store == nil {
		return nil, status.Error(codes.Unavailable, "jobs unavailable")
	}
	lease, err := g.store.Lease(ctx, jobs.Worker{ID: req.WorkerID, Slots: req.Slots, JobID: req.JobID}, nowUTC())
	if err != nil {
		if errors.Is(err, jobs.ErrNoReady) || errors.Is(err, jobs.ErrCapacity) {
			return &LeaseReply{Empty: true}, nil
		}
		if errors.Is(err, jobs.ErrInvalidID) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, "lease failed")
	}
	return &LeaseReply{Job: lease.Job, Task: lease.Task, Attempt: lease.Attempt, N: lease.N}, nil
}

// Heartbeat implements workerServer.
func (g *GRPC) Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatReply, error) {
	if g.store == nil {
		return nil, status.Error(codes.Unavailable, "jobs unavailable")
	}
	err := g.store.Heartbeat(ctx, req.TaskID, req.Attempt, req.WorkerID, nowUTC())
	if err != nil {
		if errors.Is(err, jobs.ErrStaleAttempt) || errors.Is(err, jobs.ErrNotLeased) || errors.Is(err, jobs.ErrNotFound) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, "heartbeat failed")
	}
	return &HeartbeatReply{}, nil
}

// Commit implements workerServer.
func (g *GRPC) Commit(ctx context.Context, req *CommitRequest) (*CommitReply, error) {
	if g.store == nil {
		return nil, status.Error(codes.Unavailable, "jobs unavailable")
	}
	task, err := g.store.Commit(ctx, req.TaskID, req.Attempt, req.Result, req.Error)
	if err != nil {
		if errors.Is(err, jobs.ErrStaleAttempt) {
			return nil, status.Error(codes.FailedPrecondition, "stale attempt")
		}
		if errors.Is(err, jobs.ErrNotFound) || errors.Is(err, jobs.ErrNotLeased) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, "commit failed")
	}
	return &CommitReply{Task: task}, nil
}

func leaseHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(LeaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(workerServer).Lease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + serviceName + "/Lease"}
	return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
		return srv.(workerServer).Lease(ctx, req.(*LeaseRequest))
	})
}

func heartbeatHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(HeartbeatRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(workerServer).Heartbeat(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + serviceName + "/Heartbeat"}
	return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
		return srv.(workerServer).Heartbeat(ctx, req.(*HeartbeatRequest))
	})
}

func commitHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(CommitRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(workerServer).Commit(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + serviceName + "/Commit"}
	return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
		return srv.(workerServer).Commit(ctx, req.(*CommitRequest))
	})
}

type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error) { return json.Marshal(v) }

func (jsonCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func (jsonCodec) Name() string { return "json" }

func init() {
	encoding.RegisterCodec(jsonCodec{})
}
