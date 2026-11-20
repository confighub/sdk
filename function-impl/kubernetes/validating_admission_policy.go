// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	plugincel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/cel/environment"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/function-impl/third_party/admissionpolicy"
)

// PolicyOtherDataKey is the key of the OtherData vet-validating-admission-policy reads its
// ValidatingAdmissionPolicies from.
const PolicyOtherDataKey = api.OtherDataSource("policy")

// ParamsOtherDataKey is the key of the OtherData vet-validating-admission-policy also finds a
// binding's params in, after the policy Unit: a Unit of params kept apart from the policies, so
// that the policies can be replaced without losing what was set in them.
const ParamsOtherDataKey = api.OtherDataSource("params")

// releasedOtherDataKey is the revision whose resources are each resource's oldObject.
const releasedOtherDataKey = api.OtherDataSource("LastReleasedRevisionNum")

const admissionRegistrationGroup = "admissionregistration.k8s.io"

func registerVetValidatingAdmissionPolicy(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	if err := fh.RegisterFunction("vet-validating-admission-policy", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-validating-admission-policy",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "policy",
					Required:      false,
					Description:   "Comma-separated names of the ValidatingAdmissionPolicies in the policy Unit to evaluate. Omitted, all of them",
					DataType:      api.DataTypeCSV,
				},
				{
					ParameterName: "exclude-policy",
					Required:      false,
					Description:   "Comma-separated names of ValidatingAdmissionPolicies in the policy Unit not to evaluate",
					DataType:      api.DataTypeCSV,
				},
			},
			OtherDataExpected: []api.OtherDataSource{PolicyOtherDataKey, ParamsOtherDataKey, releasedOtherDataKey},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no ValidatingAdmissionPolicy denies any resource, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:   false,
			Validating: true,
			Hermetic:   true,
			Idempotent: true,
			Description: "Fails a resource that a ValidatingAdmissionPolicy in the Unit passed as OtherData policy, such as by a Trigger's OtherUnitIDs, denies, " +
				"or in the Units a Trigger's OtherUnitSelections pass as policy, which may be none, when nothing denies anything, " +
				"evaluated as the Kubernetes API server would admit the resource: as an update of the resource in the Unit's last released revision, " +
				"which is oldObject, or as its creation when that revision does not have it. A policy is applied through each ValidatingAdmissionPolicyBinding " +
				"in that Unit that names it, or with the Deny action when none does; a binding's paramRef finds its params among that Unit's other resources, " +
				"then among those of the Unit passed as OtherData params, and a namespaceSelector reads the labels of the Namespace resources in the Unit being validated. " +
				"policy and exclude-policy evaluate some of the policies rather than all. Each finding is an Issue identified by its policy's name. " +
				"The request has no user and expressions cannot use the authorizer. " +
				"Fix: change the resource as the policy's message says, or change the policy or its params.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			_, selected := fArgs.OtherDataOrigins[PolicyOtherDataKey]
			inputs := AdmissionPolicyInputs{
				Policy:         fArgs.ParsedOtherData[PolicyOtherDataKey],
				PolicySelected: selected,
				Params:         fArgs.ParsedOtherData[ParamsOtherDataKey],
				Released:       fArgs.ParsedOtherData[releasedOtherDataKey],
			}
			for _, arg := range fArgs.Arguments {
				s, ok := arg.Value.(string)
				if !ok {
					continue
				}
				switch arg.ParameterName {
				case "policy":
					inputs.Include = splitNames(s)
				case "exclude-policy":
					inputs.Exclude = splitNames(s)
				}
			}
			return VetValidatingAdmissionPolicyWith(rp, fArgs.Options, fArgs.ParsedData, inputs)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// boundPolicy is a ValidatingAdmissionPolicy as one binding applies it.
type boundPolicy struct {
	policy    *admissionregistrationv1.ValidatingAdmissionPolicy
	binding   *admissionregistrationv1.ValidatingAdmissionPolicyBinding
	validator admissionpolicy.Validator
}

