package cli

import (
	"context"

	"go.uber.org/zap"
)

type loggerKey struct{}

// withLogger returns a copy of ctx carrying logger.
func withLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// loggerFrom retrieves the logger from ctx, falling back to a no-op logger so
// callers never have to nil-check.
func loggerFrom(ctx context.Context) *zap.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*zap.Logger); ok && logger != nil {
		return logger
	}
	return zap.NewNop()
}
