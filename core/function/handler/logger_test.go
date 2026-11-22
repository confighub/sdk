// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// A context's logger receives every invocation's log line, the failing one's
// included, and the process-wide default receives none.
func TestInvocationsLogToTheContextsLogger(t *testing.T) {
	var defaultLog, contextLog bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&defaultLog, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctx := WithLogger(context.Background(), slog.New(slog.NewTextHandler(&contextLog, nil)))
	resp, err := parseOnceHandler(t).InvokeCore(ctx, &api.FunctionInvocationRequest{
		ConfigData: parseOnceData,
		FunctionInvocations: api.FunctionInvocationList{
			call("set-test-label", "value"),
			call("fail-after-edit", "partial"),
		},
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, contextLog.String(), "invoke set-test-label: succeeded")
	assert.Contains(t, contextLog.String(), "invoke fail-after-edit: failed after editing")
	assert.Empty(t, defaultLog.String())
}

// Without one, invocations log to the process-wide default, as they always have.
func TestInvocationsLogToTheDefaultWithoutOne(t *testing.T) {
	var defaultLog bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&defaultLog, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	invokeParseOnce(t, parseOnceHandler(t), true, call("set-test-label", "value"))
	assert.Contains(t, defaultLog.String(), "invoke set-test-label: succeeded")
}
