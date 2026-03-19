// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
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
				Description:   "A YAML document or document list containing Kyverno policy resources (ValidatingPolicy, ClusterPolicy, or Policy) or Kubernetes ValidatingAdmissionPolicy resources. Policies from https://kyverno.io/policies/ can be used directly.",
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
		Description:           "Validates Kubernetes resources against Kyverno policies using the kyverno CLI. Supports Kyverno ValidatingPolicy, ClusterPolicy, and Policy resources, as well as Kubernetes ValidatingAdmissionPolicy resources. See https://kyverno.io/policies/ for available policies.",
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
	resourceFile.Write(parsedData.Bytes())
	resourceFile.WriteString("\n")
	resourceFile.Close()

	// Run kyverno apply with JSON policy report output.
	cmd := exec.Command(kyvernoBinary, "apply", policyFile.Name(),
		"--resource", resourceFile.Name(),
		"--policy-report",
		"--output-format=json",
	)
	output, err := cmd.CombinedOutput()

	// Check if it's an execution error (binary not found, etc.)
	// vs a validation failure (non-zero exit with parseable output).
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return parsedData, nil, errors.Wrapf(err, "failed to execute kyverno CLI (is %q in PATH?)", kyvernoBinary)
		}
	}

	// Parse the JSON policy report.
	report, parseErr := parsePolicyReport(output)
	if parseErr != nil {
		return parsedData, nil, errors.Wrapf(parseErr, "failed to parse kyverno output: %s", string(output))
	}

	// If all passed, return early.
	if report.Summary.Fail == 0 {
		return parsedData, api.ValidationResultTrue, nil
	}

	// Build the validation result with failed attributes.
	result := api.ValidationResultFalse
	resourceInfoMap := buildResourceInfoMap(parsedData, rp)

	for _, r := range report.Results {
		if r.Result != "fail" {
			continue
		}

		ruleName := r.Rule
		if ruleName == "" {
			ruleName = "validation"
		}

		detail := fmt.Sprintf("policy %q rule %q failed: %s", r.Policy, ruleName, r.Message)
		result.Details = append(result.Details, detail)

		path := extractPathFromMessage(r.Message)

		for _, res := range r.Resources {
			ns := res.Namespace
			if ns == "" {
				ns = "default"
			}
			resourceKey := fmt.Sprintf("%s/%s/%s", ns, res.Kind, res.Name)
			resourceInfo := lookupResourceInfo(resourceInfoMap, resourceKey)

			failedAttr := api.AttributeValue{
				AttributeInfo: api.AttributeInfo{
					AttributeIdentifier: api.AttributeIdentifier{
						ResourceInfo: resourceInfo,
					},
					AttributeMetadata: api.AttributeMetadata{
						AttributeName: api.AttributeNameNone,
					},
				},
				Issues: []api.Issue{
					{
						Identifier: r.Policy + "/" + ruleName,
						Message:    r.Message,
					},
				},
			}
			if path != "" {
				failedAttr.AttributeInfo.AttributeIdentifier.Path = api.ResolvedPath(path)
			}
			result.FailedAttributes = append(result.FailedAttributes, failedAttr)
		}
	}

	return parsedData, result, nil
}

// policyReport represents the JSON output from kyverno apply --policy-report --output-format=json.
type policyReport struct {
	Summary policyReportSummary  `json:"summary"`
	Results []policyReportResult `json:"results"`
}

type policyReportSummary struct {
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
	Skip  int `json:"skip"`
}

type policyReportResult struct {
	Source    string                 `json:"source"`
	Policy    string                 `json:"policy"`
	Rule      string                 `json:"rule"`
	Result    string                 `json:"result"`
	Message   string                 `json:"message"`
	Resources []policyReportResource `json:"resources"`
}

type policyReportResource struct {
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	APIVersion string `json:"apiVersion"`
}

// parsePolicyReport parses the JSON policy report from kyverno CLI output.
func parsePolicyReport(output []byte) (*policyReport, error) {
	// The kyverno CLI may print warnings to stderr mixed into combined output.
	// Find the JSON object in the output.
	jsonStart := strings.IndexByte(string(output), '{')
	if jsonStart < 0 {
		return nil, fmt.Errorf("no JSON found in output")
	}

	var report policyReport
	if err := json.Unmarshal(output[jsonStart:], &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// buildResourceInfoMap creates a map from "namespace/Kind/name" to ResourceInfo
// by visiting all resources in the parsed data. It derives the key from
// ResourceInfo fields populated by VisitResources, similar to
// ParseResourceMetadataFromResourceInfo in the k8s-admission-webhook package.
func buildResourceInfoMap(parsedData gaby.Container, rp *k8skit.K8sResourceProviderType) map[string]api.ResourceInfo {
	infoMap := make(map[string]api.ResourceInfo)
	visitor := func(_ *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		// Extract kind from ResourceType (e.g., "apps/v1/Deployment" -> "Deployment").
		rt := string(resourceInfo.ResourceType)
		kind := rt
		if idx := strings.LastIndex(rt, "/"); idx >= 0 {
			kind = rt[idx+1:]
		}
		apiVersion := ""
		if idx := strings.LastIndex(rt, "/"); idx >= 0 {
			apiVersion = rt[:idx]
		}

		// Extract namespace and name from ResourceName (e.g., "default/my-deploy").
		rn := string(resourceInfo.ResourceName)
		var namespace, name string
		if ns, n, ok := strings.Cut(rn, "/"); ok {
			namespace = ns
			name = n
		} else {
			name = rn
		}

		// Only apply default namespace for namespaced resources.
		if namespace == "" && !k8skit.IsClusterScoped(apiVersion, kind) {
			namespace = "default"
		}

		key := fmt.Sprintf("%s/%s/%s", namespace, kind, name)
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

// failedAtPathRE matches "failed at path /spec/..." in kyverno messages.
var failedAtPathRE = regexp.MustCompile(`failed at path (/\S+)`)

// extractPathFromMessage extracts a JSON Pointer path from a Kyverno message
// and converts it to dot notation. Returns empty string if no path found.
func extractPathFromMessage(msg string) string {
	m := failedAtPathRE.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	return jsonPointerToDotNotation(m[1])
}

// jsonPointerToDotNotation converts a JSON Pointer path like
// "/spec/template/spec/containers/0/image/" to dot notation
// "spec.template.spec.containers.0.image".
func jsonPointerToDotNotation(ptr string) string {
	ptr = strings.Trim(ptr, "/")
	if ptr == "" {
		return ""
	}
	return strings.ReplaceAll(ptr, "/", ".")
}
