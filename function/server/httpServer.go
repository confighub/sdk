// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/sync/errgroup"
)

// Define a context key for the logger
type contextKey struct{}

var loggerKey = contextKey{}

// fromContext extracts slog.Logger from context, fallback to default if not found
func fromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// newContext adds slog.Logger to context
func newContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// echoLogger provides simple request logging for Echo framework middleware.
// This is local to the functions server and doesn't pollute the core logging package.
type echoLogger struct {
	*slog.Logger
}

// newEchoLogger creates a new Echo-compatible logger wrapper
func newEchoLogger(l *slog.Logger) *echoLogger {
	return &echoLogger{l}
}

// logRequest logs HTTP request information.
func (l *echoLogger) logRequest(method, uri string, status int, err error) {
	if err != nil {
		l.Error("request failed", "error", err, "method", method, "uri", uri, "status", status)
	} else {
		l.Info("request completed", "method", method, "uri", uri, "status", status)
	}
}

func RunServer(ctx context.Context, grp *errgroup.Group, localhostOnly bool) *echo.Echo {
	httpServer, err := newHTTPServer(ctx)
	if err != nil {
		logger := fromContext(ctx)
		logger.Error("unable to create a HTTP Server", "error", err)
		os.Exit(1)
	}

	port := os.Getenv("CONFIGHUB_FUNCTION_PORT")
	if port == "" {
		port = "9080"
	}
	addr := ""
	if localhostOnly {
		addr = "127.0.0.1"
	}
	bindAddr := addr + ":" + port

	grp.Go(func() error {
		logger := fromContext(ctx)
		logger.Info("starting HTTP server", "address", bindAddr)
		err = httpServer.Start(bindAddr)
		// We need to check ErrServerClosed because otherwise it will cause the whole group to be canceled
		// on the first shutdown call.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.Wrap(err, "http server unexpected failure")
		}
		return nil
	})

	return httpServer
}

func newHTTPServer(ctx context.Context) (*echo.Echo, error) {
	rootRouter := echo.New()
	rootRouter.HideBanner = true
	useGlobalMiddlewares(ctx, rootRouter)

	rootRouter.Server.ReadTimeout = time.Second * 30
	rootRouter.Server.WriteTimeout = time.Second * 60
	rootRouter.Server.IdleTimeout = time.Second * 60
	rootRouter.Server.ReadHeaderTimeout = time.Second * 5

	// TODO: Enable these once we support running the function executor standalone.
	// Default route /debug/pprof/*
	// Example: go tool pprof http://localhost:1323/debug/pprof/heap
	// pprof.Register(rootRouter)
	// rootRouter.Use(httpMetrics.New(&httpMetrics.MiddlewareConfig{}).Middleware())
	// rootRouter.GET("/metrics", echoprometheus.NewHandler()) // adds route to serve gathered metrics

	// We don't generate swagger docs for the function executor yet. We use the Go structs directly instead.
	// Swagger endpoint
	// rootRouter.GET("/swagger/*", echoSwagger.WrapHandler)

	echoSetup(rootRouter)

	return rootRouter, nil
}

func NewTestHTTPRouter() (*echo.Echo, error) {
	rootRouter := echo.New()
	echoSetup(rootRouter)
	return rootRouter, nil
}

func useGlobalMiddlewares(ctx context.Context, router *echo.Echo) {
	logger := fromContext(ctx)
	echoLogger := newEchoLogger(logger)
	
	router.Use(
		middleware.RequestID(),
		middleware.Recover(),
		middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
			LogURI:     true,
			LogStatus:  true,
			LogMethod:  true,
			LogLatency: true,
			LogError:   true,
			LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
				echoLogger.logRequest(v.Method, v.URI, v.Status, v.Error)
				return nil
			},
		}),
		middleware.Timeout(), // 30*time.Second
	)
}
