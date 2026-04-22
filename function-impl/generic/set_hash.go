// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerSetHash(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-hash", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-hash",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated configuration path of the attribute(s) to hash. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Computes a SHA-256 hash of all values at the specified path and stores it at the resource provider's context path for Hash (e.g., confighub.com/Hash annotation for Kubernetes)",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnSetHash(resourceProvider, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// GenericFnSetHash computes a SHA-256 hash of all values at the specified path for each
// resource and stores the hash at the resource provider's context path for HashKeySuffix.
func GenericFnSetHash(
	resourceProvider yamlkit.ResourceProvider,
	parsedData gaby.Container,
	args []api.FunctionArgument,
	options *api.FunctionOptions,
) (gaby.Container, any, error) {
	unresolvedPath := args[0].Value.(string)

	hashPath := resourceProvider.ContextPath(constants.HashKeySuffix)
	if hashPath == "" {
		return parsedData, nil, nil
	}

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceTypeAny, api.UnresolvedPath(unresolvedPath))

	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		singleDoc := gaby.Container{doc}
		values, err := yamlkit.GetPathsAnyType(singleDoc, resourceTypeToPaths, []any{}, resourceProvider, api.DataTypeNone, false, false, options)
		if err != nil {
			return output, []error{err}
		}
		if len(values) == 0 {
			return output, nil
		}

		// Sort by path for deterministic hashing
		sort.Slice(values, func(i, j int) bool {
			return values[i].Path < values[j].Path
		})

		h := sha256.New()
		for _, v := range values {
			fmt.Fprintf(h, "%v", v.Value)
		}
		hash := hex.EncodeToString(h.Sum(nil))[:10]

		_, err = doc.SetP(hash, hashPath)
		if err != nil {
			return output, []error{err}
		}
		return output, nil
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)
	return parsedData, nil, err
}
