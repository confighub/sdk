// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"golang.org/x/sync/errgroup"

	"github.com/confighub/sdk/function/server"
)

var (
	// TODO: probably want this to be configurable for prod vs tests
	terminationGracePeriodSeconds = 1
	logger                        *slog.Logger
	exporter                      *prometheus.Exporter
)

func main() {
	flag.Parse()
	var err error

	logger = slog.Default()

	ctx := context.Background()
	// Use our custom context function that matches the server package
	type contextKey struct{}
	var loggerKey = contextKey{}
	ctx = context.WithValue(ctx, loggerKey, logger)
	ctx, cancel := context.WithCancel(ctx)
	grp, ctx := errgroup.WithContext(ctx)

	defer cancel()

	exporter, err = prometheus.New()
	if err != nil {
		logger.Error("unable to create a prometheus exporter", "error", err)
		os.Exit(1)
	}
	// Create a new MeterProvider with the Prometheus exporter
	provider := metric.NewMeterProvider(
		metric.WithReader(exporter),
		metric.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("confighub"),
		)),
	)
	otel.SetMeterProvider(provider)

	httpServer := server.RunServer(ctx, grp, false)

	handleIntercepts(ctx, grp, httpServer)

	if errGrp := grp.Wait(); errGrp != nil {
		logger.Error("application unexpectedly shut down", "error", errGrp)
		os.Exit(1)
	}

	logger.Info("application gracefully shut down")
}

func interceptSignals(ctx context.Context) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	select {
	case <-ctx.Done():
	case sig := <-sigc:
		logger.Info("intercepted signal", "signal", sig)
	}
}

func handleIntercepts(ctx context.Context, grp *errgroup.Group, httpServer *echo.Echo) {
	grp.Go(func() error {
		interceptSignals(ctx)

		go func() {
			interceptSignals(ctx)
			logger.Error("forcibly shutting down on second signal")
			os.Exit(1)
		}()

		shutdownCtx, shutCancel := context.WithTimeout(ctx, time.Duration(terminationGracePeriodSeconds)*time.Second)
		defer shutCancel()

		if httpServer != nil {
			return shutdown(shutdownCtx, httpServer)
		}
		return nil
	})
}

func shutdown(ctx context.Context, httpServer *echo.Echo) (errs error) {
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := errors.WithStack(httpServer.Shutdown(ctx)); err != nil {
			errs = errors.Join(errs, err)
		}
	}()

	wg.Wait()

	return
}
