// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
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

// podSecurityEvaluators holds an evaluator per selection of checks, keyed by the selected IDs.
// Building one validates its checks and lays them out by version, so each is built once.
var podSecurityEvaluators sync.Map

// podSecurityCheckIDs are the IDs of the Pod Security Standards checks, which the check and
// exclude-check parameters name.
var podSecurityCheckIDs = func() []string {
	var ids []string
	for _, check := range policy.DefaultChecks() {
		ids = append(ids, string(check.ID))
	}
	slices.Sort(ids)
	return ids
}()

// podSecurityEvaluator runs the Pod Security Standards checks that the PodSecurity admission
// controller runs, so a pod this function passes is one the cluster admits at the same level and
// version: all of them, or the ones selected. A selection is evaluated as the admission
// controller evaluates all of them, so a restricted check still overrides the baseline check it
// replaces when both are selected; a baseline check whose restricted replacement is not selected
// runs at the restricted level.
//
// A check's result does not say which check it is, so each check is wrapped to put its ID in
// front of its reason, separated by checkIDSeparator, which podSecurityIssues takes off again.
func podSecurityEvaluator(selected []string) (policy.Evaluator, error) {
	key := strings.Join(selected, ",")
	if evaluator, ok := podSecurityEvaluators.Load(key); ok {
		return evaluator.(policy.Evaluator), nil
	}
	var checks []policy.Check
	for _, check := range policy.DefaultChecks() {
		if selected != nil && !slices.Contains(selected, string(check.ID)) {
			continue
		}
		versions := make([]policy.VersionedCheck, len(check.Versions))
		for i, versioned := range check.Versions {
			versions[i] = versioned
			checkPod, id := versioned.CheckPod, string(check.ID)
			versions[i].CheckPod = func(podMetadata *metav1.ObjectMeta, podSpec *corev1.PodSpec) policy.CheckResult {
				result := checkPod(podMetadata, podSpec)
				if !result.Allowed {
					result.ForbiddenReason = id + checkIDSeparator + result.ForbiddenReason
				}
				return result
			}
		}
		check.Versions = versions
		checks = append(checks, check)
	}
	evaluator, err := policy.NewEvaluator(checks)
	if err != nil {
		return nil, err
	}
	podSecurityEvaluators.Store(key, evaluator)
	return evaluator, nil
}

const checkIDSeparator = "\x00"

