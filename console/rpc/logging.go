package rpc

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
)

// LoggingInterceptor returns a connect.UnaryInterceptorFunc that logs RPC calls.
func LoggingInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			procedure := req.Spec().Procedure

			resp, err := next(ctx, req)

			duration := time.Since(start)
			attrs := []any{
				"procedure", procedure,
				"duration_ms", duration.Milliseconds(),
			}

			if err != nil {
				attrs = append(attrs, "error", err.Error())
				slog.Error("rpc call failed", attrs...)
			} else {
				slog.Info("rpc call", attrs...)
			}

			return resp, err
		}
	}
}
