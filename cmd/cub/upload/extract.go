// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	fapi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	funcimpl "github.com/confighub/sdk/function-impl"
	"gopkg.in/yaml.v3"
)

// quietExecutor raises the default slog level so the function executor's
// per-invocation INFO lines don't pollute upload output. Done once, lazily.
var quietExecutor sync.Once

func silenceFunctionLogs() {
	quietExecutor.Do(func() { slog.SetLogLoggerLevel(slog.LevelError) })
}

// runK8sFunctionAttributeValues executes a single Kubernetes/YAML function
// in-process over the given multi-document YAML and returns its
// AttributeValueList output. This is the same local executor path cub uses for
// `cub function local`, so reference and workload-label extraction runs without
// a server round-trip or any Units existing yet.
func runK8sFunctionAttributeValues(yamlData []byte, fn string) (fapi.AttributeValueList, error) {
	silenceFunctionLogs()
	executor := funcimpl.NewStandardExecutor(nil, true)
	req := &fapi.FunctionInvocationRequest{
		FunctionContext: fapi.FunctionContext{ToolchainType: workerapi.ToolchainKubernetesYAML},
		ConfigData:      string(yamlData),
		FunctionInvocations: []fapi.FunctionInvocation{
			{FunctionName: fn},
		},
	}
	resp, err := executor.Invoke(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fn, err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s failed: %s", fn, strings.Join(resp.ErrorMessages, "; "))
	}
	raw, ok := resp.Outputs[fapi.OutputTypeAttributeValueList]
	if !ok || len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var avs fapi.AttributeValueList
	if err := json.Unmarshal(raw, &avs); err != nil {
		return nil, fmt.Errorf("decode %s output: %w", fn, err)
	}
	return avs, nil
}

// reference is one resource-to-resource reference: the resource identified by
// From ("apiVersion/kind#namespace/name") names a resource of TargetType called
// TargetName (without scope).
type reference struct {
	From       fapi.ResourceTypeAndName
	TargetType fapi.ResourceType
	TargetName fapi.ResourceName
}

// extractReferences runs get-references over the whole rendered bundle and
// returns every resolved reference (those declaring a ResourceType target with a
// non-empty value). Mirrors the installer's loadReferences/planLinks decoding.
func extractReferences(yamlData []byte) ([]reference, error) {
	avs, err := runK8sFunctionAttributeValues(yamlData, "get-references")
	if err != nil {
		return nil, err
	}
	var out []reference
	for _, av := range avs {
		if av.Details == nil {
			continue
		}
		target := av.Details.NeededRequired["ResourceType"]
		if target == "" {
			continue
		}
		val, ok := av.Value.(string)
		if !ok || val == "" {
			continue
		}
		out = append(out, reference{
			From:       fapi.ResourceTypeAndFullNameFromResourceInfo(av.ResourceInfo),
			TargetType: fapi.ResourceType(target),
			TargetName: fapi.ResourceName(val),
		})
	}
	return out, nil
}

// workloadLabels holds the selector and pod-template label maps a single
// workload resource exposes, used for selector-based link inference.
type workloadLabels struct {
	selectors []map[string]string
	templates []map[string]string
}

// extractWorkloadLabels runs get-workload-labels over the bundle and groups the
// results by the owning resource's type+scope+name key.
func extractWorkloadLabels(yamlData []byte) (map[fapi.ResourceTypeAndName]*workloadLabels, error) {
	avs, err := runK8sFunctionAttributeValues(yamlData, "get-workload-labels")
	if err != nil {
		return nil, err
	}
	byResource := map[fapi.ResourceTypeAndName]*workloadLabels{}
	for _, av := range avs {
		m := labelMapFromValue(av.Value)
		if len(m) == 0 {
			continue
		}
		key := fapi.ResourceTypeAndFullNameFromResourceInfo(av.ResourceInfo)
		wl := byResource[key]
		if wl == nil {
			wl = &workloadLabels{}
			byResource[key] = wl
		}
		if isSelectorPath(av.Path) {
			wl.selectors = append(wl.selectors, m)
		} else {
			wl.templates = append(wl.templates, m)
		}
	}
	return byResource, nil
}

// labelMapFromValue coerces a get-workload-labels value into a string map. The
// executor may surface it as a decoded map or as a YAML string, so handle both.
func labelMapFromValue(v any) map[string]string {
	switch t := v.(type) {
	case map[string]interface{}:
		m := make(map[string]string, len(t))
		for k, val := range t {
			if s, ok := val.(string); ok {
				m[k] = s
			}
		}
		return m
	case map[string]string:
		return t
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		m := map[string]string{}
		if err := yaml.Unmarshal([]byte(t), &m); err != nil {
			return nil
		}
		return m
	default:
		return nil
	}
}

func isSelectorPath(path fapi.ResolvedPath) bool {
	s := string(path)
	return strings.Contains(s, "selector") || strings.Contains(s, "Selector")
}

func anySelectorMatches(selectors, templates []map[string]string) bool {
	for _, s := range selectors {
		if len(s) == 0 {
			continue
		}
		for _, t := range templates {
			if isSubsetOf(s, t) {
				return true
			}
		}
	}
	return false
}

func isSubsetOf(s, t map[string]string) bool {
	for k, v := range s {
		if tv, ok := t[k]; !ok || tv != v {
			return false
		}
	}
	return true
}
