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
var testFunctionHandler *handler.FunctionHandler

func TestMain(m *testing.M) {
	testResourceProvider = k8skit.NewK8sResourceProvider()
	testFunctionHandler = handler.NewFunctionHandler(testResourceProvider)
	RegisterFunctions(testResourceProvider, testFunctionHandler)
	os.Exit(m.Run())
}
