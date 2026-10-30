// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package guardrails installs and inspects a tool's policy pack: a group of
// validating Triggers defined once in a policy Space and selected by the
// WhereTrigger of each Space, and Target, that opts into the group.
//
// Every manager tool ships a pack, and the mechanism is the same each time --
// create the Space, create the Triggers, add the pack's group to each in-scope
// Space's selection, and never clobber a Space that already selects Triggers
// its own way. What differs is only the rules. A tool supplies those; this
// supplies the rest.
package guardrails

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/sdk/managerkit"
)

// ValidatingFunction is the function every rule's expression is run by.
//
// vet-cel evaluates a CEL expression once per resource with the Kubernetes
// admission-policy libraries available (quantity, url, ip, cidr, regex,
// format). The generic vet-celexpr accepts the same expressions and returns the
// same verdicts, but knows nothing about Kubernetes.
const ValidatingFunction = "vet-cel"

// DefaultPolicySpace is where a pack installs unless --policy-space says
// otherwise: the Space every tool shares, so that one selection can name every
// group a Space has opted into.
const DefaultPolicySpace = managerkit.CommonSpace

// GroupLabel names the group a guardrail Trigger belongs to. A Space selects
// guardrails by listing groups, so installing a new group never adds rules to a
// Space that did not ask for them.
const GroupLabel = "Guardrails"

// AppliedToLabel marks the Triggers that belong on Targets rather than on
// Spaces: rules about what is released, which a Space would run on every
// Unit, released or not. A Trigger without it applies to Spaces.
const AppliedToLabel = "AppliedTo"

// AppliedToTarget is the AppliedToLabel value of a Target rule.
const AppliedToTarget = "Target"

// ExceptionAnnotationPrefix starts the annotation that excepts a resource from
// one rule. Each rule's Trigger skips the resources carrying
// ExceptionAnnotation(rule), and the annotation's value says why.
const ExceptionAnnotationPrefix = "exceptions.guardrails.confighub.com/"

// ExceptionAnnotation is the annotation that excepts a resource from rule.
func ExceptionAnnotation(rule string) string {
	return ExceptionAnnotationPrefix + rule
}

// exceptionWhereResource is the WhereResource of rule's Trigger: every resource
// but those excepted from it.
func exceptionWhereResource(rule string) string {
	return "metadata.annotations." + yamlkit.EscapeDotsInPathSegment(ExceptionAnnotation(rule)) + " IS NULL"
}

// Rule is one validating policy in a pack.
//
// Expression is CEL over `r`, the resource under test, run by vet-cel. It should
// pass for resources the rule does not govern, which is why these expressions
// conventionally open with a kind test: a rule that fails everything it does not
// apply to is not a rule, it is an outage.
//
// A rule that is not a CEL expression at all -- schema validation, say -- sets
// Function and Arguments instead.
type Rule struct {
	Slug        string
	Description string
	Expression  string

	// Function overrides the validating function. Empty means vet-cel with
	// Expression as its argument.
	Function string
	// Arguments are passed when Function is set. Ignored otherwise.
	Arguments []api.FunctionArgument

	// AppliedTo is AppliedToTarget for a rule that checks only released Units,
	// so it is selected by Targets rather than Spaces. Empty means Spaces.
	AppliedTo string
}

// invocation is the function and arguments this rule's Trigger runs.
func (r Rule) invocation() (string, []api.FunctionArgument) {
	if r.Function != "" {
		return r.Function, r.Arguments
	}
	return ValidatingFunction, []api.FunctionArgument{{ParameterName: "expression", Value: r.Expression}}
}

// labels are the rule's Trigger labels in group.
func (r Rule) labels(group string) map[string]string {
	labels := map[string]string{GroupLabel: group}
	if r.AppliedTo != "" {
		labels[AppliedToLabel] = r.AppliedTo
	}
	return labels
}

// Pack is a tool's group of guardrail rules.
type Pack struct {
	// Group is the GroupLabel value of every rule's Trigger, and the name a
	// Space's selection lists to opt into these rules.
	Group string
	// Rules are the policies. They install with Warn=true: advisory
	// ValidationWarnings, never blocking, until someone promotes one with
	// `cub trigger update <slug> --space <policy-space> --unwarn`.
	Rules []Rule
}

