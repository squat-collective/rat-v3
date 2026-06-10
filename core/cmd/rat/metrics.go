package main

// metrics.go — the native /metrics HTTP endpoint (gap #6). Core-native observability: a
// `rat serve` is scrapeable with NO observability plugin installed; an observability-axis
// plugin layers richer telemetry on top, it is not required for the basics.

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/le-squat/rat/core/metrics"
)

// serveMetrics starts the /metrics endpoint on addr (Prometheus text exposition), returning a
// stop func. A blank addr disables it (returns a no-op) — opt-in via RAT_METRICS_ADDR so many
// daemons on one host don't fight over a port; the capability is always present, the port isn't.
func serveMetrics(addr string, reg *metrics.Registry) func() {
	if addr == "" {
		return func() {}
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("metrics: cannot listen on %s: %v (metrics disabled)", addr, err)
		return func() {}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		reg.WritePrometheus(w)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(lis) }()
	log.Printf("metrics serving — http://%s/metrics", lis.Addr())
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}
