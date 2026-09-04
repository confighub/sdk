// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/handler"
)

var testResourceProvider *k8skit.K8sResourceProviderType
var testFunctionHandler *handler.FunctionHandler

// registrationErrors holds everything registration logged at error level. Registering a path
// that conflicts with one already registered reports itself by logging rather than by failing,
// and `go test` shows a passing package's output only under -v, so those lines were written on
// every server start with nothing watching them.
var registrationErrors []string

// errorCollectingHandler records error-level output and discards the rest. It deliberately
// wraps no other handler: slog.SetDefault points the standard log package at whatever handler
// it is given unless that handler is slog's own, so delegating to the previous default would
// send every log line back through log.Output and recurse until the stack runs out.
type errorCollectingHandler struct{}

func (h *errorCollectingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *errorCollectingHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Level < slog.LevelError {
		return nil
	}
	attrs := []string{}
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, fmt.Sprintf("%s=%v", attr.Key, attr.Value))
		return true
	})
	registrationErrors = append(registrationErrors,
		strings.TrimSpace(record.Message+" "+strings.Join(attrs, " ")))
	return nil
}

func (h *errorCollectingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *errorCollectingHandler) WithGroup(string) slog.Handler { return h }

func TestMain(m *testing.M) {
	previousLogger := slog.Default()
	previousFlags := log.Flags()
	slog.SetDefault(slog.New(&errorCollectingHandler{}))

	testResourceProvider = k8skit.NewK8sResourceProvider()
	testFunctionHandler = handler.NewFunctionHandler(testResourceProvider)
	RegisterFunctions(testResourceProvider, testFunctionHandler)

	// SetDefault only restores the standard log package's own output when handed slog's
	// built-in handler, so put it back explicitly rather than leaving every later test's
	// logging routed through the collector above.
	slog.SetDefault(previousLogger)
	log.SetOutput(os.Stderr)
	log.SetFlags(previousFlags)

	os.Exit(m.Run())
}

// TestRegistrationIsSilent keeps registration honest: the built-in Kubernetes functions
// register the same path more than once by design -- an HPA's scaleTargetRef names any of
// three workload controllers -- and each of those registrations must be recognized as
// compatible with the ones before it rather than reported as a conflict.
func TestRegistrationIsSilent(t *testing.T) {
	assert.Empty(t, registrationErrors,
		"registering the Kubernetes functions logged errors")
}
