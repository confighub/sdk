// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

// WithLogger returns a context whose function invocations log to logger rather
// than to slog.Default(), so a program running functions in its own process
// decides where each invocation's log line goes without changing the
// process-wide default. Functions themselves log only at debug level, to
// slog.Default().
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// loggerFrom returns the logger WithLogger put in ctx, or slog.Default().
func loggerFrom(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}
