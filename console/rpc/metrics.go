package rpc

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	rpcRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rpc_requests_total",
			Help: "Total number of RPC requests by procedure and code.",
		},
		[]string{"procedure", "code"},
	)

	rpcRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rpc_request_duration_seconds",
			Help:    "Histogram of RPC request latencies.",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"procedure"},
	)

	rpcRequestsInFlight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rpc_requests_in_flight",
			Help: "Number of RPC requests currently being processed.",
		},
		[]string{"procedure"},
	)
)

// MetricsInterceptor returns a connect.UnaryInterceptorFunc that records Prometheus metrics.
func MetricsInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			procedure := req.Spec().Procedure

			rpcRequestsInFlight.WithLabelValues(procedure).Inc()
			defer rpcRequestsInFlight.WithLabelValues(procedure).Dec()

			start := time.Now()
			resp, err := next(ctx, req)
			duration := time.Since(start)

			rpcRequestDuration.WithLabelValues(procedure).Observe(duration.Seconds())

			code := "ok"
			if err != nil {
				code = connect.CodeOf(err).String()
			}
			rpcRequestsTotal.WithLabelValues(procedure, code).Inc()

			return resp, err
		}
	}
}
