// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	psaapi "k8s.io/pod-security-admission/api"
	"k8s.io/pod-security-admission/policy"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// podSecurityEvaluator runs the Pod Security Standards checks that the PodSecurity admission
// controller runs, so a pod this function passes is one the cluster admits at the same level and
// version. Building it validates the checks, so it is built once.
var podSecurityEvaluator = sync.OnceValues(func() (policy.Evaluator, error) {
	return policy.NewEvaluator(policy.DefaultChecks())
})

func registerPodSecurityFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	if err := fh.RegisterFunction("vet-pod-security-standard", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-pod-security-standard",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "level",
					Required:      true,
					Description:   `Pod Security Standard level the workloads must meet: "privileged", "baseline", or "restricted"`,
					DataType:      api.DataTypeString,
					ValueConstraints: api.ValueConstraints{EnumValues: []string{
						string(psaapi.LevelPrivileged), string(psaapi.LevelBaseline), string(psaapi.LevelRestricted),
					}},
				},
				{
					ParameterName: "version",
					Required:      false,
					Description:   `Version of the standard to check against, as in the pod-security.kubernetes.io/enforce-version label: "latest" (the default) or a Kubernetes minor version such as "v1.30"`,
					DataType:      api.DataTypeString,
				},
			},
			RequiredParameters: 1,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if every workload meets the level, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:   false,
			Validating: true,
			Hermetic:   true,
			Idempotent: true,
			Description: "Validates that every pod template in the configuration meets the specified Pod Security " +
				"Standard level (https://kubernetes.io/docs/concepts/security/pod-security-standards/), " +
				"using the same checks the PodSecurity admission controller applies. Passes when there are no workloads.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: k8skit.PodSpecResourceTypes(),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var level, version string
			for _, arg := range fArgs.Arguments {
				s, ok := arg.Value.(string)
				if !ok || s == "" {
					continue
				}
				switch arg.ParameterName {
				case "level":
					level = s
				case "version":
					version = s
				}
			}
			return k8sFnVetPodSecurityStandard(rp, fArgs.ParsedData, level, version, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func k8sFnVetPodSecurityStandard(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, levelArg, versionArg string, options *api.FunctionOptions) (gaby.Container, any, error) {
	level, err := psaapi.ParseLevel(levelArg)
	if err != nil {
		return parsedData, nil, errors.Wrapf(err, "invalid level %q", levelArg)
	}
	if versionArg == "" {
		versionArg = "latest"
	}
	version, err := psaapi.ParseVersion(versionArg)
	if err != nil {
		return parsedData, nil, errors.Wrapf(err, "invalid version %q", versionArg)
	}
	lv := psaapi.LevelVersion{Level: level, Version: version}
	evaluator, err := podSecurityEvaluator()
	if err != nil {
		return parsedData, nil, errors.Wrap(err, "failed to build the Pod Security Standards checks")
	}

	result := api.ValidationResult{Passed: true}
	_, err = yamlkit.VisitResourcesFiltered(parsedData, &result, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		vr := output.(*api.ValidationResult)
		var errs []error
		for _, podSpecPath := range k8skit.PodSpecPaths(resourceInfo.ResourceType) {
			issues, found, err := evaluatePodSecurity(evaluator, lv, doc, podSpecPath)
			if err != nil {
				errs = append(errs, errors.Wrapf(err, "%s %s", resourceInfo.ResourceType, resourceInfo.ResourceName))
				continue
			}
			if !found || len(issues) == 0 {
				continue
			}
			vr.Passed = false
			vr.FailedAttributes = append(vr.FailedAttributes, api.AttributeValue{
				AttributeInfo: api.AttributeInfo{
					AttributeIdentifier: api.AttributeIdentifier{
						ResourceInfo: *resourceInfo,
						Path:         api.ResolvedPath(podSpecPath),
					},
					AttributeMetadata: api.AttributeMetadata{AttributeName: api.AttributeNameNone},
				},
				Value:  "",
				Issues: issues,
			})
		}
		return vr, errs
	})
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, result, nil
}

// evaluatePodSecurity checks the PodSpec at podSpecPath and returns one Issue per check it fails,
// identified by the check's reason and worded as the admission controller words its rejection. found is false when the resource has
// no PodSpec there.
func evaluatePodSecurity(evaluator policy.Evaluator, lv psaapi.LevelVersion, doc *gaby.YamlDoc, podSpecPath string) ([]api.Issue, bool, error) {
	podSpecDoc, found, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(podSpecPath), true)
	if err != nil || !found {
		return nil, found, err
	}
	podSpec, err := decodePodSpec(podSpecDoc.BytesWithoutCommentKeys())
	if err != nil {
		return nil, true, errors.Wrapf(err, "could not read the pod spec at %s", podSpecPath)
	}
	// Some checks read the pod's annotations, such as the legacy AppArmor ones, which sit beside
	// the PodSpec: under the pod template, or at the root of a Pod.
	var podMetadata metav1.ObjectMeta
	if metadataDoc, ok, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(podMetadataPath(podSpecPath)), true); err != nil {
		return nil, true, err
	} else if ok {
		if err := yaml.Unmarshal(metadataDoc.BytesWithoutCommentKeys(), &podMetadata); err != nil {
			return nil, true, errors.Wrapf(err, "could not read the pod metadata at %s", podMetadataPath(podSpecPath))
		}
	}

	var issues []api.Issue
	for _, check := range evaluator.EvaluatePod(lv, &podMetadata, podSpec) {
		if check.Allowed {
			continue
		}
		message := check.ForbiddenReason
		if check.ForbiddenDetail != "" {
			message += " (" + check.ForbiddenDetail + ")"
		}
		issues = append(issues, api.Issue{
			Identifier: check.ForbiddenReason,
			Message:    fmt.Sprintf("violates PodSecurity %q: %s", lv.String(), message),
		})
	}
	return issues, true, nil
}

// quantityKeys name the PodSpec fields holding resource quantities: container and
// pod resources, overhead, emptyDir sizeLimit and resourceFieldRef divisor. No
// Pod Security check reads them, and a base Unit's placeholder there
// (cpu: confighubplaceholder) is not a quantity, so they are dropped before the
// PodSpec is decoded rather than failing the whole check.
var quantityKeys = map[string]bool{"resources": true, "overhead": true, "sizeLimit": true, "divisor": true}

func decodePodSpec(data []byte) (*corev1.PodSpec, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	dropQuantities(raw)
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var podSpec corev1.PodSpec
	if err := json.Unmarshal(encoded, &podSpec); err != nil {
		return nil, err
	}
	return &podSpec, nil
}

func dropQuantities(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if quantityKeys[k] {
				delete(t, k)
				continue
			}
			dropQuantities(child)
		}
	case []any:
		for _, child := range t {
			dropQuantities(child)
		}
	}
}

// podMetadataPath is where the metadata of the pod whose PodSpec is at podSpecPath sits: beside
// it in a pod template (spec.template.spec -> spec.template.metadata), or at the root of a Pod.
func podMetadataPath(podSpecPath string) string {
	if parent, ok := strings.CutSuffix(podSpecPath, ".spec"); ok {
		return parent + ".metadata"
	}
	return "metadata"
}