// hasTargetRules reports whether any rule belongs on Targets.
func (p Pack) hasTargetRules() bool {
	return slices.ContainsFunc(p.Rules, func(r Rule) bool { return r.AppliedTo == AppliedToTarget })
}

// Selection is the WhereTrigger this package writes on a Space or a Target: the
// guardrail groups it has opted into, from one policy Space.
//
// It names the policy Space so that a group label is never trusted on its own:
// anyone who can create a Trigger somewhere could otherwise label it into every
// Space's checks.
type Selection struct {
	PolicySpace string
	// Groups are sorted and distinct.
	Groups []string
	// Target selects the Target rules rather than the Space rules.
	Target bool
}

// Where renders the selection as a WhereTrigger.
func (s Selection) Where() (string, error) {
	w := cubapi.Where{}.Eq("Space.Slug", s.PolicySpace).InStrings("Labels."+GroupLabel, s.Groups)
	if s.Target {
		w = w.Eq("Labels."+AppliedToLabel, AppliedToTarget)
	} else {
		w = w.And("Labels." + AppliedToLabel + " != '" + AppliedToTarget + "'")
	}
	return w.String(), w.Err()
}

// with returns the selection with group added.
func (s Selection) with(group string) Selection {
	groups := append(slices.Clone(s.Groups), group)
	slices.Sort(groups)
	s.Groups = slices.Compact(groups)
	return s
}

var selectionPattern = regexp.MustCompile(`^Space\.Slug = '([^']*)' AND Labels\.` + GroupLabel +
	` IN \(([^)]*)\) AND Labels\.` + AppliedToLabel + ` (=|!=) '` + AppliedToTarget + `'$`)

// ParseSelection recognizes a WhereTrigger that Selection.Where wrote. Any
// other expression is someone's own, and reports false.
func ParseSelection(where string) (Selection, bool) {
	m := selectionPattern.FindStringSubmatch(where)
	if m == nil {
		return Selection{}, false
	}
	s := Selection{PolicySpace: m[1], Target: m[3] == "="}
	for _, lit := range strings.Split(m[2], ",") {
		lit = strings.TrimSpace(lit)
		if len(lit) < 2 || lit[0] != '\'' || lit[len(lit)-1] != '\'' || strings.Contains(lit[1:len(lit)-1], "'") {
			return Selection{}, false
		}
		s = s.with(lit[1 : len(lit)-1])
	}
	return s, true
}

// Scope narrows which Spaces and Targets an install wires.
type Scope struct {
	// WhereSpace is a filter over Spaces.
	WhereSpace string
	// WhereTarget is a filter over Targets, used only by a pack with Target
	// rules.
	WhereTarget string
}

// Wiring is a Space or Target whose selection an install sets.
type Wiring struct {
	Space        string `json:"space"`
	Target       string `json:"target,omitempty"`
	WhereTrigger string `json:"whereTrigger"`

	spaceID, targetID goclientnew.UUID
}

// Name is the Space, or the Target as space/target.
func (w Wiring) Name() string { return qualified(w.Space, w.Target) }

// Skip is an in-scope Space or Target that was deliberately left alone, and
// why.
type Skip struct {
	Space  string `json:"space"`
	Target string `json:"target,omitempty"`
	Reason string `json:"reason"`
}

// Name is the Space, or the Target as space/target.
func (s Skip) Name() string { return qualified(s.Space, s.Target) }

func qualified(space, target string) string {
	if target == "" {
		return space
	}
	return space + "/" + target
}

// Plan is what an install would do, and after a commit, what it did.
type Plan struct {
	PolicySpace       string   `json:"policySpace"`
	Group             string   `json:"group"`
	Triggers          []string `json:"triggers"`
	Committed         bool     `json:"committed"`
	PolicySpaceExists bool     `json:"policySpaceExists"`
	// Wire are the Spaces and Targets whose selection gains the group.
	Wire []Wiring `json:"wire"`
	// AlreadyWired already select the group. A commit re-lists their Triggers,
	// so that a rule the install adds runs there too.
	AlreadyWired []Wiring `json:"alreadyWired"`
	Skipped      []Skip   `json:"skipped"`
}

