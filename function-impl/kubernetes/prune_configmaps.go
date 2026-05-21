// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// defaultRevisionHistoryLimit matches the Kubernetes default for Deployment
// revision history and the prior ConfigMapRenderer bridge default.
const defaultRevisionHistoryLimit = 10

func registerPruneConfigMaps(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	if err := fh.RegisterFunction("prune-configmaps", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "prune-configmaps",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "revision-history-limit",
					Required:      false,
					Description:   "Maximum number of immutable ConfigMaps with the same ResourceNameStableCore annotation to retain. Older versions beyond the limit are removed. Defaults to 10.",
					DataType:      api.DataTypeInt,
					Example:       "10",
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "For each group of immutable v1/ConfigMap resources sharing a confighub.com/ResourceNameStableCore annotation, retains at most the configured number of newest (highest confighub.com/RevisionNum) entries, removing the rest. The newest entry retains the confighub.com/RenderRevision: Latest annotation; older retained entries have it stripped and are marked with confighub.com/VisitorOptions: IgnoreProvided. Mutable ConfigMaps are left untouched.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceType("v1/ConfigMap")},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return fnPruneConfigMaps(rp, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// configMapEntry records an immutable ConfigMap document along with the
// metadata we use for grouping and ordering.
type configMapEntry struct {
	docIndex    int
	revisionNum int64
	stableCore  string
}

func fnPruneConfigMaps(
	rp *k8skit.K8sResourceProviderType,
	_ *api.FunctionContext,
	parsedData gaby.Container,
	args []api.FunctionArgument,
	options *api.FunctionOptions,
) (gaby.Container, any, error) {
	limit := defaultRevisionHistoryLimit
	if len(args) > 0 && args[0].Value != nil {
		switch v := args[0].Value.(type) {
		case int:
			if v >= 0 {
				limit = v
			}
		case int64:
			if v >= 0 {
				limit = int(v)
			}
		case float64:
			if v >= 0 {
				limit = int(v)
			}
		case string:
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				limit = n
			}
		}
	}

	stableCorePath := k8skit.K8sContextPath(constants.ResourceNameStableCoreKeySuffix)
	revisionNumPath := k8skit.K8sContextPath(constants.RevisionNumKeySuffix)
	renderRevisionPath := k8skit.K8sContextPath(constants.RenderRevisionKeySuffix)
	visitorOptionsPath := k8skit.K8sContextPath(constants.VisitorOptionsKeySuffix)

	// Group immutable ConfigMaps by stable-core annotation. Don't rely on the
	// in-document order; sort by RevisionNum after collecting.
	groups := map[string][]configMapEntry{}
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options,
		func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
			if resourceInfo.ResourceType != api.ResourceType("v1/ConfigMap") {
				return output, nil
			}
			if !isImmutableConfigMap(doc) {
				return output, nil
			}
			stableCore := stringAtPath(doc, stableCorePath)
			if stableCore == "" {
				return output, nil
			}
			revNumStr := stringAtPath(doc, revisionNumPath)
			revNum, parseErr := strconv.ParseInt(revNumStr, 10, 64)
			if parseErr != nil {
				// No usable RevisionNum — leave alone.
				return output, nil
			}
			groups[stableCore] = append(groups[stableCore], configMapEntry{
				docIndex:    index,
				revisionNum: revNum,
				stableCore:  stableCore,
			})
			return output, nil
		})
	if err != nil {
		return parsedData, nil, fmt.Errorf("prune-configmaps: %w", err)
	}

	// For each group: sort newest-first, remark Latest, prune to limit.
	toDelete := map[int]struct{}{}
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].revisionNum > group[j].revisionNum
		})

		for i, entry := range group {
			doc := parsedData[entry.docIndex]
			if doc == nil {
				continue
			}
			if i == 0 {
				// Keep the newest entry as Latest.
				if _, err := doc.SetP("Latest", renderRevisionPath); err != nil {
					return parsedData, nil, fmt.Errorf("prune-configmaps: failed to set RenderRevision on doc %d: %w", entry.docIndex, err)
				}
			} else {
				// Older retained entries: drop Latest marker and ignore in get-provided.
				_ = doc.DeleteP(renderRevisionPath)
				if _, err := doc.SetP(constants.VisitorOptionIgnoreProvided, visitorOptionsPath); err != nil {
					return parsedData, nil, fmt.Errorf("prune-configmaps: failed to set VisitorOptions on doc %d: %w", entry.docIndex, err)
				}
			}
		}

		if len(group) > limit {
			for _, entry := range group[limit:] {
				toDelete[entry.docIndex] = struct{}{}
			}
		}
	}

	if len(toDelete) == 0 {
		return parsedData, nil, nil
	}

	// Build a new container preserving non-deleted documents in their original order.
	pruned := make(gaby.Container, 0, len(parsedData)-len(toDelete))
	for i, doc := range parsedData {
		if _, drop := toDelete[i]; drop {
			continue
		}
		pruned = append(pruned, doc)
	}
	return pruned, nil, nil
}

// isImmutableConfigMap returns true if the top-level immutable field is true.
func isImmutableConfigMap(doc *gaby.YamlDoc) bool {
	if doc == nil {
		return false
	}
	node := doc.Path("immutable")
	if node == nil {
		return false
	}
	switch v := node.Data().(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

// stringAtPath returns the string at the given dot path, or "" if not present
// or not a string. Numeric annotation values (which YAML may decode as int)
// are stringified.
func stringAtPath(doc *gaby.YamlDoc, path string) string {
	if doc == nil {
		return ""
	}
	node := doc.Path(path)
	if node == nil {
		return ""
	}
	switch v := node.Data().(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}
