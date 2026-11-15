// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// Separating a check into a rule of its own, to stage it ahead of the rest of its group: to
// enforce one check fleet-wide while the others are still audited, or to except resources
// from it alone.

import (
	"context"
	"fmt"
	"slices"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Separation is checks moved out of a rule into rules of their own, each named by its check.
type Separation struct {
	PolicySpace string   `json:"policySpace"`
	Rule        string   `json:"rule"`
	Checks      []string `json:"checks"`
	Committed   bool     `json:"committed"`
	// Reevaluated is how many Units the rules were run on again: those the rule failed, whose
	// findings of the separated checks move to the new rules.
	Reevaluated int `json:"reevaluated,omitempty"`

	pack  Pack
	rules []Rule // the rule, and a rule per separated check
	group *goclientnew.Trigger
}

// PlanSeparation works out separating checks from rule, a rule of one of packs installed in
// policySpace. Each check has to run in the rule now, and one has to be left in it. It writes
// nothing.
func PlanSeparation(ctx context.Context, client *cubapi.Client, policySpace string, packs []Pack, rule string, checks []string) (Separation, error) {
	sep := Separation{PolicySpace: policySpace, Rule: rule, Checks: checks}
	if len(checks) == 0 {
		return sep, fmt.Errorf("name a check to separate")
	}
	packIndex := slices.IndexFunc(packs, func(p Pack) bool { _, ok := p.check(checks[0]); return ok })
	if packIndex < 0 {
		return sep, fmt.Errorf("no pack has a check %q", checks[0])
	}
	p := packs[packIndex]
	sep.pack = p

	triggers, err := p.installedTriggers(ctx, client, policySpace)
	if err != nil {
		return sep, err
	}
	installed := installedRules(triggers)
	at := slices.IndexFunc(installed, func(r installedRule) bool { return r.Slug == rule })
	if at < 0 {
		return sep, fmt.Errorf("rule %s of the %s pack is not installed in %s", rule, p.Group, policySpace)
	}
	group := installed[at]
	sep.group = group.Trigger

	for _, name := range checks {
		c, ok := p.check(name)
		switch {
		case !ok:
			return sep, fmt.Errorf("the %s pack has no check %q", p.Group, name)
		case !slices.Contains(group.Controls, c.Control):
			return sep, fmt.Errorf("check %s does not run in rule %s", name, rule)
		case name == rule:
			return sep, fmt.Errorf("check %s is already a rule of its own", name)
		case slices.ContainsFunc(installed, func(r installedRule) bool { return r.Slug == name }):
			return sep, fmt.Errorf("a rule is already named %s", name)
		}
		group.Controls = slices.DeleteFunc(group.Controls, func(control string) bool { return control == c.Control })
		installed = append(installed, installedRule{Slug: name, Controls: []string{c.Control}, Params: group.Params})
	}
	if len(group.Controls) == 0 {
		return sep, fmt.Errorf("separating every check of rule %s would leave it with none: it is already its checks' rule", rule)
	}
	installed[at] = group

	rules, _, err := p.layout(installed, CheckSelection{})
	if err != nil {
		return sep, err
	}
	for _, r := range rules {
		if r.Slug == rule || slices.Contains(checks, r.Slug) {
			sep.rules = append(sep.rules, r)
		}
	}
	return sep, nil
}

// ExecuteSeparation applies a separation: it rewrites the rule without the checks, and writes a
// rule for each, at the rule's stage, priority and enforcement dates. The Spaces and Targets
// selecting the pack re-list their Triggers, and the rules run again on the Units the rule
// failed, so that its findings of the separated checks move to their new rules.
func ExecuteSeparation(ctx context.Context, client *cubapi.Client, sep *Separation) error {
	var ids []goclientnew.UUID
	for _, r := range sep.rules {
		like := sep.group
		if r.Slug == sep.Rule {
			like = nil
		}
		t, err := sep.pack.ensureRule(ctx, client, sep.group.SpaceID, r, like, nil)
		if err != nil {
			return err
		}
		ids = append(ids, t.TriggerID)
	}
	plan, err := sep.pack.BuildPlan(ctx, client, sep.PolicySpace, Scope{})
	if err != nil {
		return err
	}
	for _, w := range plan.AlreadyWired {
		if err := patchSelection(ctx, client, w, map[string]any{}); err != nil {
			return fmt.Errorf("refresh %s: %w", w.Name(), err)
		}
	}
	sep.Committed = true
	if sep.Reevaluated, err = reevaluateFailing(ctx, client, sep.group.TriggerID, ids, sep.group.ToolchainType); err != nil {
		return fmt.Errorf("re-evaluate %s: %w", sep.Rule, err)
	}
	return nil
}