// BuildPlan works out which Spaces holding Kubernetes/YAML Units this pack
// would wire, and which it would leave alone. It writes nothing.
func (p Pack) BuildPlan(ctx context.Context, client *cubapi.Client, policySpace string, scope Scope) (Plan, error) {
	k8sSpaces, err := KubernetesSpaces(ctx, client)
	if err != nil {
		return Plan{}, fmt.Errorf("find Kubernetes/YAML spaces: %w", err)
	}
	candidates := make([]string, 0, len(k8sSpaces))
	for slug := range k8sSpaces {
		candidates = append(candidates, slug)
	}
	return p.PlanFor(ctx, client, policySpace, candidates, scope)
}

// PlanFor is BuildPlan over a candidate set of Spaces the caller chose, for a
// tool whose config lives in a recognizable subset of Spaces rather than in
// every Space holding Kubernetes config. Wiring the rest would attach rules
// that have nothing to say there.
//
// A Space or Target that already selects Triggers some other way is reported
// rather than modified: rewriting its selection would silently drop whatever
// policy it already has, and there is no way to tell from here whether that
// was deliberate.
func (p Pack) PlanFor(ctx context.Context, client *cubapi.Client, policySpace string, candidates []string, scope Scope) (Plan, error) {
	plan := Plan{PolicySpace: policySpace, Group: p.Group}
	for _, r := range p.Rules {
		plan.Triggers = append(plan.Triggers, r.Slug)
	}
	if _, err := cubapi.ResolveSpace(ctx, client, cubapi.ParseRef(policySpace), cubapi.ResolveOpts{}); err == nil {
		plan.PolicySpaceExists = true
	}

	want := make(map[string]bool, len(candidates))
	for _, slug := range candidates {
		want[slug] = true
	}
	spaces, err := listSpaces(ctx, client, scope.WhereSpace)
	if err != nil {
		return plan, fmt.Errorf("list spaces: %w", err)
	}
	for _, s := range spaces {
		if s.Slug == policySpace || !want[s.Slug] {
			continue
		}
		ownSelection := s.WhereTrigger == "" || s.WhereTrigger == fmt.Sprintf("SpaceID = '%s'", s.SpaceID)
		own := 0
		if ownSelection && s.TriggerFilterID == "" {
			if own, err = cubapi.CountTriggers(ctx, client, s.SpaceID); err != nil {
				return plan, err
			}
		}
		w := Wiring{Space: s.Slug, spaceID: s.SpaceID}
		if err := p.decide(&plan, w, policySpace, s.WhereTrigger, s.TriggerFilterID, ownSelection, own, false); err != nil {
			return plan, err
		}
	}

	if p.hasTargetRules() {
		targets, err := listTargets(ctx, client, scope.WhereTarget)
		if err != nil {
			return plan, fmt.Errorf("list targets: %w", err)
		}
		for _, t := range targets {
			if t.Space == policySpace {
				continue
			}
			w := Wiring{Space: t.Space, Target: t.Slug, spaceID: t.SpaceID, targetID: t.TargetID}
			if err := p.decide(&plan, w, policySpace, t.WhereTrigger, t.TriggerFilterID, t.WhereTrigger == "", 0, true); err != nil {
				return plan, err
			}
		}
	}

	byName := func(a, b Wiring) int { return strings.Compare(a.Name(), b.Name()) }
	slices.SortFunc(plan.Wire, byName)
	slices.SortFunc(plan.AlreadyWired, byName)
	sort.Slice(plan.Skipped, func(i, j int) bool { return plan.Skipped[i].Name() < plan.Skipped[j].Name() })
	return plan, nil
}

