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
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
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

// GroupLabel names the group a guardrail Trigger belongs to: the Pack label,
// which every pack of Triggers selected from a policy Space shares. A Space
// selects guardrails by listing groups, so installing a new group never adds
// rules to a Space that did not ask for them.
const GroupLabel = constants.PackLabel

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

// exceptionWhereResource is the WhereResource clause skipping the resources
// excepted from rule.
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
	Slug string
	// DisplayName is the rule's Trigger's name where a person reads it, such as on
	// the Validation page: what the rule checks, in a few words. Empty leaves the
	// Trigger's as it is.
	DisplayName string
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

	// ResourceTypes are the types the rule governs, which its WhereResource
	// selects, so it is not run on a Unit holding none of them. Empty governs
	// every resource.
	ResourceTypes []string
	// Controls are the control numbers of the checks the rule runs, recorded on
	// its Trigger.
	Controls []string
	// Priority is the rule's priority when its Trigger is created: the highest of
	// its checks'. A Trigger's priority is not changed by installing again.
	Priority string
	// ExceptFrom are other rules whose exceptions this one honors as well as its
	// own: those its checks name, and those of the rule a check was separated
	// from.
	ExceptFrom []string
	// OtherUnitIDs are Units passed to Function as OtherData, by key.
	OtherUnitIDs map[string]goclientnew.UUID
	// OtherData is what the rule reads besides its resources, selected for each Space that
	// selects it (OtherData). A rule applied to Targets cannot read any.
	OtherData []OtherData
	// PolicyUnit is the Unit, <space>/<unit>, of the policies a rule installed with
	// --policy-unit runs, recorded on its Trigger.
	PolicyUnit string
	// Library is the pack's PolicyLibrary whose policies the rule runs. Install writes its
	// Units into the policy Space and passes them as OtherUnitIDs.
	Library string
}

// whereResource is the rule's Trigger's WhereResource: the resources of its
// types, but those excepted from it.
func (r Rule) whereResource() string {
	clauses := []string{}
	if types := resourceTypesWhere(r.ResourceTypes); types != "" {
		clauses = append(clauses, types)
	}
	clauses = append(clauses, exceptionWhereResource(r.Slug))
	for _, rule := range r.ExceptFrom {
		clauses = append(clauses, exceptionWhereResource(rule))
	}
	return strings.Join(clauses, " AND ")
}

// annotations are the annotations the rule's Trigger is written with, beyond
// the plan's: its controls, and its priority when the Trigger is new.
func (r Rule) annotations(isNew bool) map[string]string {
	annotations := map[string]string{}
	if len(r.Controls) > 0 {
		annotations[ControlsAnnotation] = strings.Join(r.Controls, ",")
	}
	if isNew && r.Priority != "" {
		annotations[constants.TriggerPriorityAnnotation] = r.Priority
	}
	if r.PolicyUnit != "" {
		annotations[PolicyUnitAnnotation] = r.PolicyUnit
	}
	return annotations
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
	// Rules are policies declared as rules. They install with Warn=true:
	// advisory ValidationWarnings, never blocking, until someone enforces one
	// with `guardrails update <rule> --enforce`, which installing again leaves in
	// place.
	Rules []Rule
	// Checks are the pack's checks, which install groups into rules by the Rule
	// each names (catalog.go). They install as Rules do.
	Checks []Check
	// RuleInfo describes the rules that run several checks.
	RuleInfo []RuleInfo
	// Libraries are the admission policies the pack's EngineAdmissionPolicy checks run.
	Libraries []PolicyLibrary
}

// hasTargetRules reports whether any rule belongs on Targets.
func (p Pack) hasTargetRules() bool {
	return slices.ContainsFunc(p.Rules, func(r Rule) bool { return r.AppliedTo == AppliedToTarget }) ||
		p.hasTargetChecks()
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
	// selectsFrom names the other Space whose Triggers the Space selects, such as its base;
	// empty when it selects its own or none.
	selectsFrom string
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
	// Priority and EnforcedBy, when set, are written to every rule's Trigger as
	// its enforcement annotations; empty leaves what a Trigger has.
	Priority   string `json:"priority,omitempty"`
	EnforcedBy string `json:"enforcedBy,omitempty"`
	// SkippedChecks are checks the install leaves out, such as one missing a
	// required parameter.
	SkippedChecks []CheckSkip `json:"skippedChecks,omitempty"`
	// PolicyUnits are the Units of admission policies the install writes in the policy Space:
	// each library Unit, replaced with this version's, and each params Unit, created once.
	PolicyUnits []string `json:"policyUnits,omitempty"`
	// NarrowPolicySpace says the install narrows the policy Space's selection from its own
	// Triggers to those in no group, so that the rules do not check the PolicyUnits.
	NarrowPolicySpace bool `json:"narrowPolicySpace,omitempty"`

	// rules are the rules the install writes.
	rules []Rule
}

