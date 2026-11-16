// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
)

// ParamsOtherData is the OtherData key AdmissionPolicyFunction also looks for a binding's
// paramRef under, after the policy Unit, and the key of the params Unit in a Trigger's
// OtherUnitIDs.
const ParamsOtherData = "params"

// The annotations of a library Unit, recording where its policies came from, since installing it
// distributes them into the user's organization.
const (
	SourceAnnotation  = "Source"
	LicenseAnnotation = "License"
)

const admissionBindingType = "ValidatingAdmissionPolicyBinding"

// PolicyLibrary is a set of ValidatingAdmissionPolicies a pack adopts from one source, which its
// EngineAdmissionPolicy checks run. Install writes it into the policy Space as two Units:
//
//   - the library Unit, guardrails-<pack>-<name>, holding Policies and a binding of each, which
//     install replaces with this version's on every install;
//   - the params Unit, guardrails-<name>-params, holding Params, which install creates once and
//     never overwrites, so that a team's edits to the parameters survive upgrades.
type PolicyLibrary struct {
	// Name names the source, and the library's Units: lowercase letters, digits and '-'.
	Name string
	// Source is where the policies came from, as repository@commit, and License their license.
	Source  string
	License string
	// Policies are the ValidatingAdmissionPolicies the pack adopts, as YAML documents.
	Policies string
	// Params are the parameter resources of the source with its defaults, as YAML documents,
	// and ParamRef the name of the one each policy taking parameters is bound to.
	Params   string
	ParamRef string
}

// LibraryUnit is the slug of the library Unit of the pack's library name.
func (p Pack) LibraryUnit(name string) string { return "guardrails-" + p.Group + "-" + name }

// ParamsUnit is the slug of the params Unit of library name, which every pack adopting policies
// from the source shares.
func ParamsUnit(name string) string { return "guardrails-" + name + "-params" }

// library is the pack's library name.
func (p Pack) library(name string) (PolicyLibrary, bool) {
	i := slices.IndexFunc(p.Libraries, func(l PolicyLibrary) bool { return l.Name == name })
	if i < 0 {
		return PolicyLibrary{}, false
	}
	return p.Libraries[i], true
}

// admissionPolicy is the part of a ValidatingAdmissionPolicy install reads.
type admissionPolicy struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		ParamKind *struct {
			Kind string `json:"kind"`
		} `json:"paramKind"`
	} `json:"spec"`
}

// policies are the library's ValidatingAdmissionPolicies.
func (l PolicyLibrary) policies() ([]admissionPolicy, error) {
	var out []admissionPolicy
	for _, doc := range yamlDocuments(l.Policies) {
		var policy admissionPolicy
		if err := yaml.Unmarshal([]byte(doc), &policy); err != nil {
			return nil, fmt.Errorf("library %s: %w", l.Name, err)
		}
		if policy.Kind != "ValidatingAdmissionPolicy" || policy.Metadata.Name == "" {
			return nil, fmt.Errorf("library %s: Policies holds a %s, not a named ValidatingAdmissionPolicy", l.Name, policy.Kind)
		}
		out = append(out, policy)
	}
	return out, nil
}

// validate checks the library, and that each policy taking parameters has some to be bound to.
func (l PolicyLibrary) validate() error {
	if !namePattern.MatchString(l.Name) {
		return fmt.Errorf("library %q: a library needs a Name of lowercase letters, digits and '-'", l.Name)
	}
	if l.Source == "" || l.License == "" {
		return fmt.Errorf("library %s: a library records its Source and License", l.Name)
	}
	policies, err := l.policies()
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return fmt.Errorf("library %s has no policies", l.Name)
	}
	for _, policy := range policies {
		if policy.Spec.ParamKind != nil && (l.Params == "" || l.ParamRef == "") {
			return fmt.Errorf("library %s: policy %s takes parameters, but the library has no Params and ParamRef", l.Name, policy.Metadata.Name)
		}
	}
	return nil
}

// unitData is the library Unit's configuration: its policies, and a binding of each that denies
// what the policy denies, with the library's parameters when the policy takes some. A binding
// selects no resources of its own: which resources a rule sees is the rule's WhereResource.
func (l PolicyLibrary) unitData() (string, error) {
	policies, err := l.policies()
	if err != nil {
		return "", err
	}
	docs := yamlDocuments(l.Policies)
	for _, policy := range policies {
		spec := map[string]any{"policyName": policy.Metadata.Name, "validationActions": []string{"Deny"}}
		if policy.Spec.ParamKind != nil {
			spec["paramRef"] = map[string]any{"name": l.ParamRef, "parameterNotFoundAction": "Deny"}
		}
		binding, err := yaml.Marshal(map[string]any{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       admissionBindingType,
			"metadata":   map[string]any{"name": policy.Metadata.Name},
			"spec":       spec,
		})
		if err != nil {
			return "", err
		}
		docs = append(docs, strings.TrimSpace(string(binding)))
	}
	return strings.Join(docs, "\n---\n") + "\n", nil
}

