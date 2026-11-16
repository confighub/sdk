// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"slices"
	"strings"
	"testing"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

const controllers = "apps/v1/Deployment"

// failsWhen is the Failures of a cel check failing r at path when cond holds.
func failsWhen(cond, path, detail string) string {
	return "(" + cond + ") ? [{'ResourceName': r.metadata.name, 'ResourceType': r.apiVersion + '/' + r.kind, " +
		"'Path': '" + path + "', ?'Value': optional.none(), 'Detail': '" + detail + "'}] : []"
}

func testCatalog() Pack {
	return Pack{
		Group: "test",
		Checks: []Check{
			{Control: "CH-0301", Name: "privileged", Rule: "test-baseline", Engine: EngineCEL, Priority: PriorityHigh,
				ResourceTypes: []string{controllers},
				Failures:      failsWhen("has(r.spec.privileged) && r.spec.privileged", "spec.privileged", "runs privileged")},
			{Control: "CH-0302", Name: "host-network", Rule: "test-baseline", Engine: EngineCEL, Priority: PriorityMedium,
				ResourceTypes: []string{controllers},
				Failures:      failsWhen("has(r.spec.hostNetwork) && r.spec.hostNetwork", "spec.hostNetwork", "uses the host network")},
			{Control: "CH-0303", Name: "max-replicas", Rule: "test-baseline", Engine: EngineCEL, Priority: PriorityLow,
				ResourceTypes: []string{controllers}, Optional: true,
				Params:   []Param{{Name: "max", Required: true}},
				Failures: failsWhen("r.spec.replicas > int(params['max-replicas.max'])", "spec.replicas", "too many replicas")},
			{Control: "CH-0304", Name: "run-as-non-root", Rule: "test-pss", Engine: EnginePodSecurity, Priority: PriorityMedium,
				ResourceTypes: []string{controllers}, Level: "restricted", PodSecurityCheck: "runAsNonRoot"},
			{Control: "CH-0305", Name: "seccomp", Rule: "test-pss", Engine: EnginePodSecurity, Priority: PriorityMedium,
				ResourceTypes: []string{controllers}, Level: "restricted", PodSecurityCheck: "seccompProfile_restricted"},
			{Control: "CH-0306", Name: "test-schemas", Rule: "test-schemas", Engine: EngineFunction, Priority: PriorityHigh,
				Function: "vet-schemas"},
		},
		RuleInfo: []RuleInfo{{Slug: "test-baseline", DisplayName: "Baseline", Description: "The baseline checks."}},
	}
}

func ruleNamed(t *testing.T, rules []Rule, slug string) Rule {
	t.Helper()
	i := slices.IndexFunc(rules, func(r Rule) bool { return r.Slug == slug })
	if i < 0 {
		t.Fatalf("no rule %s among %v", slug, slugsOf(rules))
	}
	return rules[i]
}

func slugsOf(rules []Rule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.Slug
	}
	return out
}

func argument(r Rule, name string) []string {
	var out []string
	for _, a := range r.Arguments {
		if a.ParameterName == name {
			out = append(out, a.Value.(string))
		}
	}
	return out
}

func TestLayoutGroupsDefaultChecksIntoRules(t *testing.T) {
	rules, skipped, err := testCatalog().layout(nil, CheckSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped %v", skipped)
	}
	if got := slugsOf(rules); !slices.Equal(got, []string{"test-baseline", "test-pss", "test-schemas"}) {
		t.Fatalf("rules = %v", got)
	}

	baseline := ruleNamed(t, rules, "test-baseline")
	if !slices.Equal(baseline.Controls, []string{"CH-0301", "CH-0302"}) {
		t.Errorf("baseline controls = %v; the optional check should not be installed", baseline.Controls)
	}
	if baseline.Priority != PriorityHigh || baseline.DisplayName != "Baseline" || baseline.Function != ValidatingFunction {
		t.Errorf("baseline = %+v", baseline)
	}
	if want := "ConfigHub.ResourceType = 'apps/v1/Deployment' AND metadata.annotations.exceptions~1guardrails~1confighub~1com/test-baseline IS NULL"; baseline.whereResource() != want {
		t.Errorf("WhereResource = %s", baseline.whereResource())
	}

	pss := ruleNamed(t, rules, "test-pss")
	if pss.Function != PodSecurityFunction || !slices.Equal(argument(pss, "check"), []string{"runAsNonRoot,seccompProfile_restricted"}) ||
		!slices.Equal(argument(pss, "level"), []string{"restricted"}) {
		t.Errorf("pss = %+v", pss)
	}

	schemas := ruleNamed(t, rules, "test-schemas")
	if schemas.Function != "vet-schemas" || schemas.whereResource() != exceptionWhereResource("test-schemas") {
		t.Errorf("schemas = %+v, WhereResource %s", schemas, schemas.whereResource())
	}
}

