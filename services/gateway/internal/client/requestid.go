package client

import "context"

type requestIDKey struct{}

// WithRequestID carries a request ID across the tool handler into the upstream
// call, so one log line at the gateway and one at the leaf can be joined.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the ID set by WithRequestID, or "".
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
