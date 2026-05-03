package contextutil

import "context"

type contextKey string

const (
	circuitBreakerKey contextKey = "circuit_breaker_open"
	clientIPKey       contextKey = "client_ip"
)

// --- Client IP Helpers ---

// WithClientIP injects the IP into the context (used in Middleware)
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey, ip)
}

// GetClientIP retrieves the IP from the context (used in Repository/Service)
func GetClientIP(ctx context.Context) string {
	val, ok := ctx.Value(clientIPKey).(string)
	if !ok {
		return "unknown"
	}
	return val
}

// --- Circuit Breaker Helpers ---

// WithCircuitOpen sets the circuit state in the context
func WithCircuitOpen(ctx context.Context, isOpen bool) context.Context {
	return context.WithValue(ctx, circuitBreakerKey, isOpen)
}

// IsCircuitOpen checks if the operation should be blocked
func IsCircuitOpen(ctx context.Context) bool {
	val, ok := ctx.Value(circuitBreakerKey).(bool)
	return ok && val
}
