// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package channel

import (
	"log"
	"testing"
)

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

func testLogger(t *testing.T) *log.Logger {
	return log.New(testWriter{t}, "", 0)
}