// yamlDocuments are the documents of a YAML stream, split at the lines starting "---", leaving
// out those holding only comments.
func yamlDocuments(stream string) []string {
	var docs, lines []string
	flush := func() {
		doc := strings.TrimSpace(strings.Join(lines, "\n"))
		if slices.ContainsFunc(strings.Split(doc, "\n"), func(line string) bool {
			line = strings.TrimSpace(line)
			return line != "" && !strings.HasPrefix(line, "#")
		}) {
			docs = append(docs, doc)
		}
		lines = nil
	}
	for _, line := range strings.Split(stream, "\n") {
		if strings.HasPrefix(line, "---") {
			flush()
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return docs
}

// ensureLibraries writes the library and params Units of the libraries rules run in the policy
// Space, and sets each rule's OtherUnitIDs to them.
func (p Pack) ensureLibraries(ctx context.Context, client *cubapi.Client, policySpaceID goclientnew.UUID, rules []Rule) error {
	units := map[string]map[string]goclientnew.UUID{}
	for i, r := range rules {
		if r.Library == "" {
			continue
		}
		if len(units) == 0 {
			if err := policySpaceSkipsRules(ctx, client, policySpaceID); err != nil {
				return err
			}
		}
		if units[r.Library] == nil {
			ids, err := p.ensureLibrary(ctx, client, policySpaceID, r.Library)
			if err != nil {
				return err
			}
			units[r.Library] = ids
		}
		rules[i].OtherUnitIDs = units[r.Library]
	}
	return nil
}

// policySpaceSkipsRules keeps the guardrail rules off the policy Space's own Units, the library
// and params Units: they hold policy, not configuration to check, and a Space selects its own
// Triggers unless told otherwise. A policy Space selecting just its own Triggers is narrowed to
// those in no group; one selecting Triggers some other way is left as it is.
func policySpaceSkipsRules(ctx context.Context, client *cubapi.Client, spaceID goclientnew.UUID) error {
	s, err := cubapi.ResolveSpace(ctx, client, cubapi.NewRef("", spaceID.String()), cubapi.ResolveOpts{Select: policySpaceSelect})
	if err != nil {
		return fmt.Errorf("read the policy space: %w", err)
	}
	if !selectsOwnTriggers(s.Space) {
		return nil
	}
	return patchSelection(ctx, client, Wiring{Space: s.Space.Slug, spaceID: spaceID},
		map[string]any{"WhereTrigger": ungroupedTriggers(spaceID)})
}

// policySpaceSelect are the fields of the policy Space selectsOwnTriggers reads.
const policySpaceSelect = "SpaceID,Slug,WhereTrigger,TriggerFilterID"

// selectsOwnTriggers reports whether s selects just its own Triggers, as a Space does unless told
// otherwise.
func selectsOwnTriggers(s *goclientnew.Space) bool {
	return s.TriggerFilterID == nil && (s.WhereTrigger == "" || s.WhereTrigger == fmt.Sprintf("SpaceID = '%s'", s.SpaceID))
}

// ungroupedTriggers selects the Triggers of the Space spaceID that are in no group.
func ungroupedTriggers(spaceID goclientnew.UUID) string {
	return fmt.Sprintf("SpaceID = '%s' AND Labels.%s IS NULL", spaceID, GroupLabel)
}

// ensureLibrary writes the Units of library name, and returns their IDs by OtherData key.
func (p Pack) ensureLibrary(ctx context.Context, client *cubapi.Client, policySpaceID goclientnew.UUID, name string) (map[string]goclientnew.UUID, error) {
	l, ok := p.library(name)
	if !ok {
		return nil, fmt.Errorf("the %s pack has no library %s", p.Group, name)
	}
	data, err := l.unitData()
	if err != nil {
		return nil, err
	}
	toolchain := string(workerapi.ToolchainKubernetesYAML)
	lib, err := cubapi.EnsureUnitData(ctx, client, goclientnew.Unit{
		SpaceID: policySpaceID, Slug: p.LibraryUnit(name), ToolchainType: toolchain,
		Labels:      map[string]string{GroupLabel: p.Group},
		Annotations: map[string]string{SourceAnnotation: l.Source, LicenseAnnotation: l.License},
	}, data, fmt.Sprintf("Install the %s policies of the %s guardrails from %s", name, p.Group, l.Source), true)
	if err != nil {
		return nil, fmt.Errorf("write library unit %s: %w", p.LibraryUnit(name), err)
	}
	ids := map[string]goclientnew.UUID{PolicyOtherData: lib.UnitID}
	if l.Params == "" {
		return ids, nil
	}
	params, err := cubapi.EnsureUnitData(ctx, client, goclientnew.Unit{
		SpaceID: policySpaceID, Slug: ParamsUnit(name), ToolchainType: toolchain,
		Annotations: map[string]string{SourceAnnotation: l.Source, LicenseAnnotation: l.License},
	}, l.Params, fmt.Sprintf("Install the %s guardrail parameters with their defaults from %s", name, l.Source), false)
	if err != nil {
		return nil, fmt.Errorf("write params unit %s: %w", ParamsUnit(name), err)
	}
	ids[ParamsOtherData] = params.UnitID
	return ids, nil
}