func TestLayoutOptionalChecksAndParameters(t *testing.T) {
	p := testCatalog()

	// Asked for without its required parameter, a check is skipped, and says what it needs.
	rules, skipped, err := p.layout(nil, CheckSelection{Checks: []string{"max-replicas"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0].Check != "max-replicas" || !strings.Contains(skipped[0].Reason, "--param max-replicas.max=<value>") {
		t.Errorf("skipped = %v", skipped)
	}
	if slices.Contains(ruleNamed(t, rules, "test-baseline").Controls, "CH-0303") {
		t.Error("a check missing a required parameter was installed")
	}

	rules, skipped, err = p.layout(nil, CheckSelection{All: true, Params: map[string]string{"max-replicas.max": "5"}})
	if err != nil || len(skipped) != 0 {
		t.Fatalf("err %v, skipped %v", err, skipped)
	}
	baseline := ruleNamed(t, rules, "test-baseline")
	if !slices.Contains(baseline.Controls, "CH-0303") || !slices.Equal(argument(baseline, "param"), []string{"max-replicas.max=5"}) {
		t.Errorf("baseline = %+v", baseline)
	}

	for _, bad := range []CheckSelection{
		{Checks: []string{"nonexistent"}},
		{Params: map[string]string{"privileged.max": "1"}},
		{Params: map[string]string{"max": "1"}},
	} {
		if _, _, err := p.layout(nil, bad); err == nil {
			t.Errorf("layout(%+v) did not fail", bad)
		}
	}
}

// Installing again keeps what was installed: an optional check, its parameter, and a check
// separated into a rule of its own.
func TestLayoutKeepsWhatIsInstalled(t *testing.T) {
	installed := []installedRule{
		{Slug: "test-baseline", Controls: []string{"CH-0302", "CH-0303"}, Params: map[string]string{"max-replicas.max": "7"}},
		{Slug: "privileged", Controls: []string{"CH-0301"}},
	}
	rules, skipped, err := testCatalog().layout(installed, CheckSelection{})
	if err != nil || len(skipped) != 0 {
		t.Fatalf("err %v, skipped %v", err, skipped)
	}
	baseline := ruleNamed(t, rules, "test-baseline")
	if !slices.Equal(baseline.Controls, []string{"CH-0302", "CH-0303"}) || !slices.Equal(argument(baseline, "param"), []string{"max-replicas.max=7"}) {
		t.Errorf("baseline = %+v", baseline)
	}
	separated := ruleNamed(t, rules, "privileged")
	if !slices.Equal(separated.Controls, []string{"CH-0301"}) || !slices.Equal(separated.ExceptFrom, []string{"test-baseline"}) {
		t.Errorf("separated = %+v", separated)
	}
	if !strings.Contains(separated.whereResource(), "confighub~1com/privileged IS NULL AND ") ||
		!strings.HasSuffix(separated.whereResource(), "confighub~1com/test-baseline IS NULL") {
		t.Errorf("a separated check should honor both rules' exceptions: %s", separated.whereResource())
	}
}

// A check installed in a rule that is neither its own nor named after it has been regrouped by a
// newer version of the pack, and moves to the rule it names now.
func TestLayoutMovesARegroupedCheck(t *testing.T) {
	installed := []installedRule{{Slug: "test-old-group", Controls: []string{"CH-0301", "CH-0302"}}}
	rules, _, err := testCatalog().layout(installed, CheckSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if got := slugsOf(rules); slices.Contains(got, "test-old-group") {
		t.Errorf("rules = %v: the regrouped checks stayed in their old rule", got)
	}
	baseline := ruleNamed(t, rules, "test-baseline")
	if !slices.Equal(baseline.Controls, []string{"CH-0301", "CH-0302"}) || len(baseline.ExceptFrom) != 0 {
		t.Errorf("baseline = %+v", baseline)
	}
}

// The rule a separated check runs in is named after the check, so exceptions can name it.
func TestRuleSlugsIncludeSeparatedRules(t *testing.T) {
	slugs := testCatalog().ruleSlugs()
	for _, want := range []string{"test-baseline", "test-pss", "test-schemas", "privileged", "run-as-non-root"} {
		if !slices.Contains(slugs, want) {
			t.Errorf("ruleSlugs = %v, missing %s", slugs, want)
		}
	}
}

func TestValidateRejectsChecksThatCannotRunTogether(t *testing.T) {
	for name, mutate := range map[string]func(*Pack){
		"bad control":      func(p *Pack) { p.Checks[0].Control = "CH-1" },
		"repeated control": func(p *Pack) { p.Checks[1].Control = p.Checks[0].Control },
		"repeated name":    func(p *Pack) { p.Checks[1].Name = p.Checks[0].Name },
		"repeated rule name": func(p *Pack) {
			p.Checks[0].Rule, p.Checks[0].Name = "a", "a"
			p.Checks[1].Rule, p.Checks[1].Name = "a", "a"
		},
		"name of another rule": func(p *Pack) { p.Checks[0].Name = "test-pss" },
		"bad name":             func(p *Pack) { p.Checks[0].Name = "Privileged'" },
		"engines differ":       func(p *Pack) { p.Checks[3].Rule = "test-baseline" },
		"types differ":         func(p *Pack) { p.Checks[1].ResourceTypes = []string{"v1/Pod"} },
		"fn shares a rule":     func(p *Pack) { p.Checks[5].Rule = "test-baseline"; p.Checks[5].ResourceTypes = []string{controllers} },
		"levels differ":        func(p *Pack) { p.Checks[4].Level = "baseline" },
		"no priority":          func(p *Pack) { p.Checks[0].Priority = "" },
		"pss param":            func(p *Pack) { p.Checks[3].Params = []Param{{Name: "x"}} },
		"except from differs":  func(p *Pack) { p.Checks[3].ExceptFrom = []string{"test-runs-nonroot"} },
		"except from itself": func(p *Pack) {
			p.Checks[3].ExceptFrom = []string{"test-pss"}
			p.Checks[4].ExceptFrom = []string{"test-pss"}
		},
		"except from a bad slug": func(p *Pack) {
			p.Checks[3].ExceptFrom = []string{"Runs Nonroot"}
			p.Checks[4].ExceptFrom = []string{"Runs Nonroot"}
		},
	} {
		p := testCatalog()
		p.Checks = slices.Clone(p.Checks)
		mutate(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: Validate passed", name)
		}
	}
	if err := testCatalog().Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestInstalledRulesReadsControlsAndParameters(t *testing.T) {
	param := goclientnew.FunctionArgument{ParameterName: ptr("param"), Value: &goclientnew.FunctionArgument_Value{}}
	_ = param.Value.FromFunctionArgumentValue0("max-replicas.max=7")
	expression := goclientnew.FunctionArgument{ParameterName: ptr("expression"), Value: &goclientnew.FunctionArgument_Value{}}
	_ = expression.Value.FromFunctionArgumentValue0("a=b")
	got := installedRules([]*goclientnew.Trigger{{
		Slug:        "test-baseline",
		Annotations: map[string]string{ControlsAnnotation: "CH-0301, CH-0303"},
		Arguments:   []goclientnew.FunctionArgument{expression, param},
	}})
	if len(got) != 1 || !slices.Equal(got[0].Controls, []string{"CH-0301", "CH-0303"}) || len(got[0].Params) != 1 || got[0].Params["max-replicas.max"] != "7" {
		t.Errorf("installedRules = %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }

// CheckExpression runs each check's failures, naming each failure's check as its Issue. The
// expression is evaluated by the packs' tests, which have vet-cel's CEL environment.
func TestCheckExpressionNamesEachFailuresCheck(t *testing.T) {
	p := testCatalog()
	expression := CheckExpression(p.Checks[:2])
	for _, want := range []string{
		"(" + p.Checks[0].Failures + ").map(f, ",
		"'Issues': [{'Identifier': 'privileged', 'Message': f.Detail}]",
		"'Issues': [{'Identifier': 'host-network', 'Message': f.Detail}]",
		"?'Value': f.?Value",
		"].map(f, {'passed': size(f) == 0,",
	} {
		if !strings.Contains(expression, want) {
			t.Errorf("expression lacks %q:\n%s", want, expression)
		}
	}
}

// Checks of one function that runs several, chosen by its check argument, share a rule.
func TestLayoutGroupsChecksOfOneFunction(t *testing.T) {
	correctness := func(control, name, id string) Check {
		return Check{Control: control, Name: name, Rule: "test-correctness", Engine: EngineFunction, Priority: PriorityHigh,
			Function: "vet-correctness", FunctionCheck: id}
	}
	p := Pack{Group: "test", Checks: []Check{
		correctness("CH-0201", "selector-matches-template", "selector-matches-template"),
		correctness("CH-0202", "label-syntax", "label-syntax"),
	}}
	rules, _, err := p.layout(nil, CheckSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Function != "vet-correctness" ||
		!slices.Equal(argument(rules[0], "check"), []string{"selector-matches-template,label-syntax"}) ||
		!slices.Equal(rules[0].Controls, []string{"CH-0201", "CH-0202"}) {
		t.Errorf("rules = %+v", rules)
	}

	for name, mutate := range map[string]func(*Pack){
		"one without a FunctionCheck": func(p *Pack) { p.Checks[1].FunctionCheck = "" },
		"different functions":         func(p *Pack) { p.Checks[1].Function = "vet-other" },
		"different arguments": func(p *Pack) {
			p.Checks[1].Arguments = []api.FunctionArgument{{ParameterName: "strict", Value: "true"}}
		},
		"a comma in FunctionCheck": func(p *Pack) { p.Checks[0].FunctionCheck = "a,b" },
		"a check argument": func(p *Pack) {
			p.Checks[0].Arguments = []api.FunctionArgument{{ParameterName: "check", Value: "x"}}
		},
	} {
		bad := p
		bad.Checks = slices.Clone(p.Checks)
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: Validate passed", name)
		}
	}
}

// A rule honors the exceptions of the rules its checks name in ExceptFrom, and a check separated
// from it honors them too, beside the group rule's.
func TestLayoutHonorsExceptFrom(t *testing.T) {
	p := testCatalog()
	p.Checks = slices.Clone(p.Checks)
	p.Checks[3].ExceptFrom = []string{"test-runs-nonroot"}
	p.Checks[4].ExceptFrom = []string{"test-runs-nonroot"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	rules, _, err := p.layout([]installedRule{{Slug: "seccomp", Controls: []string{"CH-0305"}}}, CheckSelection{})
	if err != nil {
		t.Fatal(err)
	}
	group := ruleNamed(t, rules, "test-pss")
	if !slices.Equal(group.ExceptFrom, []string{"test-runs-nonroot"}) ||
		!strings.HasSuffix(group.whereResource(), "confighub~1com/test-pss IS NULL AND metadata.annotations.exceptions~1guardrails~1confighub~1com/test-runs-nonroot IS NULL") {
		t.Errorf("group = %+v, where %s", group, group.whereResource())
	}
	if separated := ruleNamed(t, rules, "seccomp"); !slices.Equal(separated.ExceptFrom, []string{"test-pss", "test-runs-nonroot"}) {
		t.Errorf("separated = %+v", separated)
	}
}
