package client

import (
	"context"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// correlationIDKey is the context key for correlation IDs
type correlationIDKey struct{}

// CorrelationIDLogKey is the key used in structured logs
const CorrelationIDLogKey = "correlation_id"

// WithCorrelationID adds a new correlation ID to the context and configures tflog
func WithCorrelationID(ctx context.Context) context.Context {
	correlationID := uuid.New().String()
	ctx = context.WithValue(ctx, correlationIDKey{}, correlationID)
	ctx = tflog.SetField(ctx, CorrelationIDLogKey, correlationID)
	return ctx
}

// GetCorrelationID retrieves the correlation ID from context, or returns empty string if not set
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok {
		return id
	}
	return ""
}
