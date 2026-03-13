// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

// kyvernoBinary is the name or path of the kyverno CLI binary.
// It can be overridden for testing.
var kyvernoBinary = "kyverno"

// GetVetKyvernoSignature returns the function signature for vet-kyverno.
func GetVetKyvernoSignature() api.FunctionSignature {
	return api.FunctionSignature{
		FunctionName: "vet-kyverno",
		Parameters: []api.FunctionParameter{
			{
				ParameterName: "policy",
				Required:      true,
				Description:   "A YAML document or document list containing Kyverno policy resources (ClusterPolicy, Policy, or ValidatingPolicy). Policies from https://kyverno.io/policies/ can be used directly.",
				DataType:      api.DataTypeYAML,
			},
		},
		RequiredParameters: 1,
		OutputInfo: &api.FunctionOutput{
			ResultName:  "passed",
			Description: "True if all resources pass all Kyverno policy validations, false otherwise",
			OutputType:  api.OutputTypeValidationResult,
		},
		Mutating:              false,
		Validating:            true,
		Hermetic:              false, // execs kyverno CLI
		Idempotent:            true,
		Description:           "Validates Kubernetes resources against Kyverno policies using the kyverno CLI. Supports ClusterPolicy and Policy resources with validate rules. See https://kyverno.io/policies/ for available policies.",
		FunctionType:          api.FunctionTypeCustom,
		AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
	}
}

// VetKyvernoFunction validates Kubernetes resources against Kyverno policies
// by execing the kyverno CLI.
func VetKyvernoFunction(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
	return vetKyverno(k8skit.NewK8sResourceProvider(), fArgs.ParsedData, fArgs.Arguments)
}

func vetKyverno(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	policyYAML := args[0].Value.(string)

	// Write policy to a temp file.
	policyFile, err := os.CreateTemp("", "kyverno-policy-*.yaml")
	if err != nil {
		return parsedData, nil, errors.Wrap(err, "failed to create temp policy file")
	}
	defer os.Remove(policyFile.Name())
	if _, err := policyFile.WriteString(policyYAML); err != nil {
		policyFile.Close()
		return parsedData, nil, errors.Wrap(err, "failed to write policy file")
	}
	policyFile.Close()

	// Write resources to a temp file.
	resourceFile, err := os.CreateTemp("", "kyverno-resource-*.yaml")
	if err != nil {
		return parsedData, nil, errors.Wrap(err, "failed to create temp resource file")
	}
	defer os.Remove(resourceFile.Name())
	for i, doc := range parsedData {
		if i > 0 {
			resourceFile.WriteString("---\n")
		}
		resourceFile.Write(doc.Bytes())
		resourceFile.WriteString("\n")
	}
	resourceFile.Close()

	// Run kyverno apply.
	cmd := exec.Command(kyvernoBinary, "apply", policyFile.Name(),
		"--resource", resourceFile.Name(),
		"--detailed-results",
	)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Parse the output into validation results.
	// Exit code 0 = all pass, non-zero = failures or errors.
	if err == nil {
		return parsedData, api.ValidationResultTrue, nil
	}

	// Check if it's an exit error (validation failure) vs execution error.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return parsedData, nil, errors.Wrapf(err, "failed to execute kyverno CLI (is %q in PATH?)", kyvernoBinary)
	}

	// Parse failures from the output.
	failures := parseKyvernoOutput(outputStr)
	if len(failures) == 0 {
		// Non-zero exit but couldn't parse failures — return the raw output.
		return parsedData, nil, errors.Newf("kyverno validation failed: %s", outputStr)
	}

	// Build the validation result with failed attributes.
	result := api.ValidationResultFalse

	// Build a resource info map for path lookups.
	resourceInfoMap := buildResourceInfoMap(parsedData, rp)

	for _, f := range failures {
		detail := fmt.Sprintf("policy %q rule %q failed: %s", f.policyName, f.ruleName, f.message)
		result.Details = append(result.Details, detail)

		path := ""
		if f.path != "" {
			path = gaby.JSONPointerToPath(f.path)
		}

		resourceInfo := lookupResourceInfo(resourceInfoMap, f.resourceKey)
		failedAttr := api.AttributeValue{
			AttributeInfo: api.AttributeInfo{
				AttributeIdentifier: api.AttributeIdentifier{
					ResourceInfo: resourceInfo,
					Path:         api.ResolvedPath(path),
				},
				AttributeMetadata: api.AttributeMetadata{
					AttributeName: api.AttributeNameNone,
					Details: &api.AttributeDetails{
						Description: detail,
					},
				},
			},
		}
		result.FailedAttributes = append(result.FailedAttributes, failedAttr)
	}

	return parsedData, result, nil
}

// kyvernoFailure represents a parsed failure from kyverno CLI output.
type kyvernoFailure struct {
	policyName  string
	ruleName    string
	message     string
	path        string
	resourceKey string // "namespace/Kind/name"
}

// parseKyvernoOutput parses kyverno apply output into structured failures.
// Output format:
//
//	policy <name> -> resource <ns>/<Kind>/<name> failed:
//	1 - <ruleName> validation error: <message> rule <ruleName> failed at path <path>
var (
	policyLineRE  = regexp.MustCompile(`^policy (\S+) -> resource (\S+) failed:`)
	ruleFailureRE = regexp.MustCompile(`^\d+ - (\S+) validation error: (.+)`)
	pathRE        = regexp.MustCompile(`failed at path (/\S*)`)
)

func parseKyvernoOutput(output string) []kyvernoFailure {
	var failures []kyvernoFailure
	var currentPolicy string
	var currentResource string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		if m := policyLineRE.FindStringSubmatch(line); m != nil {
			currentPolicy = m[1]
			currentResource = m[2]
			continue
		}

		if m := ruleFailureRE.FindStringSubmatch(line); m != nil && currentPolicy != "" {
			f := kyvernoFailure{
				policyName:  currentPolicy,
				ruleName:    m[1],
				message:     m[2],
				resourceKey: currentResource,
			}
			if pm := pathRE.FindStringSubmatch(line); pm != nil {
				f.path = strings.TrimRight(pm[1], "/")
			}
			failures = append(failures, f)
		}
	}
	return failures
}

// buildResourceInfoMap creates a map from "namespace/Kind/name" to ResourceInfo
// by visiting all resources in the parsed data.
func buildResourceInfoMap(parsedData gaby.Container, rp *k8skit.K8sResourceProviderType) map[string]api.ResourceInfo {
	infoMap := make(map[string]api.ResourceInfo)
	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		kind := doc.Path("kind").Data()
		name := doc.Path("metadata.name").Data()
		ns := doc.Path("metadata.namespace").Data()
		nsStr := "default"
		if s, ok := ns.(string); ok && s != "" {
			nsStr = s
		}
		kindStr, _ := kind.(string)
		nameStr, _ := name.(string)
		key := fmt.Sprintf("%s/%s/%s", nsStr, kindStr, nameStr)
		infoMap[key] = *resourceInfo
		return output, nil
	}
	yamlkit.VisitResources(parsedData, nil, rp, visitor)
	return infoMap
}

// lookupResourceInfo finds a ResourceInfo by the kyverno "ns/Kind/name" key.
func lookupResourceInfo(infoMap map[string]api.ResourceInfo, key string) api.ResourceInfo {
	if info, ok := infoMap[key]; ok {
		return info
	}
	return api.ResourceInfo{}
}
