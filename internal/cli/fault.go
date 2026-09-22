package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/fault"
)

const maxFaultDelay = 30 * time.Second

// RunFault serves a loopback reverse proxy that delays or injects a status.
func RunFault(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fault", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	listen := fs.String("listen", "127.0.0.1:19080", "loopback listen address")
	target := fs.String("target", "", "upstream gateway URL")
	delay := fs.Duration("delay", 0, "injected delay before proxy or status")
	status := fs.Int("status", 0, "if set, return this status and do not proxy")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: fault: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("cli: fault: unexpected argument %q", fs.Arg(0))
	}
	if *target == "" {
		return fmt.Errorf("cli: fault: -target is required")
	}
	if *delay > maxFaultDelay {
		return fmt.Errorf("cli: fault: delay exceeds 30s")
	}
	if err := requireLoopbackListen(*listen); err != nil {
		return err
	}
	h, err := fault.Handler(*target, fault.Spec{Delay: *delay, Status: *status})
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("cli: fault: %w", err)
	}
	writef(stdout, "fault    listen=%s  target=%s  delay=%s  status=%d\n", ln.Addr().String(), *target, delay.String(), *status)
	writef(stdout, "injected faults are not a pass. point aquila replay -base/-patch at this listen address.\n")
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func requireLoopbackListen(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("cli: fault: listen: %w", err)
	}
	if port == "" {
		return fmt.Errorf("cli: fault: listen missing port")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("cli: fault: listen must be loopback")
	}
	return nil
}