// selectPodSecurityChecks is the sorted list of checks to run given the check and exclude-check
// arguments, each a comma-separated list of check IDs, or nil for all of them.
func selectPodSecurityChecks(include, exclude string) ([]string, error) {
	parse := func(param, list string) ([]string, error) {
		var ids []string
		for _, id := range strings.Split(list, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if !slices.Contains(podSecurityCheckIDs, id) {
				return nil, errors.Errorf("%s: unknown check %q; the checks are %s", param, id, strings.Join(podSecurityCheckIDs, ", "))
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	included, err := parse("check", include)
	if err != nil {
		return nil, err
	}
	excluded, err := parse("exclude-check", exclude)
	if err != nil {
		return nil, err
	}
	if len(included) == 0 && len(excluded) == 0 {
		return nil, nil
	}
	if len(included) == 0 {
		included = podSecurityCheckIDs
	}
	selected := []string{}
	for _, id := range podSecurityCheckIDs {
		if slices.Contains(included, id) && !slices.Contains(excluded, id) {
			selected = append(selected, id)
		}
	}
	return selected, nil
}

func registerPodSecurityFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	if err := fh.RegisterFunction("vet-pod-security-standard", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-pod-security-standard",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "level",
					Required:      false,
					Description:   `Pod Security Standard level every workload must meet: "privileged", "baseline", or "restricted"; or "namespace", the level in the pod-security.kubernetes.io/enforce label of each workload's Namespace, found in the Unit or among the resources passed as OtherData namespaces, such as by a Trigger's OtherUnitSelections, the strictest counting when several of those declare one Namespace, failing a workload whose Namespace is not found or has no such label. Omitted, each workload is checked at the level in its own pod-security.kubernetes.io/enforce label, and a workload without one is not checked`,
					DataType:      api.DataTypeString,
					ValueConstraints: api.ValueConstraints{EnumValues: []string{
						string(psaapi.LevelPrivileged), string(psaapi.LevelBaseline), string(psaapi.LevelRestricted), fromNamespace,
					}},
				},
				{
					ParameterName: "version",
					Required:      false,
					Description:   `Version of the standard to check against, as in the pod-security.kubernetes.io/enforce-version label: "latest" (the default) or a Kubernetes minor version such as "v1.30"; or "namespace", the version in the pod-security.kubernetes.io/enforce-version label of each workload's Namespace, found as for level, or "latest" when it has none, failing a workload whose Namespace is not found. A workload's own enforce-version label takes precedence when level is omitted`,
					DataType:      api.DataTypeString,
					DefaultValue:  "latest",
				},
				{
					ParameterName: "check",
					Required:      false,
					Description:   "Comma-separated IDs of the checks to run, of those the level includes, such as privileged,hostNamespaces. Omitted, all of them. The IDs are " + strings.Join(podSecurityCheckIDs, ", "),
					DataType:      api.DataTypeCSV,
				},
				{
					ParameterName: "exclude-check",
					Required:      false,
					Description:   "Comma-separated IDs of checks not to run, such as runAsNonRoot",
					DataType:      api.DataTypeCSV,
				},
			},
			RequiredParameters: 0,
			OtherDataExpected:  []api.OtherDataSource{NamespacesOtherDataKey},
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
			Description: "Fails a pod template that does not meet a Pod Security Standard level " +
				"(https://kubernetes.io/docs/concepts/security/pod-security-standards/), using the same checks the " +
				"PodSecurity admission controller applies: the level given, or without one, the level in each workload's own " +
				"pod-security.kubernetes.io/enforce label, as on its namespace, so one Trigger can hold every workload to " +
				"its namespace's level, or with level namespace, the level labeled on the workload's Namespace itself. check and exclude-check run some of the level's checks rather than all. Each " +
				"failure is an Issue identified by the check's ID. Passes when there are no workloads, or none to check. " +
				"Fix: set what the check names in the pod's or the container's securityContext, or hold the workload to a lower level.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: k8skit.PodSpecResourceTypes(),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var level, version, include, exclude string
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
				case "check":
					include = s
				case "exclude-check":
					exclude = s
				}
			}
			selected, err := selectPodSecurityChecks(include, exclude)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			namespaces := namespacesByName(fArgs.ParsedData, fArgs.ParsedOtherData[NamespacesOtherDataKey])
			return k8sFnVetPodSecurityStandard(rp, fArgs.ParsedData, level, version, selected, namespaces, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// fromNamespace is the level or version argument that reads it from the workload's Namespace.
const fromNamespace = "namespace"

// NamespacesOtherDataKey is the key of the OtherData vet-pod-security-standard finds the Namespaces
// of the workloads it checks among, besides those in the Unit, when level or version is namespace.
const NamespacesOtherDataKey = api.OtherDataSource("namespaces")

// namespacesByName indexes, by name, the Namespace resources in the Unit and those passed as
// OtherData. The Unit's own declaration of a Namespace is the one used. Among those passed, which
// can declare one Namespace more than once, as two Units released to one cluster can, the one with
// the strictest enforce label is used, and between two at the same level the one with the newer
// enforce-version, whatever order they were passed in: a declaration without the label does not
// lift the level another sets, and an invalid label is reported rather than passed over.
func namespacesByName(own gaby.Container, passed gaby.Container) map[string]*gaby.YamlDoc {
	namespaces := map[string]*gaby.YamlDoc{}
	ownNames := map[string]bool{}
	for _, doc := range own {
		if name := namespaceName(doc); name != "" && !ownNames[name] {
			namespaces[name] = doc
			ownNames[name] = true
		}
	}
	for _, doc := range passed {
		name := namespaceName(doc)
		if name == "" || ownNames[name] {
			continue
		}
		if chosen, seen := namespaces[name]; !seen || stricterNamespace(doc, chosen) {
			namespaces[name] = doc
		}
	}
	return namespaces
}

// namespaceName is the name of a v1 Namespace, or empty for any other resource.
func namespaceName(doc *gaby.YamlDoc) string {
	if doc == nil {
		return ""
	}
	kind, _ := doc.Search("kind").Data().(string)
	apiVersion, _ := doc.Search("apiVersion").Data().(string)
	name, _ := doc.Search("metadata", "name").Data().(string)
	if kind != "Namespace" || apiVersion != "v1" {
		return ""
	}
	return name
}

// stricterNamespace says whether one declaration of a Namespace holds workloads to more than
// another: a stricter enforce level, or the same level at a newer enforce-version, as a version of
// the standards adds to what the one before it checks. A Namespace without the version label is
// held to the latest, and one with an invalid label sorts above any valid one, so it is reported.
func stricterNamespace(namespace, than *gaby.YamlDoc) bool {
	if a, b := enforceStrictness(namespace), enforceStrictness(than); a != b {
		return a > b
	}
	version, versionValid := enforceVersion(namespace)
	thanVersion, thanValid := enforceVersion(than)
	if versionValid != thanValid {
		return !versionValid
	}
	return versionValid && thanVersion.Older(version)
}

// enforceVersion is a Namespace's enforce-version label, latest when it has none, and whether the
// label is valid.
func enforceVersion(namespace *gaby.YamlDoc) (psaapi.Version, bool) {
	value, ok := namespace.Search("metadata", "labels", podSecurityEnforceVersionLabel).Data().(string)
	if !ok {
		return psaapi.LatestVersion(), true
	}
	version, err := psaapi.ParseVersion(value)
	return version, err == nil
}

// enforceStrictness orders Namespaces by their enforce label: none, then privileged, baseline and
// restricted, then an invalid one.
func enforceStrictness(namespace *gaby.YamlDoc) int {
	value, ok := namespace.Search("metadata", "labels", podSecurityEnforceLabel).Data().(string)
	if !ok {
		return 0
	}
	level, err := psaapi.ParseLevel(value)
	if err != nil {
		return 4
	}
	switch level {
	case psaapi.LevelPrivileged:
		return 1
	case psaapi.LevelBaseline:
		return 2
	default:
		return 3
	}
}

// The labels that set a namespace's Pod Security Standard level. On a workload they say which level
// it is held to when vet-pod-security-standard is given none.
const (
	podSecurityEnforceLabel        = "pod-security.kubernetes.io/enforce"
	podSecurityEnforceVersionLabel = "pod-security.kubernetes.io/enforce-version"
)

func k8sFnVetPodSecurityStandard(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, levelArg, versionArg string, selected []string, namespaces map[string]*gaby.YamlDoc, options *api.FunctionOptions) (gaby.Container, any, error) {
	if versionArg == "" {
		versionArg = "latest"
	}
	versionFromNamespace := versionArg == fromNamespace
	version := psaapi.LatestVersion()
	if !versionFromNamespace {
		var err error
		if version, err = psaapi.ParseVersion(versionArg); err != nil {
			return parsedData, nil, errors.Wrapf(err, "invalid version %q", versionArg)
		}
	}
	levelFromNamespace := levelArg == fromNamespace
	// With a level, every workload is held to it; without one, each is held to its own label, or
	// with namespace, to its Namespace's.
	var fixed *psaapi.LevelVersion
	if levelArg != "" && !levelFromNamespace {
		level, err := psaapi.ParseLevel(levelArg)
		if err != nil {
			return parsedData, nil, errors.Wrapf(err, "invalid level %q", levelArg)
		}
		fixed = &psaapi.LevelVersion{Level: level, Version: version}
	}
	evaluator, err := podSecurityEvaluator(selected)
	if err != nil {
		return parsedData, nil, errors.Wrap(err, "failed to build the Pod Security Standards checks")
	}

	result := api.ValidationResult{Passed: true}
	_, err = yamlkit.VisitResourcesFiltered(parsedData, &result, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		vr := output.(*api.ValidationResult)
		podSpecPaths := k8skit.PodSpecPaths(resourceInfo.ResourceType)
		if len(podSpecPaths) == 0 {
			return vr, nil
		}
		workloadVersion := version
		var namespace *gaby.YamlDoc
		if levelFromNamespace || versionFromNamespace {
			found, missing := workloadNamespace(doc, namespaces)
			if missing != nil {
				vr.Passed = false
				missing.ResourceInfo = *resourceInfo
				vr.FailedAttributes = append(vr.FailedAttributes, *missing)
				return vr, nil
			}
			namespace = found
		}
		if versionFromNamespace {
			namespaceVersion, invalid := labeledVersion(namespace, version)
			if invalid != nil {
				vr.Passed = false
				invalid.ResourceInfo = *resourceInfo
				vr.FailedAttributes = append(vr.FailedAttributes, *invalid)
				return vr, nil
			}
			workloadVersion = namespaceVersion
		}
		lv := fixed
		if lv != nil && versionFromNamespace {
			lv = &psaapi.LevelVersion{Level: fixed.Level, Version: workloadVersion}
		}
		if lv == nil && levelFromNamespace {
			labeled, invalid := labeledLevel(namespace, workloadVersion)
			if invalid == nil && labeled == nil {
				invalid = missingNamespaceLabel(namespace)
			}
			if invalid != nil {
				vr.Passed = false
				invalid.ResourceInfo = *resourceInfo
				vr.FailedAttributes = append(vr.FailedAttributes, *invalid)
				return vr, nil
			}
			if !versionFromNamespace {
				// The level is the Namespace's; its version is the argument's.
				labeled.Version = version
			}
			lv = labeled
		}
		if lv == nil {
			labeled, invalid := labeledLevel(doc, workloadVersion)
			if invalid != nil {
				vr.Passed = false
				invalid.ResourceInfo = *resourceInfo
				vr.FailedAttributes = append(vr.FailedAttributes, *invalid)
				return vr, nil
			}
			if labeled == nil {
				return vr, nil
			}
			lv = labeled
		}
		var errs []error
		for _, podSpecPath := range podSpecPaths {
			issues, found, err := evaluatePodSecurity(evaluator, *lv, doc, podSpecPath)
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

// workloadNamespace finds the Namespace of a workload, named by its metadata.namespace or
// "default". Not found, it returns a failed attribute saying so, without the ResourceInfo the
// caller fills in.
func workloadNamespace(doc *gaby.YamlDoc, namespaces map[string]*gaby.YamlDoc) (*gaby.YamlDoc, *api.AttributeValue) {
	name, _ := doc.Search("metadata", "namespace").Data().(string)
	if name == "" {
		name = "default"
	}
	if namespace, ok := namespaces[name]; ok {
		return namespace, nil
	}
	return nil, &api.AttributeValue{
		AttributeInfo: api.AttributeInfo{
			AttributeIdentifier: api.AttributeIdentifier{Path: "metadata.namespace"},
			AttributeMetadata:   api.AttributeMetadata{AttributeName: api.AttributeNameNone},
		},
		Value: name,
		Issues: []api.Issue{{Identifier: "missing namespace", Message: fmt.Sprintf(
			"the Namespace %s, whose Pod Security labels the workload is held to, is neither in the Unit nor passed as OtherData %s", name, NamespacesOtherDataKey)}},
	}
}

// missingNamespaceLabel is the failure of a workload whose Namespace has no enforce label to hold
// it to, without the ResourceInfo the caller fills in.
func missingNamespaceLabel(namespace *gaby.YamlDoc) *api.AttributeValue {
	name, _ := namespace.Search("metadata", "name").Data().(string)
	return &api.AttributeValue{
		AttributeInfo: api.AttributeInfo{
			AttributeIdentifier: api.AttributeIdentifier{Path: "metadata.namespace"},
			AttributeMetadata:   api.AttributeMetadata{AttributeName: api.AttributeNameNone},
		},
		Value: name,
		Issues: []api.Issue{{Identifier: "missing " + podSecurityEnforceLabel, Message: fmt.Sprintf(
			"the Namespace %s has no %s label to hold the workload to", name, podSecurityEnforceLabel)}},
	}
}

// labeledVersion is the version in a Namespace's enforce-version label, or defaultVersion when it
// has none. A label whose value is not a version is returned as a failed attribute naming it,
// without the ResourceInfo the caller fills in.
func labeledVersion(namespace *gaby.YamlDoc, defaultVersion psaapi.Version) (psaapi.Version, *api.AttributeValue) {
	value, ok := namespace.Search("metadata", "labels", podSecurityEnforceVersionLabel).Data().(string)
	if !ok {
		return defaultVersion, nil
	}
	version, err := psaapi.ParseVersion(value)
	if err != nil {
		return defaultVersion, &api.AttributeValue{
			AttributeInfo: api.AttributeInfo{
				AttributeIdentifier: api.AttributeIdentifier{Path: "metadata.namespace"},
				AttributeMetadata:   api.AttributeMetadata{AttributeName: api.AttributeNameNone},
			},
			Value: value,
			Issues: []api.Issue{{Identifier: "invalid " + podSecurityEnforceVersionLabel, Message: fmt.Sprintf(
				"the Namespace's label %s=%q %s", podSecurityEnforceVersionLabel, value, err.Error())}},
		}
	}
	return version, nil
}

// labeledLevel is the level and version a workload's own labels hold it to, or nil when it has
// no enforce label. A label whose value is not a level or version is returned as a failed
// attribute naming it, without the ResourceInfo the caller fills in.
func labeledLevel(doc *gaby.YamlDoc, defaultVersion psaapi.Version) (*psaapi.LevelVersion, *api.AttributeValue) {
	labelValue := func(key string) (string, bool) {
		value, ok := doc.Search("metadata", "labels", key).Data().(string)
		return value, ok
	}
	invalid := func(key, value, why string) *api.AttributeValue {
		return &api.AttributeValue{
			AttributeInfo: api.AttributeInfo{
				AttributeIdentifier: api.AttributeIdentifier{
					Path: api.ResolvedPath("metadata.labels." + yamlkit.EscapeDotsInPathSegment(key)),
				},
				AttributeMetadata: api.AttributeMetadata{AttributeName: api.AttributeNameNone},
			},
			Value:  value,
			Issues: []api.Issue{{Identifier: "invalid " + key, Message: fmt.Sprintf("label %s=%q %s", key, value, why)}},
		}
	}
	levelValue, ok := labelValue(podSecurityEnforceLabel)
	if !ok {
		return nil, nil
	}
	level, err := psaapi.ParseLevel(levelValue)
	if err != nil {
		return nil, invalid(podSecurityEnforceLabel, levelValue, err.Error())
	}
	version := defaultVersion
	if versionValue, ok := labelValue(podSecurityEnforceVersionLabel); ok {
		if version, err = psaapi.ParseVersion(versionValue); err != nil {
			return nil, invalid(podSecurityEnforceVersionLabel, versionValue, err.Error())
		}
	}
	return &psaapi.LevelVersion{Level: level, Version: version}, nil
}

// evaluatePodSecurity checks the PodSpec at podSpecPath and returns one Issue per check it fails,
// identified by the check's ID and worded as the admission controller words its rejection.
// found is false when the resource has no PodSpec there.
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
		id, reason, _ := strings.Cut(check.ForbiddenReason, checkIDSeparator)
		message := reason
		if check.ForbiddenDetail != "" {
			message += " (" + check.ForbiddenDetail + ")"
		}
		issues = append(issues, api.Issue{
			Identifier: id,
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