// name says which policy, and which binding of it, a finding comes from.
func (b *boundPolicy) name() string {
	if b.binding.Name == "" {
		return b.policy.Name
	}
	return b.policy.Name + "/" + b.binding.Name
}

func (b *boundPolicy) failurePolicy() admissionregistrationv1.FailurePolicyType {
	if b.policy.Spec.FailurePolicy == nil {
		return admissionregistrationv1.Fail
	}
	return *b.policy.Spec.FailurePolicy
}

// denies says whether a violation fails validation, as the binding's Deny action makes it fail
// admission. A binding that only warns or audits reports what it found without failing.
func (b *boundPolicy) denies() bool {
	if len(b.binding.Spec.ValidationActions) == 0 {
		return true
	}
	for _, action := range b.binding.Spec.ValidationActions {
		if action == admissionregistrationv1.Deny {
			return true
		}
	}
	return false
}

// admissionPolicies is what the Unit passed as OtherData policy holds: its policies, each as its
// bindings apply it, and the rest of its resources, which a binding's paramRef may name, as it may
// name the resources of the Unit passed as OtherData params.
type admissionPolicies struct {
	bound  []*boundPolicy
	others []*unstructured.Unstructured
	params []*unstructured.Unstructured
}

// AdmissionPolicyInputs is what vet-validating-admission-policy evaluates a Unit's resources with,
// besides the resources themselves.
type AdmissionPolicyInputs struct {
	// Policy is the data of the Unit holding the policies and their bindings.
	Policy gaby.Container
	// PolicySelected says Policy holds what a selection picked, the resources of several Units,
	// rather than a Unit named for its policies. A selection that picks none denies nothing,
	// where a named Unit with none is a mistake to report.
	PolicySelected bool
	// Params is the data of a Unit holding params, which a binding's paramRef finds after any in
	// Policy. Optional.
	Params gaby.Container
	// Released is the data of the Unit's last released revision, whose resources are the
	// oldObjects of the ones being validated. Optional: without it, every resource is created.
	Released gaby.Container
	// Include names the policies to evaluate, all of them when empty; Exclude, those not to.
	Include, Exclude []string
}

