package log

import "context"

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys defined in other packages.
type contextKey int

const loggerKey contextKey = 0

// NewContext returns a copy of ctx that carries the given Logger.
// Use FromContext to retrieve it in downstream handlers and functions.
//
//	ctx = log.NewContext(ctx, logger.With(log.String("request_id", id)))
func NewContext(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns the Logger stored in ctx by NewContext.
// Returns nil if no Logger is present — callers that need a guaranteed
// non-nil logger should fall back to their injected logger:
//
//	l := log.FromContext(r.Context())
//	if l == nil {
//	    l = logger
//	}
func FromContext(ctx context.Context) *Logger {
	l, _ := ctx.Value(loggerKey).(*Logger)
	return l
}
