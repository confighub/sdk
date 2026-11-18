// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMatchResource(t *testing.T) {
	all := []unitResource{
		{resourceType: "apps/v1/Deployment", resourceName: "shop/web"},
		{resourceType: "v1/Service", resourceName: "shop/web"},
		{resourceType: "serving.knative.dev/v1/Service", resourceName: "shop/web"},
		{resourceType: "v1/Service", resourceName: "prod/api"},
		{resourceType: "rbac.authorization.k8s.io/v1/ClusterRole", resourceName: "/admin"},
	}
	for _, tc := range []struct {
		spec, want, err string
	}{
		{spec: "apps/v1/Deployment/shop/web", want: "apps/v1/Deployment/shop/web"},
		{spec: "Deployment/web", want: "apps/v1/Deployment/shop/web"},
		{spec: "deployment/shop/web", want: "apps/v1/Deployment/shop/web"},
		{spec: "v1/Service/shop/web", want: "v1/Service/shop/web"},
		{spec: "serving.knative.dev/v1/Service/shop/web", want: "serving.knative.dev/v1/Service/shop/web"},
		{spec: "serving.knative.dev/v1/Service/web", want: "serving.knative.dev/v1/Service/shop/web"},
		{spec: "Service/api", want: "v1/Service/prod/api"},
		{spec: "ClusterRole/admin", want: "rbac.authorization.k8s.io/v1/ClusterRole/admin"},
		{spec: "rbac.authorization.k8s.io/v1/ClusterRole/admin", want: "rbac.authorization.k8s.io/v1/ClusterRole/admin"},
		{spec: "Service/shop/web", err: "names 2 resources; name one in full: v1/Service/shop/web, serving.knative.dev/v1/Service/shop/web"},
		{spec: "Deployment/Web", err: "no resource Deployment/Web"},
		{spec: "Deployment", err: "must be <type>/[<namespace>/]<name>"},
	} {
		got, err := matchResource(all, tc.spec)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err = %v, want %q", tc.spec, err, tc.err)
			}
			continue
		}
		if err != nil || len(got) != 1 || got[0].ref() != tc.want {
			t.Errorf("%s: got %v, %v; want %s", tc.spec, refs(got), err, tc.want)
		}
	}
}

func TestPickResources(t *testing.T) {
	one := []unitResource{{resourceType: "apps/v1/Deployment", resourceName: "shop/web"}}
	if got, err := pickResources("prod/web", one, ""); err != nil || len(got) != 1 {
		t.Errorf("one resource: got %v, %v", got, err)
	}
	two := append(slices.Clone(one), unitResource{resourceType: "v1/Service", resourceName: "shop/web"})
	if _, err := pickResources("prod/web", two, ""); err == nil || !strings.Contains(err.Error(), "--resource") {
		t.Errorf("two resources without --resource: err = %v", err)
	}
	if got, err := pickResources("prod/web", two, "Service/web"); err != nil || got[0].ref() != "v1/Service/shop/web" {
		t.Errorf("two resources with --resource: got %v, %v", got, err)
	}
}

func TestParseRules(t *testing.T) {
	rules := []string{"a", "b", "c"}
	if got, _ := parseRules("", rules); !slices.Equal(got, rules) {
		t.Errorf("empty: got %v", got)
	}
	if got, _ := parseRules("c, a,c", rules); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("list: got %v", got)
	}
	if _, err := parseRules("a,d", rules); err == nil || !strings.Contains(err.Error(), `"d"`) {
		t.Errorf("unknown: err = %v", err)
	}
}

