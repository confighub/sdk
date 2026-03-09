// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"os"
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/handler"
)

var testResourceProvider *k8skit.K8sResourceProviderType

func TestMain(m *testing.M) {
	kc := handler.NewFunctionHandler()
	registrar := NewKubernetesRegistrar()
	registrar.RegisterFunctions(kc)
	testResourceProvider = registrar.GetResourceProvider()
	os.Exit(m.Run())
}
