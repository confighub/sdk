/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Adapted from k8s.io/apiserver/pkg/admission/plugin/policy/matching/matching.go and
// k8s.io/apiserver/pkg/admission/plugin/policy/generic/policy_matcher.go (v0.35.1). The
// namespace selector is evaluated against labels a NamespaceLabels function supplies, rather than
// against a namespace lister and API client, and a nil selector matches everything, as the API
// server's defaulting makes it.

package admissionpolicy

import (
	"fmt"

	v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/object"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/rules"
)

// NamespaceLabels returns the labels of the named Namespace.
type NamespaceLabels func(name string) (map[string]string, error)

// DefinitionMatches returns whether a policy's matchConstraints match the request, and the
// resource and kind it matched as.
func DefinitionMatches(attr admission.Attributes, o admission.ObjectInterfaces, namespaceLabels NamespaceLabels,
	constraints *v1.MatchResources) (bool, schema.GroupVersionResource, schema.GroupVersionKind, error) {
	if constraints == nil {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, fmt.Errorf("policy contained no match constraints, a required field")
	}
	return matches(attr, o, namespaceLabels, constraints)
}

// BindingMatches returns whether a binding's matchResources match the request. A binding without
// any matches every request its policy does.
func BindingMatches(attr admission.Attributes, o admission.ObjectInterfaces, namespaceLabels NamespaceLabels,
	matchResources *v1.MatchResources) (bool, error) {
	if matchResources == nil {
		return true, nil
	}
	isMatch, _, _, err := matches(attr, o, namespaceLabels, matchResources)
	return isMatch, err
}

type matchCriteria struct {
	constraints *v1.MatchResources
}

func (m *matchCriteria) GetParsedObjectSelector() (labels.Selector, error) {
	if m.constraints.ObjectSelector == nil {
		return labels.Everything(), nil
	}
	return metav1.LabelSelectorAsSelector(m.constraints.ObjectSelector)
}

func (m *matchCriteria) GetParsedNamespaceSelector() (labels.Selector, error) {
	if m.constraints.NamespaceSelector == nil {
		return labels.Everything(), nil
	}
	return metav1.LabelSelectorAsSelector(m.constraints.NamespaceSelector)
}

func matches(attr admission.Attributes, o admission.ObjectInterfaces, namespaceLabels NamespaceLabels,
	constraints *v1.MatchResources) (bool, schema.GroupVersionResource, schema.GroupVersionKind, error) {
	criteria := &matchCriteria{constraints: constraints}
	matches, matchNsErr := matchNamespaceSelector(criteria, attr, namespaceLabels)
	// Should not return an error here for policy which do not apply to the request, even if err is an unexpected scenario.
	if !matches && matchNsErr == nil {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, nil
	}

	objectMatcher := &object.Matcher{}
	matches, matchObjErr := objectMatcher.MatchObjectSelector(criteria, attr)
	// Should not return an error here for policy which do not apply to the request, even if err is an unexpected scenario.
	if !matches && matchObjErr == nil {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, nil
	}

	matchPolicy := constraints.MatchPolicy
	if isExcluded, _, _, err := matchesResourceRules(constraints.ExcludeResourceRules, matchPolicy, attr, o); isExcluded || err != nil {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, err
	}

	var (
		isMatch       bool
		matchResource schema.GroupVersionResource
		matchKind     schema.GroupVersionKind
		matchErr      error
	)
	if len(constraints.ResourceRules) == 0 {
		isMatch = true
		matchKind = attr.GetKind()
		matchResource = attr.GetResource()
	} else {
		isMatch, matchResource, matchKind, matchErr = matchesResourceRules(constraints.ResourceRules, matchPolicy, attr, o)
	}
	if matchErr != nil {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, matchErr
	}
	if !isMatch {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, nil
	}

	// now that we know this applies to this request otherwise, if there were selector errors, return them
	if matchNsErr != nil {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, matchNsErr
	}
	if matchObjErr != nil {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, matchObjErr
	}

	return true, matchResource, matchKind, nil
}