func TestAnnotationPath(t *testing.T) {
	want := "metadata.annotations.exceptions~1guardrails~1confighub~1com/rbac-no-wildcards"
	if got := annotationPath(ExceptionAnnotation("rbac-no-wildcards")); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// The exception help and list say which rules honor another's exceptions, so an exception that
// skips a resource in more than its own rule says so.
func TestExceptionsHonoredBy(t *testing.T) {
	p := testCatalog()
	p.Checks = slices.Clone(p.Checks)
	p.Checks[3].ExceptFrom = []string{"test-runs-nonroot"}
	p.Checks[4].ExceptFrom = []string{"test-runs-nonroot"}
	honoredBy := exceptionsHonoredBy([]Pack{p})
	if !reflect.DeepEqual(honoredBy, map[string][]string{"test-runs-nonroot": {"test-pss"}}) {
		t.Fatalf("exceptionsHonoredBy = %v", honoredBy)
	}
	want := []string{"An exception to test-runs-nonroot also skips the resource in test-pss."}
	for _, listed := range [][]string{{"test-runs-nonroot"}, {"test-pss"}} {
		if got := honoredByLines(honoredBy, listed); !slices.Equal(got, want) {
			t.Errorf("honoredByLines(%v) = %q", listed, got)
		}
	}
	if got := honoredByLines(honoredBy, []string{"test-baseline"}); len(got) != 0 {
		t.Errorf("honoredByLines of an unrelated rule = %q", got)
	}
	if help := ExceptionCmd(nil, p).Long; !strings.Contains(help, "\n\n"+want[0]+"\n\nAn exception is part") {
		t.Errorf("help does not list the honored exceptions:\n%s", help)
	}
	if help := ExceptionCmd(nil, testCatalog()).Long; !strings.Contains(help, "reads them all back.\n\nAn exception is part") {
		t.Errorf("help without honored exceptions:\n%s", help)
	}
}

// `exception list` counts the temporary exceptions, and with --temporary lists only them.
func TestExceptionReportTemporary(t *testing.T) {
	all := func() []Exception {
		return []Exception{
			{Rule: "a", Space: "s", Unit: "u1", ResourceType: "apps/v1/Deployment", ResourceName: "x/web", Reason: "singleton by design"},
			{Rule: "a", Space: "s", Unit: "u2", ResourceType: "apps/v1/Deployment", ResourceName: "x/web", Reason: TemporaryReason("images rebuilt in Q1"), Temporary: true},
			{Rule: "b", Space: "s", Unit: "u3", ResourceType: "apps/v1/Deployment", ResourceName: "x/api", Reason: TemporaryReason("later"), Temporary: true},
		}
	}
	listed := []string{"a", "b", "c"}
	print := func(rep ExceptionReport, temporary bool) string {
		var out strings.Builder
		cmd := &cobra.Command{}
		cmd.SetOut(&out)
		printExceptions(cmd, rep, listed, nil, temporary)
		return out.String()
	}

	rep := exceptionReport(all(), listed, nil, false, "")
	if rep.Total != 3 || rep.Temporary != 2 || !reflect.DeepEqual(rep.ByRule, map[string]int{"a": 2, "b": 1, "c": 0}) {
		t.Errorf("report = total %d, temporary %d, by rule %v", rep.Total, rep.Temporary, rep.ByRule)
	}
	if text := print(rep, false); !strings.Contains(text, "3 exceptions: 2 a, 1 b\n2 of them temporary, to be fixed: exception list --temporary") {
		t.Errorf("table:\n%s", text)
	}

	rep = exceptionReport(all(), listed, nil, true, "")
	var units []string
	for _, e := range rep.Exceptions {
		units = append(units, e.Unit)
	}
	if !slices.Equal(units, []string{"u2", "u3"}) || rep.Total != 2 || rep.Temporary != 2 ||
		!reflect.DeepEqual(rep.ByRule, map[string]int{"a": 1, "b": 1, "c": 0}) {
		t.Errorf("--temporary report = %v, total %d, temporary %d, by rule %v", units, rep.Total, rep.Temporary, rep.ByRule)
	}
	if text := print(rep, true); !strings.Contains(text, "2 temporary exceptions: 1 a, 1 b") || strings.Contains(text, "of them temporary") {
		t.Errorf("--temporary table:\n%s", text)
	}
	if text := print(exceptionReport(all()[:1], listed, nil, true, ""), true); !strings.Contains(text, "No resources are temporarily excepted") {
		t.Errorf("--temporary with none:\n%s", text)
	}
}
