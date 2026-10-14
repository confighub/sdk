// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"sync"

	"github.com/confighub/sdk/configkit/appyamlkit"
	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/configkit/inikit"
	"github.com/confighub/sdk/configkit/jsonkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/configkit/textkit"
	"github.com/confighub/sdk/configkit/tomlkit"
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/function/handler"
)

// localExecutor is what blame reads a unit's data with locally: per toolchain, the resource
// provider that names its resources and its arrays' merge keys, and compute-mutations, which
// every toolchain's handler registers for itself and which flattens the data into fields.
//
// It registers the toolchains and no functions, which is the whole of what blame uses. A
// standard executor would also link every built-in function, and the Kubernetes client and
// kustomize libraries behind them, into every program that imports this package.
// TestLocalExecutorCoversEveryToolchain keeps the list here the same as the standard one.
// Built once: registering a provider registers its toolchain's path registry.
var localExecutor = sync.OnceValue(func() *executor.ConcreteFunctionExecutor {
	exec := executor.NewEmptyExecutor()
	for _, provider := range []handler.ToolchainProvider{
		cubkit.NewConfigHubResourceProvider(),
		k8skit.NewK8sResourceProvider(),
		propkit.NewPropertiesResourceProvider(),
		appyamlkit.NewAppConfigYAMLResourceProvider(),
		tomlkit.NewTOMLResourceProvider(),
		inikit.NewINIResourceProvider(),
		jsonkit.NewJSONResourceProvider(),
		envkit.NewEnvResourceProvider(),
		textkit.NewTextResourceProvider(),
	} {
		exec.RegisterToolchain(provider, true)
	}
	return exec
})