func splitNames(list string) []string {
	var names []string
	for _, name := range strings.Split(list, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// readAdmissionPolicies reads the policy Unit's data and compiles each of its policies that
// inputs selects, and reads the params Unit's resources.
func readAdmissionPolicies(inputs AdmissionPolicyInputs) (*admissionPolicies, error) {
	var policies []*admissionregistrationv1.ValidatingAdmissionPolicy
	bindings := map[string][]*admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	read := &admissionPolicies{}
	for _, doc := range inputs.Params {
		if doc == nil || doc.Data() == nil {
			continue
		}
		object, err := unstructuredFromDoc(doc)
		if err != nil {
			return nil, errors.Wrapf(err, "the Unit passed as OtherData %s", ParamsOtherDataKey)
		}
		read.params = append(read.params, object)
	}
	for _, doc := range inputs.Policy {
		if doc == nil || doc.Data() == nil {
			continue
		}
		object, err := unstructuredFromDoc(doc)
		if err != nil {
			return nil, errors.Wrapf(err, "the Unit passed as OtherData %s", PolicyOtherDataKey)
		}
		gvk := object.GroupVersionKind()
		switch {
		case gvk.Group == admissionRegistrationGroup && gvk.Kind == "ValidatingAdmissionPolicy":
			policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			if err := fromUnstructured(object, policy); err != nil {
				return nil, err
			}
			policies = append(policies, policy)
		case gvk.Group == admissionRegistrationGroup && gvk.Kind == "ValidatingAdmissionPolicyBinding":
			binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := fromUnstructured(object, binding); err != nil {
				return nil, err
			}
			bindings[binding.Spec.PolicyName] = append(bindings[binding.Spec.PolicyName], binding)
		default:
			read.others = append(read.others, object)
		}
	}
	if len(policies) == 0 && !inputs.PolicySelected {
		return nil, errors.Newf("no ValidatingAdmissionPolicy to evaluate: pass a Unit holding one as OtherData %s", PolicyOtherDataKey)
	}
	// A name the policy Unit does not have is a mistake in the Trigger, which would otherwise
	// pass everything without saying so.
	for _, name := range slices.Concat(inputs.Include, inputs.Exclude) {
		if !slices.ContainsFunc(policies, func(p *admissionregistrationv1.ValidatingAdmissionPolicy) bool { return p.Name == name }) {
			return nil, errors.Newf("the Unit passed as OtherData %s has no ValidatingAdmissionPolicy named %q", PolicyOtherDataKey, name)
		}
	}
	policies = slices.DeleteFunc(policies, func(p *admissionregistrationv1.ValidatingAdmissionPolicy) bool {
		return (len(inputs.Include) > 0 && !slices.Contains(inputs.Include, p.Name)) || slices.Contains(inputs.Exclude, p.Name)
	})
	for _, policy := range policies {
		validator := compileAdmissionPolicy(policy)
		policyBindings := bindings[policy.Name]
		if len(policyBindings) == 0 {
			// A policy has no effect in a cluster until something binds it. Here the Unit was
			// passed to be enforced, so a policy it does not bind is applied as a binding with
			// no parameters, no further matching and the Deny action would apply it.
			policyBindings = []*admissionregistrationv1.ValidatingAdmissionPolicyBinding{{
				Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName:        policy.Name,
					ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny},
				},
			}}
		}
		for _, binding := range policyBindings {
			read.bound = append(read.bound, &boundPolicy{policy: policy, binding: binding, validator: validator})
		}
	}
	return read, nil
}

// compileAdmissionPolicy compiles a policy as the API server's validating admission plugin does,
// without an authorizer, which nothing here can answer.
func compileAdmissionPolicy(policy *admissionregistrationv1.ValidatingAdmissionPolicy) admissionpolicy.Validator {
	hasParams := policy.Spec.ParamKind != nil
	optionalVars := plugincel.OptionalVariableDeclarations{HasParams: hasParams}
	compiler, err := plugincel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
	if err != nil {
		// The base environment is built from the library's own declarations, so it does not
		// fail for anything a policy says.
		panic(err)
	}
	variables := make([]plugincel.NamedExpressionAccessor, len(policy.Spec.Variables))
	for i, variable := range policy.Spec.Variables {
		variables[i] = &admissionpolicy.Variable{Name: variable.Name, Expression: variable.Expression}
	}
	compiler.CompileAndStoreVariables(variables, optionalVars, environment.StoredExpressions)

	var matcher matchconditions.Matcher
	if len(policy.Spec.MatchConditions) > 0 {
		conditions := make([]plugincel.ExpressionAccessor, len(policy.Spec.MatchConditions))
		for i := range policy.Spec.MatchConditions {
			conditions[i] = (*matchconditions.MatchCondition)(&policy.Spec.MatchConditions[i])
		}
		matcher = matchconditions.NewMatcher(compiler.CompileCondition(conditions, optionalVars, environment.StoredExpressions),
			policy.Spec.FailurePolicy, "policy", "validate", policy.Name)
	}
	validations := make([]plugincel.ExpressionAccessor, len(policy.Spec.Validations))
	messages := make([]plugincel.ExpressionAccessor, len(policy.Spec.Validations))
	for i, validation := range policy.Spec.Validations {
		validations[i] = &admissionpolicy.ValidationCondition{Expression: validation.Expression, Message: validation.Message, Reason: validation.Reason}
		if validation.MessageExpression != "" {
			messages[i] = &admissionpolicy.MessageExpressionCondition{MessageExpression: validation.MessageExpression}
		}
	}
	auditAnnotations := make([]plugincel.ExpressionAccessor, len(policy.Spec.AuditAnnotations))
	for i, annotation := range policy.Spec.AuditAnnotations {
		auditAnnotations[i] = &admissionpolicy.AuditAnnotationCondition{Key: annotation.Key, ValueExpression: annotation.ValueExpression}
	}
	return admissionpolicy.NewValidator(
		compiler.CompileCondition(validations, optionalVars, environment.StoredExpressions),
		matcher,
		compiler.CompileCondition(auditAnnotations, optionalVars, environment.StoredExpressions),
		compiler.CompileCondition(messages, optionalVars, environment.StoredExpressions),
		policy.Spec.FailurePolicy,
	)
}

