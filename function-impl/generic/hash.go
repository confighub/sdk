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

// hashLength is how many hex characters of the SHA-256 are kept. It matches the length the
// configmap bridge and render-configmap use, so a hash computed here names the same content
// they would name.
const hashLength = 10

const hashPathDescription = "Dot-separated configuration path of the attribute(s) to hash; repeat it to hash the values at several paths together, such as data and binaryData. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax."

func registerSetHash(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-hash", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-hash",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "path",
					Required:         true,
					Description:      hashPathDescription,
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			VarArgs:    true,
			Mutating:   true,
			Validating: false,
			Hermetic:   true,
			Idempotent: true,
			Replayable: true,
			// On a Kubernetes ConfigMap the annotation it writes is the provided path of the
			// configmap-hash attribute, which render-configmap writes too: set-hash is how a
			// ConfigMap authored as Kubernetes/YAML offers the hash Needs/Provides delivers.
			Description:           "Computes a SHA-256 hash of all values at the specified paths and stores it at the resource provider's context path for Hash: the confighub.com/Hash annotation for Kubernetes. On a ConfigMap, that annotation is the hash Needs/Provides offers to the pod templates that need it, in hash.confighub.com/<configmap> or confighub.com/Hash, so a change to the ConfigMap rolls the pods. It applies to every resource that has the path, of any type, so narrow it with a where-resource filter",
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

func registerGetHash(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-hash", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-hash",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "path",
					Required:         true,
					Description:      hashPathDescription,
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "hash",
				Description: "Hash of the values at the specified paths, one entry per resource that has values there",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:   false,
			Validating: false,
			Hermetic:   true,
			Idempotent: true,
			// Returns the hash that set-hash would store, without storing it. A Unit that
			// needs its own content hash carried elsewhere -- onto a workload's pod template,
			// so that changing a ConfigMap rolls the Deployment reading it -- can then be
			// left alone: the hash is computed from the upstream data while a Link resolves
			// and written to the downstream Unit, rather than kept in the resource it
			// describes, where it is a revision behind whatever it is hashing.
			Description:           "Computes a SHA-256 hash of all values at the specified paths and returns it, without modifying the configuration data. Returns the same hash set-hash stores. Use as an UpstreamGetter on a TransformPaths Link to propagate a content hash to another Unit. It reads every resource that has the path, of any type, so narrow it with a where-resource filter.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnGetHash(resourceProvider, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// hashPaths returns the visitor map of the paths set-hash and get-hash are given. Values at
// all of them are hashed together, sorted by path, so one path hashes exactly as it did alone,
// and a path a resource lacks adds nothing: set-hash data binaryData gives a ConfigMap without
// binaryData the hash set-hash data gives it.
func hashPaths(resourceProvider yamlkit.ResourceProvider, args []api.FunctionArgument) (api.ResourceTypeToPathToVisitorInfoType, error) {
	merged := api.ResourceTypeToPathToVisitorInfoType{}
	for _, arg := range args {
		unresolvedPath, ok := arg.Value.(string)
		if !ok || unresolvedPath == "" {
			return nil, fmt.Errorf("path must be a non-empty string, got %v", arg.Value)
		}
		for rt, paths := range yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceTypeAny, api.UnresolvedPath(unresolvedPath)) {
			if merged[rt] == nil {
				merged[rt] = api.PathToVisitorInfoType{}
			}
			for path, info := range paths {
				merged[rt][path] = info
			}
		}
	}
	return merged, nil
}

// hashValuesAtPath computes the hash of the values at resourceTypeToPaths within one resource.
// found is false when the resource has no values at the path, which is not an error: a
// document holding resources of several types is expected to have some the path does not
// apply to.
//
// set-hash and get-hash share this, so what a Unit stores and what a Link propagates are the
// same hash of the same values.
func hashValuesAtPath(
	doc *gaby.YamlDoc,
	resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType,
	resourceProvider yamlkit.ResourceProvider,
	options *api.FunctionOptions,
) (hash string, found bool, err error) {
	singleDoc := gaby.Container{doc}
	values, err := yamlkit.GetPathsAnyType(singleDoc, resourceTypeToPaths, []any{}, resourceProvider, api.DataTypeNone, false, false, options)
	if err != nil {
		return "", false, err
	}
	if len(values) == 0 {
		return "", false, nil
	}

	// Sort by path for deterministic hashing
	sort.Slice(values, func(i, j int) bool {
		return values[i].Path < values[j].Path
	})

	h := sha256.New()
	for _, v := range values {
		fmt.Fprintf(h, "%v", v.Value)
	}
	return hex.EncodeToString(h.Sum(nil))[:hashLength], true, nil
}

// GenericFnSetHash computes a SHA-256 hash of all values at the specified paths for each
// resource and stores the hash at the resource provider's context path for HashKeySuffix.
func GenericFnSetHash(
	resourceProvider yamlkit.ResourceProvider,
	parsedData gaby.Container,
	args []api.FunctionArgument,
	options *api.FunctionOptions,
) (gaby.Container, any, error) {
	hashPath := resourceProvider.ContextPath(constants.HashKeySuffix)
	if hashPath == "" {
		return parsedData, nil, nil
	}

	resourceTypeToPaths, err := hashPaths(resourceProvider, args)
	if err != nil {
		return parsedData, nil, err
	}

	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		hash, found, err := hashValuesAtPath(doc, resourceTypeToPaths, resourceProvider, options)
		if err != nil {
			return output, []error{err}
		}
		if !found {
			return output, nil
		}

		_, err = doc.SetP(hash, hashPath)
		if err != nil {
			return output, []error{err}
		}
		return output, nil
	}

	_, err = yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)
	return parsedData, nil, err
}

// GenericFnGetHash computes a SHA-256 hash of all values at the specified paths for each
// resource and returns them, one AttributeValue per resource, leaving the data unchanged.
// The reported Path is the first path hashed, not a path the hash is stored at -- the point
// of this function is that it is stored nowhere.
func GenericFnGetHash(
	resourceProvider yamlkit.ResourceProvider,
	parsedData gaby.Container,
	args []api.FunctionArgument,
	options *api.FunctionOptions,
) (gaby.Container, any, error) {
	resourceTypeToPaths, err := hashPaths(resourceProvider, args)
	if err != nil {
		return parsedData, nil, err
	}
	reportedPath := args[0].Value.(string)

	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		accumulated := output.(api.AttributeValueList)

		hash, found, err := hashValuesAtPath(doc, resourceTypeToPaths, resourceProvider, options)
		if err != nil {
			return output, []error{err}
		}
		if !found {
			return output, nil
		}

		return append(accumulated, api.AttributeValue{
			AttributeInfo: api.AttributeInfo{
				AttributeIdentifier: api.AttributeIdentifier{
					ResourceInfo: *resourceInfo,
					Path:         api.ResolvedPath(reportedPath),
				},
				AttributeMetadata: api.AttributeMetadata{
					DataType: api.DataTypeString,
				},
			},
			Value: hash,
		}), nil
	}

	output, err := yamlkit.VisitResourcesFiltered(parsedData, api.AttributeValueList{}, resourceProvider, options, visitor)
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, output.(api.AttributeValueList), nil
}