// matchNamespaceSelector is namespace.Matcher's MatchNamespaceSelector, with the labels of the
// request's Namespace from namespaceLabels.
func matchNamespaceSelector(criteria *matchCriteria, attr admission.Attributes, namespaceLabels NamespaceLabels) (bool, error) {
	namespaceName := attr.GetNamespace()
	if len(namespaceName) == 0 && attr.GetResource().Resource != "namespaces" {
		// If the request is about a cluster scoped resource, and it is not a
		// namespace, it is never exempted.
		return true, nil
	}
	selector, err := criteria.GetParsedNamespaceSelector()
	if err != nil {
		return false, err
	}
	if selector.Empty() {
		return true, nil
	}
	var labelsOfNamespace map[string]string
	if attr.GetResource().Resource == "namespaces" && len(attr.GetSubresource()) == 0 &&
		(attr.GetOperation() == admission.Create || attr.GetOperation() == admission.Update) {
		// A Namespace being created or updated is matched by its own labels.
		accessor, ok := attr.GetObject().(metav1.Object)
		if !ok {
			return false, fmt.Errorf("the Namespace has no metadata")
		}
		labelsOfNamespace = accessor.GetLabels()
	} else if labelsOfNamespace, err = namespaceLabels(namespaceName); err != nil {
		return false, err
	}
	return selector.Matches(labels.Set(labelsOfNamespace)), nil
}

func matchesResourceRules(namedRules []v1.NamedRuleWithOperations, matchPolicy *v1.MatchPolicyType, attr admission.Attributes, o admission.ObjectInterfaces) (bool, schema.GroupVersionResource, schema.GroupVersionKind, error) {
	matchKind := attr.GetKind()
	matchResource := attr.GetResource()

	for _, namedRule := range namedRules {
		rule := v1.RuleWithOperations(namedRule.RuleWithOperations)
		ruleMatcher := rules.Matcher{
			Rule: rule,
			Attr: attr,
		}
		if !ruleMatcher.Matches() {
			continue
		}
		// an empty name list always matches
		if len(namedRule.ResourceNames) == 0 {
			return true, matchResource, matchKind, nil
		}
		// TODO: GetName() can return an empty string if the user is relying on
		// the API server to generate the name... figure out what to do for this edge case
		name := attr.GetName()
		for _, matchedName := range namedRule.ResourceNames {
			if name == matchedName {
				return true, matchResource, matchKind, nil
			}
		}
	}

	// if match policy is undefined or exact, don't perform fuzzy matching
	// note that defaulting to fuzzy matching is set by the API
	if matchPolicy == nil || *matchPolicy == v1.Exact {
		return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, nil
	}

	attrWithOverride := &attrWithResourceOverride{Attributes: attr}
	equivalents := o.GetEquivalentResourceMapper().EquivalentResourcesFor(attr.GetResource(), attr.GetSubresource())
	for _, namedRule := range namedRules {
		for _, equivalent := range equivalents {
			if equivalent == attr.GetResource() {
				// we have already checked the original resource
				continue
			}
			attrWithOverride.resource = equivalent
			rule := v1.RuleWithOperations(namedRule.RuleWithOperations)
			m := rules.Matcher{
				Rule: rule,
				Attr: attrWithOverride,
			}
			if !m.Matches() {
				continue
			}
			matchKind = o.GetEquivalentResourceMapper().KindFor(equivalent, attr.GetSubresource())
			if matchKind.Empty() {
				return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, fmt.Errorf("unable to convert to %v: unknown kind", equivalent)
			}
			// an empty name list always matches
			if len(namedRule.ResourceNames) == 0 {
				return true, equivalent, matchKind, nil
			}

			// TODO: GetName() can return an empty string if the user is relying on
			// the API server to generate the name... figure out what to do for this edge case
			name := attr.GetName()
			for _, matchedName := range namedRule.ResourceNames {
				if name == matchedName {
					return true, equivalent, matchKind, nil
				}
			}
		}
	}
	return false, schema.GroupVersionResource{}, schema.GroupVersionKind{}, nil
}

type attrWithResourceOverride struct {
	admission.Attributes
	resource schema.GroupVersionResource
}

func (a *attrWithResourceOverride) GetResource() schema.GroupVersionResource { return a.resource }
