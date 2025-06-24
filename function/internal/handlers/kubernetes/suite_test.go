// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"os"
	"testing"

	"github.com/confighub/sdk/function/handler"
)

func TestMain(m *testing.M) {
	kc := handler.NewFunctionHandler()
	KubernetesRegistrar.RegisterFunctions(kc)
	os.Exit(m.Run())
}
