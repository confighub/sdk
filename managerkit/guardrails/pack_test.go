// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
)

func TestRuleInvocationDefaultsToCEL(t *testing.T) {
	fn, args := Rule{Slug: "s", Expression: "r.kind != 'Pod'"}.invocation()
	if fn != ValidatingFunction {
		t.Errorf("function = %q, want %q", fn, ValidatingFunction)
	}
	// vet-cel takes its expression as a named parameter; passing it positionally
	// is accepted by a different function entirely and silently means something
	// else.
	if len(args) != 1 || args[0].ParameterName != "expression" || args[0].Value != "r.kind != 'Pod'" {
		t.Errorf("args = %+v", args)
	}
}

func TestRuleInvocationOverride(t *testing.T) {
	r := Rule{Slug: "s", Function: "vet-schemas"}
	fn, args := r.invocation()
	if fn != "vet-schemas" || len(args) != 0 {
		t.Errorf("fn = %q, args = %+v", fn, args)
	}
	r.Arguments = []api.FunctionArgument{{ParameterName: "k", Value: "v"}}
	if _, args := r.invocation(); len(args) != 1 {
		t.Errorf("args = %+v", args)
	}
}

func TestSelectionWhere(t *testing.T) {
	for _, tc := range []struct {
		sel  Selection
		want string
	}{
		{Selection{PolicySpace: "common", Groups: []string{"namespace"}},
			"Space.Slug = 'common' AND Labels.Pack IN ('namespace') AND Labels.AppliedTo != 'Target'"},
		{Selection{PolicySpace: "confighub-system", Groups: []string{"basic", "rbac"}, Target: true},
			"Space.Slug = 'confighub-system' AND Labels.Pack IN ('basic', 'rbac') AND Labels.AppliedTo = 'Target'"},
	} {
		got, err := tc.sel.Where()
		if err != nil || got != tc.want {
			t.Errorf("Where() = %q, %v; want %q", got, err, tc.want)
		}
		parsed, ok := ParseSelection(got)
		if !ok || !reflect.DeepEqual(parsed, tc.sel) {
			t.Errorf("ParseSelection(%q) = %+v, %v; want %+v", got, parsed, ok, tc.sel)
		}
	}
}

// Only what Selection.Where writes is ours to rewrite. Anything else is someone's
// own selection, and adding a group to it would mean guessing what they meant.
func TestParseSelectionRejectsOtherExpressions(t *testing.T) {
	for _, where := range []string{
		"",
		"SpaceID = 'ce022de3-bb4c-4dbb-990f-831260862c3a'",
		"Space.Slug = 'confighub-system' AND Labels.Pack IN ('basic')",
		"Labels.Pack IN ('basic') AND Labels.AppliedTo != 'Target'",
		"Space.Slug = 'common' AND Labels.Pack IN ('basic') AND Labels.AppliedTo != 'Target' AND Slug = 'x'",
		"Space.Slug = 'common' AND Labels.Pack IN (basic) AND Labels.AppliedTo != 'Target'",
	} {
		if sel, ok := ParseSelection(where); ok {
			t.Errorf("ParseSelection(%q) = %+v, want not ours", where, sel)
		}
	}
}

func TestSelectionWithKeepsGroupsSortedAndDistinct(t *testing.T) {
	sel := Selection{PolicySpace: "common", Groups: []string{"basic", "rbac"}}
	if got := sel.with("namespace").Groups; !reflect.DeepEqual(got, []string{"basic", "namespace", "rbac"}) {
		t.Errorf("with(namespace) = %v", got)
	}
	if got := sel.with("rbac").Groups; !reflect.DeepEqual(got, []string{"basic", "rbac"}) {
		t.Errorf("with(rbac) = %v", got)
	}
	if !reflect.DeepEqual(sel.Groups, []string{"basic", "rbac"}) {
		t.Errorf("with changed the receiver: %v", sel.Groups)
	}
}