// VetValidatingAdmissionPolicy evaluates the ValidatingAdmissionPolicies policyData holds, the data
// of the Unit passed as OtherData policy, against each resource of parsedData, as created.
func VetValidatingAdmissionPolicy(rp yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData, policyData gaby.Container) (gaby.Container, any, error) {
	return VetValidatingAdmissionPolicyWith(rp, options, parsedData, AdmissionPolicyInputs{Policy: policyData})
}

// VetValidatingAdmissionPolicyWith evaluates the ValidatingAdmissionPolicies inputs selects against
// each resource of parsedData, as an update of the same resource in inputs.Released or, when that
// has none, as its creation.
func VetValidatingAdmissionPolicyWith(rp yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container, inputs AdmissionPolicyInputs) (gaby.Container, any, error) {
	policies, err := readAdmissionPolicies(inputs)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}
	namespaces, err := namespacesIn(parsedData)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}
	objectInterfaces := admission.NewObjectInterfacesFromScheme(runtime.NewScheme())

	result := api.ValidationResult{Passed: true}
	_, err = yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		object, err := unstructuredFromDoc(doc)
		if err != nil {
			return output, []error{errors.Wrapf(err, "resource %s", resourceInfo.ResourceName)}
		}
		var oldObject *unstructured.Unstructured
		if len(inputs.Released) > 0 {
			if oldDoc, _ := yamlkit.FindResourceDoc(inputs.Released, rp, resourceInfo); oldDoc != nil {
				if oldObject, err = unstructuredFromDoc(oldDoc); err != nil {
					return output, []error{errors.Wrapf(err, "released resource %s", resourceInfo.ResourceName)}
				}
			}
		}
		attributes := admissionAttributesFor(object, oldObject)
		for _, bound := range policies.bound {
			for _, finding := range evaluateBoundPolicy(bound, policies, objectInterfaces, namespaces, attributes) {
				detail := fmt.Sprintf("%s %s: %s: %s", resourceInfo.ResourceType, resourceInfo.ResourceName, bound.name(), finding)
				if !bound.denies() {
					result.Details = append(result.Details, "warning: "+detail)
					continue
				}
				result.Passed = false
				result.Details = append(result.Details, detail)
				result.FailedAttributes = append(result.FailedAttributes, api.AttributeValue{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{ResourceInfo: *resourceInfo, Path: "."},
						AttributeMetadata:   api.AttributeMetadata{AttributeName: api.AttributeNameNone},
					},
					Issues: []api.Issue{{Identifier: bound.policy.Name, Message: finding}},
				})
			}
		}
		return output, nil
	})
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}
	return parsedData, result, nil
}