// enforcementAnnotations are the annotations the plan writes on each rule's
// Trigger, nil when none.
func (p Plan) enforcementAnnotations() map[string]string {
	annotations := map[string]string{}
	if p.Priority != "" {
		annotations[constants.TriggerPriorityAnnotation] = p.Priority
	}
	if p.EnforcedBy != "" {
		annotations[constants.TriggerEnforcedByAnnotation] = p.EnforcedBy
	}
	if len(annotations) == 0 {
		return nil
	}
	return annotations
}

// BuildPlan works out which Spaces holding Kubernetes/YAML Units this pack
// would wire, and which it would leave alone, and the rules it would write,
// with the optional checks and parameters in sel. It writes nothing.
func (p Pack) BuildPlan(ctx context.Context, client *cubapi.Client, policySpace string, scope Scope, sel ...CheckSelection) (Plan, error) {
	k8sSpaces, err := KubernetesSpaces(ctx, client)
	if err != nil {
		return Plan{}, fmt.Errorf("find Kubernetes/YAML spaces: %w", err)
	}
	candidates := make([]string, 0, len(k8sSpaces))
	for slug := range k8sSpaces {
		candidates = append(candidates, slug)
	}
	return p.PlanFor(ctx, client, policySpace, candidates, scope, sel...)
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
func (p Pack) PlanFor(ctx context.Context, client *cubapi.Client, policySpace string, candidates []string, scope Scope, sel ...CheckSelection) (Plan, error) {
	plan := Plan{PolicySpace: policySpace, Group: p.Group}
	var policySpaceID *goclientnew.UUID
	// A new policy Space selects its own Triggers.
	ownSelection := true
	if ps, err := cubapi.ResolveSpace(ctx, client, cubapi.ParseRef(policySpace), cubapi.ResolveOpts{Select: policySpaceSelect}); err == nil {
		plan.PolicySpaceExists = true
		policySpaceID = &ps.Space.SpaceID
		ownSelection = selectsOwnTriggers(ps.Space)
	}
	var installed []installedRule
	if plan.PolicySpaceExists && len(p.Checks) > 0 {
		triggers, err := p.installedTriggers(ctx, client, policySpace)
		if err != nil {
			return plan, err
		}
		installed = installedRules(triggers)
	}
	var selection CheckSelection
	if len(sel) > 0 {
		selection = sel[0]
	}
	rules, skipped, err := p.layout(installed, selection)
	if err != nil {
		return plan, err
	}
	for _, ref := range selection.PolicyUnits {
		rule, err := p.policyUnitRule(ctx, client, policySpaceID, ref)
		if err != nil {
			return plan, err
		}
		if slices.ContainsFunc(rules, func(r Rule) bool { return r.Slug == rule.Slug }) {
			return plan, fmt.Errorf("--policy-unit %s: two policy Units are named %s", ref, rule.Slug)
		}
		rules = append(rules, rule)
	}
	plan.rules, plan.SkippedChecks = rules, skipped
	for _, r := range rules {
		plan.Triggers = append(plan.Triggers, r.Slug)
		if r.Library == "" || slices.Contains(plan.PolicyUnits, p.LibraryUnit(r.Library)) {
			continue
		}
		plan.PolicyUnits = append(plan.PolicyUnits, p.LibraryUnit(r.Library))
		if l, _ := p.library(r.Library); l.Params != "" {
			plan.PolicyUnits = append(plan.PolicyUnits, ParamsUnit(r.Library))
		}
		plan.NarrowPolicySpace = ownSelection
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
		// A variant made from a Space that selects its own Triggers selects the Triggers in that
		// Space, as cub variant create writes it, and so do the variants made from it, which copy
		// its selection. With none there, it selects nothing, as a new Space does; with some,
		// wiring it would stop running them, as for a Space's own.
		holder, selectsOther := selectedSpace(s.WhereTrigger)
		selectsOther = selectsOther && holder != s.SpaceID
		ownSelection := s.WhereTrigger == "" || s.WhereTrigger == fmt.Sprintf("SpaceID = '%s'", s.SpaceID) || selectsOther
		if !selectsOther {
			holder = s.SpaceID
		}
		own := 0
		if ownSelection && s.TriggerFilterID == "" {
			if own, err = cubapi.CountTriggers(ctx, client, holder); err != nil {
				return plan, err
			}
		}
		w := Wiring{Space: s.Slug, spaceID: s.SpaceID}
		if selectsOther && own > 0 {
			// The Space by its slug, or its ID when the caller cannot read it.
			name := holder.String()
			if selected, err := cubapi.ResolveSpace(ctx, client, cubapi.ParseRef(name), cubapi.ResolveOpts{Select: "Slug,SpaceID"}); err == nil {
				name = selected.Space.Slug
			}
			w.selectsFrom = "the Space " + name
			if holder == s.UpstreamSpaceID {
				w.selectsFrom = "its upstream Space " + name
			}
		}
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

// selectedSpacePattern is a WhereTrigger selecting the Triggers in one Space.
var selectedSpacePattern = regexp.MustCompile(`^SpaceID = '([0-9a-fA-F-]{36})'$`)

// selectedSpace is the Space whose Triggers where selects, when it selects those of one Space.
func selectedSpace(where string) (goclientnew.UUID, bool) {
	match := selectedSpacePattern.FindStringSubmatch(where)
	if match == nil {
		return goclientnew.UUID{}, false
	}
	id, err := uuid.Parse(match[1])
	return goclientnew.UUID(id), err == nil
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
	case ownSelection && own > 0 && w.selectsFrom != "":
		skip(fmt.Sprintf("selects the %d Trigger(s) in %s, which a guardrail selection would stop running", own, w.selectsFrom))
		return nil
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
	rules := plan.rules
	if rules == nil {
		// A plan not built by PlanFor installs the declared rules and the default checks.
		if rules, _, err = p.layout(nil, CheckSelection{}); err != nil {
			return err
		}
	}
	rules = slices.Clone(rules)
	if err := p.ensureLibraries(ctx, client, ps.SpaceID, rules); err != nil {
		return err
	}
	for _, r := range rules {
		if _, err := p.ensureRule(ctx, client, ps.SpaceID, r, nil, plan.enforcementAnnotations()); err != nil {
			return err
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

// ensureRule writes rule r's Trigger in the policy Space, and returns it. A
// rule installs as a warning, but one already promoted with --unwarn stays
// blocking: re-installing to update the rules must not quietly undo that
// decision. Only a rule with no Trigger yet is new; on any other error,
// guessing would risk the very demotion this avoids.
//
// like, when not nil, is a Trigger a new one takes its stage and enforcement
// annotations from: the rule a check is separated from. annotations are merged
// into what the Trigger has, so a rerun without them keeps them.
func (p Pack) ensureRule(ctx context.Context, client *cubapi.Client, policySpaceID goclientnew.UUID, r Rule,
	like *goclientnew.Trigger, annotations map[string]string) (*goclientnew.Trigger, error) {
	fn, args := r.invocation()
	warn, isNew := true, true
	existing, err := cubapi.ResolveTrigger(ctx, client, cubapi.NewRef("", r.Slug),
		cubapi.ResolveOpts{Space: policySpaceID, Select: "TriggerID,Slug,Warn"})
	switch {
	case err == nil && existing.Trigger != nil:
		warn, isNew = existing.Trigger.Warn, false
	case err != nil && !cubapi.IsNotFoundError(err):
		return nil, fmt.Errorf("read trigger %s: %w", r.Slug, err)
	}
	var inherited map[string]string
	if isNew && like != nil {
		warn = like.Warn
		inherited = map[string]string{}
		for _, key := range []string{constants.TriggerPriorityAnnotation, constants.TriggerEnforcedByAnnotation, constants.TriggerEnforcedAtAnnotation} {
			if value, ok := like.Annotations[key]; ok {
				inherited[key] = value
			}
		}
	}
	t, err := cubapi.EnsureTrigger(ctx, client, goclientnew.Trigger{
		SpaceID:             policySpaceID,
		Slug:                r.Slug,
		DisplayName:         r.DisplayName,
		Description:         r.Description,
		Event:               "Mutation",
		ToolchainType:       "Kubernetes/YAML",
		FunctionName:        fn,
		Arguments:           cubapi.Arguments(args),
		OtherUnitIDs:        r.OtherUnitIDs,
		OtherUnitSelections: r.otherUnitSelections(),
		WhereResource:       r.whereResource(),
		Warn:                warn,
		Labels:              r.labels(p.Group),
		Annotations:         mergeAnnotations(r.annotations(isNew), inherited, annotations),
	})
	if err != nil {
		return nil, fmt.Errorf("create trigger %s: %w", r.Slug, err)
	}
	return t, nil
}

// mergeAnnotations is the union of the annotation maps, later ones winning; nil
// when empty, so that nothing is sent.
func mergeAnnotations(all ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range all {
		maps.Copy(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// installedTriggers are the Triggers of the pack's rules in the policy Space.
func (p Pack) installedTriggers(ctx context.Context, client *cubapi.Client, policySpace string) ([]*goclientnew.Trigger, error) {
	extended, err := cubapi.ListTriggers(ctx, client,
		cubapi.Where{}.Eq("Space.Slug", policySpace).Eq("Labels."+GroupLabel, p.Group),
		cubapi.ListOpts{Select: "TriggerID,SpaceID,Slug,ToolchainType,Labels,Annotations,Arguments,Warn,Disabled"})
	if err != nil {
		return nil, fmt.Errorf("list the %s rules in %s: %w", p.Group, policySpace, err)
	}
	triggers := make([]*goclientnew.Trigger, 0, len(extended))
	for _, et := range extended {
		if et.Trigger != nil {
			triggers = append(triggers, et.Trigger)
		}
	}
	return triggers, nil
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
