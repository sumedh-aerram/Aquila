package gateway

import (
	"context"
	"net/http"
	stdproxy "net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/examples/shop/internal/httputil"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httpx"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func NewHandler(usersURL, checkoutURL string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("/users/", proxy(usersURL))
	mux.Handle("/checkout", proxy(checkoutURL))
	mux.Handle("/checkout/", proxy(checkoutURL))
	return httpx.Wrap("gateway", mux)
}

func proxy(raw string) http.Handler {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			httputil.WriteError(w, http.StatusInternalServerError, "gateway misconfigured")
		})
	}
	rp := stdproxy.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	rp.Transport = otelhttp.NewTransport(&http.Transport{IdleConnTimeout: 90 * time.Second})
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		httputil.WriteError(w, http.StatusBadGateway, "upstream unavailable")
	}
	rp.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		if !strings.HasPrefix(req.URL.Path, "/") {
			req.URL.Path = "/" + req.URL.Path
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		rp.ServeHTTP(w, r.WithContext(ctx))
	})
}
