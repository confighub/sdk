// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"os"
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/handler"
)

var testResourceProvider *k8skit.K8sResourceProviderType

func TestMain(m *testing.M) {
	testResourceProvider = k8skit.NewK8sResourceProvider()
	kc := handler.NewFunctionHandler(testResourceProvider)
	RegisterFunctions(testResourceProvider, kc)
	os.Exit(m.Run())
}