// evaluateBoundPolicy returns what the policy, as the binding applies it, denies about the
// resource the attributes describe: nothing when it does not apply or admits the resource.
func evaluateBoundPolicy(bound *boundPolicy, policies *admissionPolicies, objectInterfaces admission.ObjectInterfaces,
	namespaces namespaceLister, attributes admission.Attributes) []string {
	// A failure to decide is a denial unless the policy says to ignore failures, as in the
	// API server.
	failed := func(message string) []string {
		if bound.failurePolicy() == admissionregistrationv1.Ignore {
			return nil
		}
		return []string{message}
	}

	matches, matchedResource, matchedKind, err := admissionpolicy.DefinitionMatches(attributes, objectInterfaces,
		namespaces.labels, bound.policy.Spec.MatchConstraints)
	if err != nil {
		return failed("failed to match the policy: " + err.Error())
	}
	if !matches {
		return nil
	}
	matches, err = admissionpolicy.BindingMatches(attributes, objectInterfaces,
		namespaces.labels, bound.binding.Spec.MatchResources)
	if err != nil {
		return failed("failed to match the binding: " + err.Error())
	}
	if !matches {
		return nil
	}

	versionedAttributes, err := admission.NewVersionedAttributes(attributes, matchedKind, objectInterfaces)
	if err != nil {
		return failed("failed to read the resource as " + matchedKind.String() + ": " + err.Error())
	}
	var namespace *corev1.Namespace
	if name := attributes.GetNamespace(); name != "" {
		namespace = namespaces.get(name)
	}

	paramSets := []runtime.Object{nil}
	if paramKind := bound.policy.Spec.ParamKind; paramKind != nil {
		paramRef := bound.binding.Spec.ParamRef
		if paramRef == nil {
			return failed(fmt.Sprintf("the policy takes %s params, and nothing binds them", paramKind.Kind))
		}
		paramSets = policies.paramsFor(paramKind, paramRef, attributes.GetNamespace())
		if len(paramSets) == 0 {
			if paramRef.ParameterNotFoundAction != nil && *paramRef.ParameterNotFoundAction == admissionregistrationv1.AllowAction {
				return nil
			}
			return failed(fmt.Sprintf("no %s params found for the binding in the policy Unit", paramKind.Kind))
		}
	}

	var findings []string
	for _, params := range paramSets {
		validateResult := bound.validator.Validate(context.Background(), matchedResource, versionedAttributes, params,
			namespace, celconfig.RuntimeCELCostBudget, nil)
		for _, decision := range validateResult.Decisions {
			if decision.Action == admissionpolicy.ActionDeny {
				findings = append(findings, decision.Message)
			}
		}
	}
	return findings
}

// paramsFor finds the params a binding's paramRef names among the policy Unit's other resources,
// or when it has none, among the params Unit's. A paramRef without a namespace finds params in the
// namespace of the resource being validated, or cluster-scoped ones.
func (p *admissionPolicies) paramsFor(paramKind *admissionregistrationv1.ParamKind, paramRef *admissionregistrationv1.ParamRef, namespace string) []runtime.Object {
	if params := findParams(p.others, paramKind, paramRef, namespace); len(params) > 0 {
		return params
	}
	return findParams(p.params, paramKind, paramRef, namespace)
}

func findParams(candidates []*unstructured.Unstructured, paramKind *admissionregistrationv1.ParamKind, paramRef *admissionregistrationv1.ParamRef, namespace string) []runtime.Object {
	selector := labels.Everything()
	if paramRef.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(paramRef.Selector); err != nil {
			return nil
		}
	}
	wantNamespace := paramRef.Namespace
	if wantNamespace == "" {
		wantNamespace = namespace
	}
	var params []runtime.Object
	for _, candidate := range candidates {
		switch {
		case candidate.GetAPIVersion() != paramKind.APIVersion || candidate.GetKind() != paramKind.Kind:
		case paramRef.Name != "" && candidate.GetName() != paramRef.Name:
		case candidate.GetNamespace() != "" && candidate.GetNamespace() != wantNamespace:
		case !selector.Matches(labels.Set(candidate.GetLabels())):
		default:
			params = append(params, candidate)
		}
	}
	return params
}