// decide files one Space or Target into the plan. ownSelection reports that
// where selects only the entity's own Triggers, as a new Space does, and own is
// how many of those there are.
func (p Pack) decide(plan *Plan, w Wiring, policySpace, where, triggerFilterID string, ownSelection bool, own int, target bool) error {
	skip := func(reason string) {
		plan.Skipped = append(plan.Skipped, Skip{Space: w.Space, Target: w.Target, Reason: reason})
	}
	var sel Selection
	switch parsed, ok := ParseSelection(where); {
	case triggerFilterID != "":
		skip("selects Triggers with a Filter (TriggerFilterID); guardrails are selected by WhereTrigger alone, so clear it first")
		return nil
	case ok && parsed.PolicySpace != policySpace:
		skip("selects guardrails from policy Space " + parsed.PolicySpace)
		return nil
	case ok && parsed.Target != target:
		skip("custom WhereTrigger — add Labels." + GroupLabel + " '" + p.Group + "' to it by hand")
		return nil
	case ok && slices.Contains(parsed.Groups, p.Group):
		w.WhereTrigger = where
		plan.AlreadyWired = append(plan.AlreadyWired, w)
		return nil
	case ok:
		sel = parsed.with(p.Group)
	case ownSelection && own > 0:
		skip(fmt.Sprintf("has %d Trigger(s) of its own, which a guardrail selection would stop running", own))
		return nil
	case ownSelection:
		sel = Selection{PolicySpace: policySpace, Target: target}.with(p.Group)
	default:
		skip("custom WhereTrigger — add Labels." + GroupLabel + " '" + p.Group + "' to it by hand")
		return nil
	}
	expr, err := sel.Where()
	if err != nil {
		return err
	}
	w.WhereTrigger = expr
	plan.Wire = append(plan.Wire, w)
	return nil
}

// Execute applies a plan: the policy Space, the Triggers, and the selection on
// each Space and Target the plan chose to wire. It is idempotent -- every
// entity is ensured rather than created -- so re-running after adding a rule
// installs the new Trigger and leaves the rest alone. It re-lists the Triggers
// of the Spaces and Targets already wired, which would not otherwise run a
// rule added since.
//
// progress, when non-nil, is called with a line per step for a CLI to print.
func (p Pack) Execute(ctx context.Context, client *cubapi.Client, policySpace string, plan Plan, progress func(string)) error {
	say := func(s string) {
		if progress != nil {
			progress(s)
		}
	}

	// Shared, so the labels name no single tool.
	ps, err := cubapi.EnsureSpace(ctx, client, goclientnew.Space{
		Slug:   policySpace,
		Labels: map[string]string{"role": "policy"},
	})
	if err != nil {
		return fmt.Errorf("create policy space %s: %w", policySpace, err)
	}
	for _, r := range p.Rules {
		fn, args := r.invocation()
		if _, err := cubapi.EnsureTrigger(ctx, client, goclientnew.Trigger{
			SpaceID:       ps.SpaceID,
			Slug:          r.Slug,
			Description:   r.Description,
			Event:         "Mutation",
			ToolchainType: "Kubernetes/YAML",
			FunctionName:  fn,
			Arguments:     cubapi.Arguments(args),
			WhereResource: exceptionWhereResource(r.Slug),
			Warn:          true,
			Labels:        r.labels(p.Group),
		}); err != nil {
			return fmt.Errorf("create trigger %s: %w", r.Slug, err)
		}
	}
	say(fmt.Sprintf("Guardrail group %q ready in %s.", p.Group, policySpace))

	for _, w := range plan.Wire {
		if err := patchSelection(ctx, client, w, map[string]any{"WhereTrigger": w.WhereTrigger}); err != nil {
			return fmt.Errorf("wire %s: %w", w.Name(), err)
		}
		say("  wired " + w.Name())
	}
	for _, w := range plan.AlreadyWired {
		if err := patchSelection(ctx, client, w, map[string]any{}); err != nil {
			return fmt.Errorf("refresh %s: %w", w.Name(), err)
		}
	}
	if len(plan.AlreadyWired) > 0 {
		say(fmt.Sprintf("  re-listed the Triggers of %d already wired", len(plan.AlreadyWired)))
	}
	return nil
}

// patchSelection merge-patches a Space or Target with fields and re-lists its
// Triggers.
func patchSelection(ctx context.Context, client *cubapi.Client, w Wiring, fields map[string]any) error {
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	refresh := true
	if w.Target == "" {
		res, err := client.API.PatchSpaceWithBodyWithResponse(ctx, w.spaceID,
			&goclientnew.PatchSpaceParams{RefreshTriggers: &refresh}, mergePatchContentType, bytes.NewReader(body))
		if cubapi.IsAPIError(err, res) {
			return cubapi.InterpretErrorGeneric(err, res)
		}
		return nil
	}
	res, err := client.API.PatchTargetWithBodyWithResponse(ctx, w.spaceID, w.targetID,
		&goclientnew.PatchTargetParams{RefreshTriggers: &refresh}, mergePatchContentType, bytes.NewReader(body))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

const mergePatchContentType = "application/merge-patch+json"