// The annotation key's dots are path separators unless escaped, so an unescaped
// key would name a path that never exists and except nothing.
func TestExceptionWhereResource(t *testing.T) {
	want := "metadata.annotations.exceptions~1guardrails~1confighub~1com/no-rbac-wildcards IS NULL"
	if got := exceptionWhereResource("no-rbac-wildcards"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRuleLabels(t *testing.T) {
	if got := (Rule{}).labels("rbac"); !reflect.DeepEqual(got, map[string]string{GroupLabel: "rbac"}) {
		t.Errorf("Space rule labels = %v", got)
	}
	want := map[string]string{GroupLabel: "basic", AppliedToLabel: AppliedToTarget}
	if got := (Rule{AppliedTo: AppliedToTarget}).labels("basic"); !reflect.DeepEqual(got, want) {
		t.Errorf("Target rule labels = %v, want %v", got, want)
	}
}

func TestDecide(t *testing.T) {
	pack := Pack{Group: "rbac"}
	common := func(target bool, groups ...string) string {
		where, err := Selection{PolicySpace: "common", Groups: groups, Target: target}.Where()
		if err != nil {
			t.Fatal(err)
		}
		return where
	}
	for _, tc := range []struct {
		name          string
		where, filter string
		ownSelection  bool
		own           int
		upstream      bool // the Space selects the Triggers in its upstream Space
		target        bool
		wire          string // the WhereTrigger written, when wired
		already, skip bool
	}{
		{name: "new Space", ownSelection: true, wire: common(false, "rbac")},
		{name: "new Target", ownSelection: true, target: true, wire: common(true, "rbac")},
		{name: "Space with its own Triggers", ownSelection: true, own: 2, skip: true},
		{name: "variant of a Space with no Triggers", ownSelection: true, upstream: true, wire: common(false, "rbac")},
		{name: "variant of a Space with Triggers", ownSelection: true, upstream: true, own: 1, skip: true},
		{name: "adds to a selection", where: common(false, "basic", "workload"), wire: common(false, "basic", "rbac", "workload")},
		{name: "already selects the group", where: common(false, "basic", "rbac"), already: true},
		{name: "another policy Space", where: strings.Replace(common(false, "basic"), "'common'", "'other'", 1), skip: true},
		{name: "Target selection on a Space", where: common(true, "basic"), skip: true},
		{name: "TriggerFilterID", ownSelection: true, filter: "0b5b5d1e-0000-0000-0000-000000000000", skip: true},
		{name: "custom WhereTrigger", where: "Labels.Pack = 'basic'", skip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plan Plan
			w := Wiring{Space: "s"}
			if tc.upstream && tc.own > 0 {
				w.selectsFrom = "its upstream Space base"
			}
			if err := pack.decide(&plan, w, "common", tc.where, tc.filter, tc.ownSelection, tc.own, tc.target); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.wire != "":
				if len(plan.Wire) != 1 || plan.Wire[0].WhereTrigger != tc.wire {
					t.Errorf("plan = %+v, want wired with %q", plan, tc.wire)
				}
			case tc.already:
				if len(plan.AlreadyWired) != 1 || len(plan.Wire)+len(plan.Skipped) != 0 {
					t.Errorf("plan = %+v, want already wired", plan)
				}
			case tc.skip:
				if len(plan.Skipped) != 1 || len(plan.Wire)+len(plan.AlreadyWired) != 0 {
					t.Errorf("plan = %+v, want skipped", plan)
				} else if tc.upstream && !strings.Contains(plan.Skipped[0].Reason, "upstream Space") {
					t.Errorf("reason = %q, want it to name the upstream Space", plan.Skipped[0].Reason)
				}
			}
		})
	}
}

// A variant cub variant create made from a Space selecting its own Triggers selects the Triggers
// in that upstream Space, and a variant made from the variant copies that selection. An install
// wires them when that Space holds none, and leaves them alone when it holds some.
func TestPlanWiresAVariantOfASpaceWithNoTriggers(t *testing.T) {
	const (
		policyID   = "10000000-0000-4000-8000-000000000000"
		emptyBase  = "20000000-0000-4000-8000-000000000000"
		fullBase   = "30000000-0000-4000-8000-000000000000"
		emptyChild = "40000000-0000-4000-8000-000000000000"
		fullChild  = "50000000-0000-4000-8000-000000000000"
		emptyGrand = "60000000-0000-4000-8000-000000000000"
		fullGrand  = "70000000-0000-4000-8000-000000000000"
	)
	space := func(id, slug, where, upstream string) string {
		s := fmt.Sprintf(`{"Space":{"SpaceID":%q,"Slug":%q,"WhereTrigger":%q`, id, slug, where)
		if upstream != "" {
			s += fmt.Sprintf(`,"UpstreamSpaceID":%q`, upstream)
		}
		return s + "}}"
	}
	spaces := map[string]string{
		emptyBase:  space(emptyBase, "empty-base", "", ""),
		fullBase:   space(fullBase, "full-base", "", ""),
		emptyChild: space(emptyChild, "empty-prod", "SpaceID = '"+emptyBase+"'", emptyBase),
		fullChild:  space(fullChild, "full-prod", "SpaceID = '"+fullBase+"'", fullBase),
		emptyGrand: space(emptyGrand, "empty-prod-eu", "SpaceID = '"+emptyBase+"'", emptyChild),
		fullGrand:  space(fullGrand, "full-prod-eu", "SpaceID = '"+fullBase+"'", fullChild),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		where := r.URL.Query().Get("where")
		switch strings.TrimPrefix(r.URL.Path, "/api") {
		case "/space":
			if strings.Contains(where, "'common'") {
				fmt.Fprintf(w, "[%s]", space(policyID, "common", "", ""))
				return
			}
			for id, s := range spaces {
				if strings.Contains(where, id) {
					fmt.Fprintf(w, "[%s]", s)
					return
				}
			}
			all := slices.Sorted(maps.Values(spaces))
			fmt.Fprintf(w, "[%s]", strings.Join(all, ","))
		case "/trigger":
			if strings.Contains(where, fullBase) {
				fmt.Fprintf(w, `[{"Trigger":{"Slug":"own","SpaceID":%q}}]`, fullBase)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := packOf("rule").PlanFor(context.Background(), client, "common",
		[]string{"empty-base", "full-base", "empty-prod", "full-prod", "empty-prod-eu", "full-prod-eu"}, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	var wired, skipped []string
	for _, w := range plan.Wire {
		wired = append(wired, w.Space)
	}
	for _, s := range plan.Skipped {
		skipped = append(skipped, s.Space+": "+s.Reason)
	}
	if want := []string{"empty-base", "empty-prod", "empty-prod-eu"}; !slices.Equal(wired, want) {
		t.Errorf("wired %q, want %q", wired, want)
	}
	want := []string{
		"full-base: has 1 Trigger(s) of its own, which a guardrail selection would stop running",
		"full-prod: selects the 1 Trigger(s) in its upstream Space full-base, which a guardrail selection would stop running",
		"full-prod-eu: selects the 1 Trigger(s) in the Space full-base, which a guardrail selection would stop running",
	}
	if !slices.Equal(skipped, want) {
		t.Errorf("skipped %q, want %q", skipped, want)
	}
}
