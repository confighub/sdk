// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// The check catalog: a pack declares checks, and install groups them into rules.
//
// A check is one thing a pack verifies, with a control number of ours that never changes. A rule
// is a validating Trigger, and runs one or more checks; it is what is staged, from audit to
// enforced. Checks that a Trigger can run together -- the same engine, the same resource types,
// the same AppliedTo -- name the same rule, and each finding names the check it fails by its
// Issue Identifier: the check's Name, or, for a check of a function, its FunctionCheck. A check can be separated into a rule of its own, to stage it ahead of the
// rest of its group, and stays there when the pack is installed again.

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The engines a check can run on. Each is a validating function that reports a finding per
// check, named by the check, so a rule can run several checks of one engine.
const (
	// EngineCEL is vet-cel. The check's Failures is a CEL list of failure maps.
	EngineCEL = "cel"
	// EnginePodSecurity is vet-pod-security-standard, at the check's Level, running the
	// Pod Security Standards check named by the check's PodSecurityCheck. It is shorthand for
	// an EngineFunction check of that function, whose FunctionCheck is the PodSecurityCheck.
	EnginePodSecurity = "pss"
	// EngineFunction is any other validating function, with the check's Function and
	// Arguments. A check that sets FunctionCheck is one of several the function runs, chosen by
	// its check argument, as vet-correctness's are, and shares a rule with the checks naming the
	// same rule, function and arguments; their FunctionChecks are joined into the rule's check
	// argument. A check that does not is a rule of its own.
	EngineFunction = "fn"
	// EngineAdmissionPolicy is AdmissionPolicyFunction, running the ValidatingAdmissionPolicy
	// named by the check's Policy from the pack's library named by its Library. The checks of
	// one library naming one rule share it; their policies are joined into its policy argument.
	EngineAdmissionPolicy = "vap"
)

// PodSecurityFunction runs the EnginePodSecurity checks.
const PodSecurityFunction = "vet-pod-security-standard"

// ControlsAnnotation records on a rule's Trigger the control numbers of the checks it runs,
// comma-separated, so a Trigger read from the server says what it covers, and installing again
// knows which checks it holds.
const ControlsAnnotation = constants.TriggerControlsAnnotation

// The priorities a check can have by default, from most to least urgent.
const (
	PriorityHigh   = "High"
	PriorityMedium = "Medium"
	PriorityLow    = "Low"
)

// Param is a parameter a check takes. A cel check reads it from vet-cel's params as
// params['<check>.<name>'].
type Param struct {
	Name        string
	Description string
	// Required parameters have no default: the check is not installed until it is given one.
	Required bool
	Default  string
}

// Check is one thing a pack verifies.
type Check struct {
	// Control is the check's control number, CH-NNNN. It never changes or is reused.
	Control string
	// Name is the check's name, unique across packs. A cel check's findings carry it as their
	// Issue Identifier; a function check's carry whatever the function names them, which for
	// a check with a FunctionCheck is the FunctionCheck.
	Name string
	// DisplayName and Description describe the check, and the rule when it is a rule of its
	// own.
	DisplayName string
	Description string
	// Rule is the slug of the rule the check runs in, unless it has been separated into one of
	// its own. The checks naming one rule have to be able to run together.
	Rule string
	// Engine is how the check runs: EngineCEL, EnginePodSecurity or EngineFunction.
	Engine string
	// Priority is the check's default priority: PriorityHigh, PriorityMedium or PriorityLow.
	// A rule's is the highest of its checks', until someone changes it.
	Priority string
	// Optional checks are installed only when asked for by name, or with all of them.
	Optional bool
	Params   []Param
	// Fix says what fixes a finding: a command, a profile or a function.
	Fix string
	// Corresponds names the upstream controls the check corresponds to, prefixed by their
	// source: "PSS:privileged", "KS:C-0057", "CIS:5.2.2".
	Corresponds []string

	// ResourceTypes are the types the check governs, [Group/]Version/Kind, which its rule's
	// WhereResource selects. Empty governs every resource.
	ResourceTypes []string
	// AppliedTo is AppliedToTarget for a check of released Units only.
	AppliedTo string
	// ExceptFrom are other rules whose exceptions except a resource from this check too, since
	// a resource excepted from them cannot pass it: a workload excepted from running as non-root
	// cannot meet the restricted Pod Security Standard. A rule honors the exceptions of each,
	// so the checks naming one rule name the same ones.
	ExceptFrom []string

	// Failures, for EngineCEL, is a CEL list of failure maps, empty when the resource r passes.
	// Each map has ResourceName, ResourceType and Path, the optional Value, and Detail, a
	// sentence saying why.
	Failures string
	// Level and PodSecurityCheck, for EnginePodSecurity, are the Pod Security Standards level
	// and the ID of the check, as k8s.io/pod-security-admission names it.
	Level            string
	PodSecurityCheck string
	// Function and Arguments, for EngineFunction, are the function the check's rule runs.
	Function  string
	Arguments []api.FunctionArgument
	// FunctionCheck, for EngineFunction, is the name the function's check argument knows the
	// check by, for a function that runs several checks and names each finding by its check.
	FunctionCheck string
	// Library and Policy, for EngineAdmissionPolicy, are the pack's PolicyLibrary and the name
	// of the ValidatingAdmissionPolicy in it, which is the Identifier of the check's findings.
	Library string
	Policy  string
}

