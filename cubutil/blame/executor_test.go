// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"testing"

	funcimpl "github.com/confighub/sdk/function-impl"
)

// TestLocalExecutorCoversEveryToolchain: localExecutor lists the toolchains itself so that
// importing this package does not link every built-in function. The standard executor is the
// list of toolchains there are, so a toolchain added there and not here fails this test
// rather than leaving its units unreadable to blame. The import is the test's alone, and
// reaches no program built from this package.
func TestLocalExecutorCoversEveryToolchain(t *testing.T) {
	standard := funcimpl.NewStandardExecutor(nil, false)
	local := localExecutor()
	for toolchain := range standard.RegisteredFunctions() {
		if _, ok := local.GetResourceProvider(toolchain); !ok {
			t.Errorf("toolchain %s has no resource provider in localExecutor", toolchain)
		}
	}
	for toolchain := range local.RegisteredFunctions() {
		if _, ok := local.RegisteredFunctions()[toolchain]["compute-mutations"]; !ok {
			t.Errorf("toolchain %s has no compute-mutations", toolchain)
		}
	}
}