// admissionAttributesFor describes the creation of a resource, or with oldObject, its update, as
// the API server would be asked to admit it. The resource is named by guessing it from the kind,
// as a REST mapper with no discovery does.
func admissionAttributesFor(object, oldObject *unstructured.Unstructured) admission.Attributes {
	gvk := object.GroupVersionKind()
	resource, _ := meta.UnsafeGuessKindToResource(gvk)
	if irregular, ok := irregularResources[gvk.GroupKind()]; ok {
		resource.Resource = irregular
	}
	if oldObject != nil {
		return admission.NewAttributesRecord(object, oldObject, gvk, object.GetNamespace(), object.GetName(), resource, "",
			admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{})
	}
	return admission.NewAttributesRecord(object, nil, gvk, object.GetNamespace(), object.GetName(), resource, "",
		admission.Create, &metav1.CreateOptions{}, false, &user.DefaultInfo{})
}

// irregularResources are the built-in kinds whose resource is not the guess from the kind.
var irregularResources = map[schema.GroupKind]string{
	{Group: "", Kind: "Endpoints"}: "endpoints",
}

// namespaceLister answers a policy's namespaceSelector from the Namespaces in the Unit being
// validated. A Namespace that is not there has only the label the API server gives every
// Namespace, its name.
type namespaceLister map[string]*corev1.Namespace

func namespacesIn(parsedData gaby.Container) (namespaceLister, error) {
	namespaces := namespaceLister{}
	for _, doc := range parsedData {
		if doc == nil || doc.Data() == nil {
			continue
		}
		object, err := unstructuredFromDoc(doc)
		if err != nil || object.GetAPIVersion() != "v1" || object.GetKind() != "Namespace" {
			continue
		}
		namespace := &corev1.Namespace{}
		if err := fromUnstructured(object, namespace); err != nil {
			return nil, err
		}
		namespaces[namespace.Name] = withNameLabel(namespace)
	}
	return namespaces, nil
}

func withNameLabel(namespace *corev1.Namespace) *corev1.Namespace {
	if namespace.Labels == nil {
		namespace.Labels = map[string]string{}
	}
	if _, ok := namespace.Labels[corev1.LabelMetadataName]; !ok {
		namespace.Labels[corev1.LabelMetadataName] = namespace.Name
	}
	return namespace
}

func (l namespaceLister) get(name string) *corev1.Namespace {
	if namespace, ok := l[name]; ok {
		return namespace
	}
	return withNameLabel(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
}

func (l namespaceLister) labels(name string) (map[string]string, error) {
	return l.get(name).Labels, nil
}

// unstructuredFromDoc reads a resource of a Unit's data as the API server would receive it.
func unstructuredFromDoc(doc *gaby.YamlDoc) (*unstructured.Unstructured, error) {
	docJSON, err := doc.MarshalJSONWithoutCommentKeys()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON(docJSON); err != nil {
		return nil, errors.Wrap(err, "not a Kubernetes resource")
	}
	dropNullFields(object.Object)
	return object, nil
}

// dropNullFields removes the fields set to null, at any depth. The API server decodes a resource
// into its type before admission, and prunes a custom resource by its schema, so a field of a
// struct written as null is unset there, and has() is false for it. Left in, a null fails every
// expression that uses the field, even one guarded by has().
//
// Without the resource's schema a struct can't be told from a map, so this differs from a cluster
// in two cases. A null value in a map of strings, such as a label, annotation or ConfigMap key, is
// kept by the API server as an empty string, and is removed here. A field of a custom resource
// marked nullable keeps its null in a cluster, and is removed here.
func dropNullFields(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, field := range value {
			if field == nil {
				delete(value, key)
				continue
			}
			dropNullFields(field)
		}
	case []any:
		for _, item := range value {
			dropNullFields(item)
		}
	}
}

// fromUnstructured decodes a resource into its typed form. A policy of an earlier version of the
// API, such as v1beta1, has the same fields as v1.
func fromUnstructured(object *unstructured.Unstructured, into any) error {
	data, err := json.Marshal(object.Object)
	if err != nil {
		return errors.WithStack(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		return errors.Wrapf(err, "%s %s is not a valid %s", object.GetKind(), object.GetName(), object.GetKind())
	}
	return nil
}