// asFunction is c as an EngineFunction check: a pss check is a check of
// vet-pod-security-standard at its level.
func (c Check) asFunction() Check {
	if c.Engine != EnginePodSecurity {
		return c
	}
	c.Engine = EngineFunction
	c.Function = PodSecurityFunction
	c.Arguments = []api.FunctionArgument{{ParameterName: "level", Value: c.Level}}
	c.FunctionCheck = c.PodSecurityCheck
	return c
}

// RuleInfo describes a rule that runs several checks, where a person reads it.
type RuleInfo struct {
	Slug        string
	DisplayName string
	Description string
}

// CheckSelection says which checks an install adds beyond a pack's defaults and those already
// installed, and the parameters it gives them.
type CheckSelection struct {
	// Checks are optional checks to install, by name.
	Checks []string
	// All installs every optional check.
	All bool
	// Params are parameter values, keyed <check>.<name>.
	Params map[string]string
	// PolicyUnits are Units of the caller's own ValidatingAdmissionPolicies, as <space>/<unit>,
	// to install a rule running each.
	PolicyUnits []string
}

// CheckSkip is a check an install leaves out, and why.
type CheckSkip struct {
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

// installedRule is what a rule's Trigger in the policy Space says about the checks it runs.
type installedRule struct {
	Slug     string
	Controls []string
	// Params are the parameter values in its arguments, keyed <check>.<name>.
	Params map[string]string
	// Trigger is the Trigger, for what a separated rule copies from its group's.
	Trigger *goclientnew.Trigger
}

var (
	controlPattern = regexp.MustCompile(`^CH-[0-9]{4}$`)
	// A check's name is written into CEL as a string literal, and into annotation keys.
	namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// Validate checks that the pack's checks can be installed: names and control numbers unique and
// well formed, and the checks naming one rule able to run together.
func (p Pack) Validate() error {
	names := map[string]bool{}
	controls := map[string]bool{}
	byRule := map[string]Check{}
	declared := map[string]bool{}
	for _, r := range p.Rules {
		declared[r.Slug] = true
	}
	libraries := map[string]bool{}
	for _, l := range p.Libraries {
		if libraries[l.Name] {
			return fmt.Errorf("library %s is declared twice", l.Name)
		}
		libraries[l.Name] = true
		if err := l.validate(); err != nil {
			return err
		}
	}
	for _, c := range p.Checks {
		switch {
		case !namePattern.MatchString(c.Name) || !namePattern.MatchString(c.Rule):
			return fmt.Errorf("check %q: a check needs a Name and a Rule of lowercase letters, digits and '-'", c.Name)
		case !controlPattern.MatchString(c.Control):
			return fmt.Errorf("check %s: control number %q is not CH-NNNN", c.Name, c.Control)
		case controls[c.Control]:
			return fmt.Errorf("check %s: control number %s is used twice", c.Name, c.Control)
		case names[c.Name]:
			return fmt.Errorf("check %s: the name is used twice", c.Name)
		}
		controls[c.Control] = true
		names[c.Name] = true
		if _, err := cubapi.ParseTriggerPriority(c.Priority); err != nil || c.Priority == "" {
			return fmt.Errorf("check %s: priority %q is not High, Medium or Low", c.Name, c.Priority)
		}
		switch c.Engine {
		case EngineCEL:
			if c.Failures == "" {
				return fmt.Errorf("check %s: a cel check needs Failures", c.Name)
			}
		case EnginePodSecurity:
			if c.Level == "" || c.PodSecurityCheck == "" {
				return fmt.Errorf("check %s: a pss check needs a Level and a PodSecurityCheck", c.Name)
			}
		case EngineFunction:
			if c.Function == "" {
				return fmt.Errorf("check %s: an fn check needs a Function", c.Name)
			}
			if strings.ContainsAny(c.FunctionCheck, ", ") {
				return fmt.Errorf("check %s: FunctionCheck %q has a comma or a space, which would split it", c.Name, c.FunctionCheck)
			}
			if slices.ContainsFunc(c.Arguments, func(a api.FunctionArgument) bool { return a.ParameterName == "check" }) && c.FunctionCheck != "" {
				return fmt.Errorf("check %s: its rule writes the check argument from FunctionCheck, so Arguments may not hold one", c.Name)
			}
		case EngineAdmissionPolicy:
			if err := p.checkPolicy(c); err != nil {
				return err
			}
		default:
			return fmt.Errorf("check %s: unknown engine %q", c.Name, c.Engine)
		}
		for _, param := range c.Params {
			if param.Name == "" || strings.ContainsAny(param.Name, ".=") {
				return fmt.Errorf("check %s: parameter name %q is empty or has '.' or '='", c.Name, param.Name)
			}
		}
		if len(c.Params) > 0 && c.Engine != EngineCEL {
			return fmt.Errorf("check %s: only cel checks take parameters", c.Name)
		}
		for _, rule := range c.ExceptFrom {
			if !namePattern.MatchString(rule) || rule == c.Rule || rule == c.Name {
				return fmt.Errorf("check %s: ExceptFrom %q is not another rule's slug", c.Name, rule)
			}
		}
		first, ok := byRule[c.Rule]
		if !ok {
			byRule[c.Rule] = c
			continue
		}
		if why := cannotShareRule(first, c); why != "" {
			return fmt.Errorf("checks %s and %s name rule %s, but %s", first.Name, c.Name, c.Rule, why)
		}
	}
	for slug := range byRule {
		if declared[slug] {
			return fmt.Errorf("rule %s is both declared and named by checks", slug)
		}
	}
	// A check separated into a rule of its own takes the check's name as the rule's slug, so the
	// name cannot be another rule's.
	for _, c := range p.Checks {
		if _, named := byRule[c.Name]; c.Name != c.Rule && (named || declared[c.Name]) {
			return fmt.Errorf("check %s: rule %s has the same name, which the check's rule would take if it were separated", c.Name, c.Name)
		}
	}
	return nil
}

// checkPolicy checks that the EngineAdmissionPolicy check c names a policy of one of the pack's
// libraries.
func (p Pack) checkPolicy(c Check) error {
	l, ok := p.library(c.Library)
	if !ok {
		return fmt.Errorf("check %s: the pack has no library %q", c.Name, c.Library)
	}
	policies, err := l.policies()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(policies, func(policy admissionPolicy) bool { return policy.Metadata.Name == c.Policy }) {
		return fmt.Errorf("check %s: library %s has no policy %q", c.Name, c.Library, c.Policy)
	}
	if strings.ContainsAny(c.Policy, ", ") {
		return fmt.Errorf("check %s: policy %q has a comma or a space, which would split it", c.Name, c.Policy)
	}
	return nil
}

// cannotShareRule says why two checks cannot run in one rule, or "" when they can.
func cannotShareRule(a, b Check) string {
	a, b = a.asFunction(), b.asFunction()
	switch {
	case a.Engine != b.Engine:
		return "their engines differ"
	case a.Engine == EngineFunction && (a.FunctionCheck == "" || b.FunctionCheck == ""):
		return "an fn check without a FunctionCheck is a rule of its own"
	case a.Function != b.Function:
		return "their functions differ"
	case !reflect.DeepEqual(a.Arguments, b.Arguments):
		return "their functions' arguments differ"
	case !slices.Equal(sortedTypes(a.ResourceTypes), sortedTypes(b.ResourceTypes)):
		return "their resource types differ"
	case a.AppliedTo != b.AppliedTo:
		return "one applies to Targets and the other does not"
	case !slices.Equal(sortedTypes(a.ExceptFrom), sortedTypes(b.ExceptFrom)):
		return "they honor the exceptions of different rules"
	case a.Library != b.Library:
		return "their policy libraries differ"
	}
	return ""
}

func sortedTypes(types []string) []string {
	out := slices.Clone(types)
	slices.Sort(out)
	return slices.Compact(out)
}

// check finds a check by name.
func (p Pack) check(name string) (Check, bool) {
	i := slices.IndexFunc(p.Checks, func(c Check) bool { return c.Name == name })
	if i < 0 {
		return Check{}, false
	}
	return p.Checks[i], true
}

// ruleInfo describes the rule slug, from its RuleInfo or, for a rule of one check, the check.
func (p Pack) ruleInfo(slug string, checks []Check) RuleInfo {
	if i := slices.IndexFunc(p.RuleInfo, func(r RuleInfo) bool { return r.Slug == slug }); i >= 0 {
		return p.RuleInfo[i]
	}
	if len(checks) == 1 {
		return RuleInfo{Slug: slug, DisplayName: checks[0].DisplayName, Description: checks[0].Description}
	}
	return RuleInfo{Slug: slug}
}

// layout works out the rules an install writes: which checks it installs, and the rule each
// runs in. It installs the pack's default checks, the optional ones selected, and every check
// already installed, so installing again never removes one. A check separated into a rule of
// its own stays there. A check missing a required parameter is skipped, and says so.
func (p Pack) layout(installed []installedRule, sel CheckSelection) ([]Rule, []CheckSkip, error) {
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	for _, name := range sel.Checks {
		if _, ok := p.check(name); !ok {
			return nil, nil, fmt.Errorf("the %s pack has no check %q", p.Group, name)
		}
	}
	for key := range sel.Params {
		name, param, ok := strings.Cut(key, ".")
		c, found := p.check(name)
		if !ok || !found || !slices.ContainsFunc(c.Params, func(pm Param) bool { return pm.Name == param }) {
			return nil, nil, fmt.Errorf("the %s pack has no parameter %q: parameters are <check>.<name>", p.Group, key)
		}
	}

	placed := map[string]installedRule{} // control -> the rule installed with it
	params := map[string]string{}
	for _, r := range installed {
		for _, control := range r.Controls {
			placed[control] = r
		}
		maps.Copy(params, r.Params)
	}
	maps.Copy(params, sel.Params)

	var skipped []CheckSkip
	type ruleChecks struct {
		checks    []Check
		separated *installedRule
	}
	rules := map[string]*ruleChecks{}
	var order []string
	for _, c := range p.Checks {
		at, isInstalled := placed[c.Control]
		wanted := !c.Optional || sel.All || slices.Contains(sel.Checks, c.Name) || isInstalled
		if !wanted {
			continue
		}
		if missing := missingParams(c, params); len(missing) > 0 {
			skipped = append(skipped, CheckSkip{Check: c.Name,
				Reason: "needs --param " + strings.Join(missing, ", --param ")})
			continue
		}
		// A check separated into a rule of its own runs in a rule named after it. One installed
		// in any other rule has moved to the rule it names now.
		slug := c.Rule
		var separated *installedRule
		if isInstalled && at.Slug == c.Name && c.Name != c.Rule {
			slug = at.Slug
			separated = &at
		}
		if rules[slug] == nil {
			rules[slug] = &ruleChecks{separated: separated}
			order = append(order, slug)
		}
		rules[slug].checks = append(rules[slug].checks, c)
	}

	out := make([]Rule, 0, len(p.Rules)+len(order))
	out = append(out, p.Rules...)
	for _, slug := range order {
		rc := rules[slug]
		rule, err := p.ruleOf(slug, rc.checks, params)
		if err != nil {
			return nil, nil, err
		}
		if rc.separated != nil {
			// A separated check still skips the resources excepted from the rule it came from.
			rule.ExceptFrom = append([]string{rc.checks[0].Rule}, rule.ExceptFrom...)
		}
		out = append(out, rule)
	}
	return out, skipped, nil
}

// missingParams are the <check>.<name> of the required parameters of c that params lacks.
func missingParams(c Check, params map[string]string) []string {
	var missing []string
	for _, param := range c.Params {
		if _, ok := params[c.Name+"."+param.Name]; param.Required && !ok {
			missing = append(missing, c.Name+"."+param.Name+"=<value>")
		}
	}
	return missing
}

// ruleOf is the rule slug running checks, which can run together.
func (p Pack) ruleOf(slug string, checks []Check, params map[string]string) (Rule, error) {
	first := checks[0]
	info := p.ruleInfo(slug, checks)
	rule := Rule{
		Slug:          slug,
		DisplayName:   info.DisplayName,
		Description:   info.Description,
		AppliedTo:     first.AppliedTo,
		ResourceTypes: sortedTypes(first.ResourceTypes),
		ExceptFrom:    sortedTypes(first.ExceptFrom),
	}
	for _, c := range checks {
		rule.Controls = append(rule.Controls, c.Control)
		rule.Priority = higherPriority(rule.Priority, c.Priority)
	}
	first = first.asFunction()
	switch first.Engine {
	case EngineCEL:
		rule.Function = ValidatingFunction
		rule.Arguments = []api.FunctionArgument{{ParameterName: "expression", Value: CheckExpression(checks)}}
		for _, c := range checks {
			for _, param := range c.Params {
				key := c.Name + "." + param.Name
				value, ok := params[key]
				if !ok {
					value = param.Default
				}
				rule.Arguments = append(rule.Arguments, api.FunctionArgument{ParameterName: "param", Value: key + "=" + value})
			}
		}
	case EngineFunction:
		rule.Function = first.Function
		rule.Arguments = slices.Clone(first.Arguments)
		if first.FunctionCheck == "" {
			if len(checks) > 1 {
				return Rule{}, fmt.Errorf("rule %s: an fn check without a FunctionCheck is a rule of its own", slug)
			}
			break
		}
		ids := make([]string, len(checks))
		for i, c := range checks {
			ids[i] = c.asFunction().FunctionCheck
		}
		rule.Arguments = append(rule.Arguments, api.FunctionArgument{ParameterName: "check", Value: strings.Join(ids, ",")})
	case EngineAdmissionPolicy:
		names := make([]string, len(checks))
		for i, c := range checks {
			names[i] = c.Policy
		}
		rule.Function, rule.Library = AdmissionPolicyFunction, first.Library
		rule.Arguments = []api.FunctionArgument{{ParameterName: "policy", Value: strings.Join(names, ",")}}
	}
	return rule, nil
}

func higherPriority(a, b string) string {
	rank := map[string]int{"": 0, PriorityLow: 1, PriorityMedium: 2, PriorityHigh: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// CheckExpression is the vet-cel expression of a rule running the cel checks: the failures of
// each, with each failure naming its check as an Issue, in one result.
func CheckExpression(checks []Check) string {
	lists := make([]string, len(checks))
	for i, c := range checks {
		lists[i] = "(" + c.Failures + ").map(f, {'ResourceName': f.ResourceName, 'ResourceType': f.ResourceType, " +
			"'Path': f.Path, ?'Value': f.?Value, 'Detail': f.Detail, " +
			"'Issues': [{'Identifier': '" + c.Name + "', 'Message': f.Detail}]})"
	}
	// The list is bound once, as the element of a one-element list, since vet-cel's CEL has no
	// variable binding and the result reads it three times.
	return "[" + strings.Join(lists, " + ") + "].map(f, {'passed': size(f) == 0, " +
		"'details': f.map(a, a.Detail), 'failed_attributes': f})[0]"
}

// controlsOf are the control numbers a rule's Trigger records.
func controlsOf(t *goclientnew.Trigger) []string {
	return cubapi.TriggerControls(t.Annotations)
}

// installedRules reads what the Triggers of a pack's rules say about the checks they run: the
// control numbers each records, and the parameters in its arguments.
func installedRules(triggers []*goclientnew.Trigger) []installedRule {
	var out []installedRule
	for _, t := range triggers {
		r := installedRule{Slug: t.Slug, Params: map[string]string{}, Trigger: t, Controls: controlsOf(t)}
		for _, arg := range t.Arguments {
			if arg.ParameterName == nil || *arg.ParameterName != "param" {
				continue
			}
			if arg.Value == nil {
				continue
			}
			if s, err := arg.Value.AsFunctionArgumentValue0(); err == nil {
				if key, value, ok := strings.Cut(s, "="); ok && strings.Contains(key, ".") {
					r.Params[key] = value
				}
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// ruleSlugs are the slugs of the rules the pack declares or its checks name, and those its checks'
// rules would have if separated: each check's name.
func (p Pack) ruleSlugs() []string {
	var out []string
	for _, r := range p.Rules {
		out = append(out, r.Slug)
	}
	for _, c := range p.Checks {
		for _, slug := range []string{c.Rule, c.Name} {
			if !slices.Contains(out, slug) {
				out = append(out, slug)
			}
		}
	}
	return out
}

// hasTargetChecks reports whether any check belongs on Targets.
func (p Pack) hasTargetChecks() bool {
	return slices.ContainsFunc(p.Checks, func(c Check) bool { return c.AppliedTo == AppliedToTarget })
}

// resourceTypesWhere is the WhereResource clause selecting types, "" for every type.
func resourceTypesWhere(types []string) string {
	if len(types) == 0 {
		return ""
	}
	quoted := make([]string, len(types))
	for i, t := range types {
		quoted[i] = "'" + t + "'"
	}
	if len(quoted) == 1 {
		return "ConfigHub.ResourceType = " + quoted[0]
	}
	return "ConfigHub.ResourceType IN (" + strings.Join(quoted, ", ") + ")"
}
