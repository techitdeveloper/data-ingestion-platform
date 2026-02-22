package trace

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// WithRequestID creates a new context with a request ID.
func WithRequestID(ctx context.Context) context.Context {
	requestID := uuid.New().String()
	return context.WithValue(ctx, requestIDKey, requestID)
}

// GetRequestID extracts the request ID from context, or returns empty string.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
