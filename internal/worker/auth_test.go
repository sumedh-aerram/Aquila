package worker

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sumedhaerram/aquila/internal/jobs"
)

func TestGRPCRequiresTokenWhenSet(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, jobs.NewMemory(), "s3cret") }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	tests := []struct {
		name  string
		token string
		want  codes.Code
	}{
		{"none", "", codes.Unauthenticated},
		{"wrong", "guess", codes.Unauthenticated},
		{"right", "s3cret", codes.OK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := Dial(ln.Addr().String(), tc.token)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			_, err = ClientLease(t.Context(), conn, "w")
			if got := status.Code(err); got != tc.want {
				t.Fatalf("code=%v err=%v", got, err)
			}
		})
	}
}
