// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
)

// AdmissionPolicyFunction runs the ValidatingAdmissionPolicies of the Unit passed to it as
// OtherData PolicyOtherData.
const AdmissionPolicyFunction = "vet-validating-admission-policy"

// PolicyOtherData is the OtherData key AdmissionPolicyFunction reads its policies under, and
// the key of the Unit in a Trigger's OtherUnitIDs.
const PolicyOtherData = "policy"

// PolicyUnitAnnotation records on the Trigger of a rule installed with --policy-unit the Unit
// whose policies it runs, as <space>/<unit>.
const PolicyUnitAnnotation = "PolicyUnit"

const admissionPolicyType = "admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy"

// policyUnitRule is the rule running the ValidatingAdmissionPolicies of the Unit ref names, as
// <space>/<unit>: a user's own, already in ConfigHub and released to their clusters, so that the
// policy checked before release is the one enforced at admission. The rule is named after the
// Unit. Which resources each policy sees is up to the Unit's bindings, matchResources included,
// as at admission; a parameter a policy's paramRef names is looked for in the same Unit.
//
// policySpaceID, when the policy Space exists, is where the rule's Trigger goes. A Trigger there
// already named after the Unit is refused unless it runs this Unit's policies, since installing
// would overwrite it: another Unit's rule of the same name, or another pack's or tool's rule.
func (p Pack) policyUnitRule(ctx context.Context, client *cubapi.Client, policySpaceID *goclientnew.UUID, ref string) (Rule, error) {
	space, unit, ok := strings.Cut(ref, "/")
	if !ok || space == "" || unit == "" {
		return Rule{}, fmt.Errorf("--policy-unit %q: write <space>/<unit>", ref)
	}
	u, err := cubapi.ResolveUnit(ctx, client, cubapi.NewRef(space, unit), cubapi.ResolveOpts{Select: "UnitID,Slug,SpaceID,ToolchainType"})
	if err != nil {
		return Rule{}, fmt.Errorf("--policy-unit %s: %w", ref, err)
	}
	if u.Unit.ToolchainType != string(workerapi.ToolchainKubernetesYAML) {
		return Rule{}, fmt.Errorf("--policy-unit %s is %s, not Kubernetes/YAML", ref, u.Unit.ToolchainType)
	}
	policies, err := cubapi.ListResources(ctx, client,
		cubapi.Where{}.Eq("UnitID", u.Unit.UnitID.String()).Eq("ResourceType", admissionPolicyType),
		cubapi.ListOpts{Select: "ResourceName"})
	if err != nil {
		return Rule{}, fmt.Errorf("list the policies of %s: %w", ref, err)
	}
	if len(policies) == 0 {
		return Rule{}, fmt.Errorf("--policy-unit %s holds no ValidatingAdmissionPolicy", ref)
	}
	slug := u.Unit.Slug
	if slices.Contains(p.ruleSlugs(), slug) {
		return Rule{}, fmt.Errorf("--policy-unit %s would install a rule named %s, which the %s pack already has", ref, slug, p.Group)
	}
	if policySpaceID != nil {
		existing, err := cubapi.ResolveTrigger(ctx, client, cubapi.NewRef("", slug),
			cubapi.ResolveOpts{Space: *policySpaceID, Select: "TriggerID,Slug,Annotations,OtherUnitIDs"})
		switch {
		case err != nil && !cubapi.IsNotFoundError(err):
			return Rule{}, fmt.Errorf("read trigger %s: %w", slug, err)
		case err == nil && existing.Trigger != nil:
			t := existing.Trigger
			if t.Annotations[PolicyUnitAnnotation] == "" || t.OtherUnitIDs[PolicyOtherData] != u.Unit.UnitID {
				return Rule{}, fmt.Errorf("--policy-unit %s would install a rule named %s, but the policy Space already has a Trigger %s that does not run its policies", ref, slug, slug)
			}
		}
	}
	return Rule{
		Slug:         slug,
		DisplayName:  "Admission policies of " + slug,
		Description:  fmt.Sprintf("Fails a resource that a ValidatingAdmissionPolicy of %s denies, for the resources its bindings select, as the policy would at admission.", ref),
		Function:     AdmissionPolicyFunction,
		OtherUnitIDs: map[string]goclientnew.UUID{PolicyOtherData: u.Unit.UnitID},
		PolicyUnit:   space + "/" + slug,
	}, nil
}
