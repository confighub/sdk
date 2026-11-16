// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

const (
	policyUnitID  = "6b7c1d2e-0000-4000-8000-000000000001"
	policySpaceID = "6b7c1d2e-0000-4000-8000-000000000002"
	otherUnitID   = "6b7c1d2e-0000-4000-8000-000000000003"
)

type testPolicyUnit struct {
	toolchain string
	policies  int
}

// policyUnitServer serves a Space apps holding units, and the Triggers given as JSON, each found
// by a where naming its slug or listing the test group.
func policyUnitServer(t *testing.T, units map[string]testPolicyUnit, triggers map[string]string) *cubapi.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		where := r.URL.Query().Get("where")
		switch {
		case r.Method == http.MethodGet && path == "/space":
			_, _ = fmt.Fprintf(w, `[{"Space":{"SpaceID":%q,"Slug":"apps"}}]`, policySpaceID)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/unit"):
			for slug, u := range units {
				if strings.Contains(where, "'"+slug+"'") {
					_, _ = fmt.Fprintf(w, `[{"Unit":{"UnitID":%q,"SpaceID":%q,"Slug":%q,"ToolchainType":%q}}]`, policyUnitID, policySpaceID, slug, u.toolchain)
					return
				}
			}
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodGet && path == "/resource":
			n := 0
			for _, u := range units {
				n = u.policies
			}
			items := make([]string, n)
			for i := range items {
				items[i] = fmt.Sprintf(`{"Resource":{"ResourceName":"p%d"}}`, i)
			}
			_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
		case r.Method == http.MethodGet && path == "/trigger":
			var found []string
			for slug, trigger := range triggers {
				if strings.Contains(where, "Labels.Pack IN ('test')") || strings.Contains(where, "'"+slug+"'") {
					found = append(found, trigger)
				}
			}
			_, _ = w.Write([]byte("[" + strings.Join(found, ",") + "]"))
		default:
			t.Errorf("unexpected request %s %s %s", r.Method, path, where)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// triggerJSON is a Trigger in the policy Space named slug, with annotations and the Unit it
// passes as OtherData policy, if any.
func triggerJSON(slug, annotations, policyUnit string) string {
	other := ""
	if policyUnit != "" {
		other = fmt.Sprintf(`,"OtherUnitIDs":{"policy":%q}`, policyUnit)
	}
	return fmt.Sprintf(`{"Trigger":{"TriggerID":%q,"SpaceID":%q,"Slug":%q,"Annotations":{%s}%s}}`,
		uuid.NewString(), policySpaceID, slug, annotations, other)
}

// --policy-unit installs a rule named after the Unit, running its policies, and refuses a Unit
// that holds none, is not Kubernetes/YAML, or would take the name of one of the pack's rules or
// of a Trigger in the policy Space that does not run its policies.
func TestPolicyUnitRule(t *testing.T) {
	spaceID := goclientnew.UUID(uuid.MustParse(policySpaceID))
	units := map[string]testPolicyUnit{"admission": {"Kubernetes/YAML", 2}}
	c := policyUnitServer(t, units, nil)
	rule, err := testCatalog().policyUnitRule(context.Background(), c, &spaceID, "apps/admission")
	if err != nil {
		t.Fatal(err)
	}
	fn, args := rule.invocation()
	if rule.Slug != "admission" || fn != AdmissionPolicyFunction || len(args) != 0 ||
		rule.OtherUnitIDs[PolicyOtherData].String() != policyUnitID || rule.annotations(true)[PolicyUnitAnnotation] != "apps/admission" {
		t.Errorf("rule = %+v", rule)
	}
	if want := "metadata.annotations.exceptions~1guardrails~1confighub~1com/admission IS NULL"; rule.whereResource() != want {
		t.Errorf("where = %s", rule.whereResource())
	}
	// Installing it again updates its own Trigger.
	c = policyUnitServer(t, units, map[string]string{"admission": triggerJSON("admission", `"PolicyUnit":"apps/admission"`, policyUnitID)})
	if _, err := testCatalog().policyUnitRule(context.Background(), c, &spaceID, "apps/admission"); err != nil {
		t.Errorf("installing again: %v", err)
	}

	for name, tc := range map[string]struct {
		ref     string
		unit    testPolicyUnit
		trigger string
		want    string
	}{
		"no policies":    {"apps/admission", testPolicyUnit{"Kubernetes/YAML", 0}, "", "holds no ValidatingAdmissionPolicy"},
		"not kubernetes": {"apps/admission", testPolicyUnit{"OpenTofu/HCL", 1}, "", "not Kubernetes/YAML"},
		"a pack's rule":  {"apps/test-pss", testPolicyUnit{"Kubernetes/YAML", 1}, "", "already has"},
		"no space":       {"admission", testPolicyUnit{"Kubernetes/YAML", 1}, "", "write <space>/<unit>"},
		"another Unit's rule": {"apps/admission", testPolicyUnit{"Kubernetes/YAML", 1},
			triggerJSON("admission", `"PolicyUnit":"infra/admission"`, otherUnitID), "does not run its policies"},
		"another rule": {"apps/admission", testPolicyUnit{"Kubernetes/YAML", 1},
			triggerJSON("admission", `"Controls":"CH-0101"`, ""), "does not run its policies"},
	} {
		slug := tc.ref[strings.LastIndex(tc.ref, "/")+1:]
		var triggers map[string]string
		if tc.trigger != "" {
			triggers = map[string]string{slug: tc.trigger}
		}
		c := policyUnitServer(t, map[string]testPolicyUnit{slug: tc.unit}, triggers)
		if _, err := testCatalog().policyUnitRule(context.Background(), c, &spaceID, tc.ref); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

// The exception commands take a rule installed with --policy-unit, found by its annotation among
// the group's Triggers, beside the rules the packs name, and refuse any other Trigger's slug.
func TestExceptionsTakePolicyUnitRules(t *testing.T) {
	c := policyUnitServer(t, nil, map[string]string{
		"admission": triggerJSON("admission", `"PolicyUnit":"apps/admission"`, policyUnitID),
		"hand-made": triggerJSON("hand-made", `"Priority":"High"`, ""),
	})
	ctx, rules, groups := context.Background(), ruleSlugs([]Pack{testCatalog()}), []string{"test"}
	got, err := policyUnitRuleSlugs(ctx, c, groups)
	if err != nil || !slices.Equal(got, []string{"admission"}) {
		t.Errorf("policyUnitRuleSlugs = %v, %v", got, err)
	}
	if err := checkInstalledRule(ctx, c, "admission", rules, groups); err != nil {
		t.Errorf("the policy-unit rule: %v", err)
	}
	if err := checkInstalledRule(ctx, c, "hand-made", rules, groups); err == nil || !strings.Contains(err.Error(), "unknown rule") {
		t.Errorf("a Trigger without PolicyUnit: err = %v", err)
	}
	// A pack's rule is taken without asking the server.
	if err := checkInstalledRule(ctx, nil, "test-baseline", rules, groups); err != nil {
		t.Errorf("a pack's rule: %v", err)
	}
}
