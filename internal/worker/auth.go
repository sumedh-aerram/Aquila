package worker

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// tokenInterceptor rejects calls without the control-plane token. An empty
// token leaves the worker port open, matching the HTTP API's local mode.
func tokenInterceptor(token string) grpc.UnaryServerInterceptor {
	want := sha256.Sum256([]byte(token))
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if token == "" {
			return handler(ctx, req)
		}
		got := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if v := md.Get("authorization"); len(v) > 0 {
				got = strings.TrimSpace(strings.TrimPrefix(v[0], "Bearer "))
			}
		}
		sum := sha256.Sum256([]byte(got))
		if subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}
		return handler(ctx, req)
	}
}

type bearer string

func (b bearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(b)}, nil
}

// The worker protocol runs on loopback or a private network without TLS in
// local Compose; production fronts it with a TLS-terminating network.
func (bearer) RequireTransportSecurity() bool { return false }
