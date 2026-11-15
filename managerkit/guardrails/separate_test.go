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
)

// installedServer lists the Triggers of the test catalog's rules as installed: the baseline rule
// with two checks, and the pss rule.
func installedServer(t *testing.T) *cubapi.Client {
	t.Helper()
	triggers := []string{
		fmt.Sprintf(`{"Trigger":{"TriggerID":%q,"SpaceID":%q,"Slug":"test-baseline","Warn":false,"ToolchainType":"Kubernetes/YAML",`+
			`"Annotations":{"Controls":"CH-0301,CH-0302","Priority":"High"}}}`, promotedID, testSpaceID),
		fmt.Sprintf(`{"Trigger":{"TriggerID":%q,"SpaceID":%q,"Slug":"test-pss","Warn":true,"ToolchainType":"Kubernetes/YAML",`+
			`"Annotations":{"Controls":"CH-0304,CH-0305"}}}`, warnedID, testSpaceID),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		where := r.URL.Query().Get("where")
		if r.Method == http.MethodGet && path == "/trigger" && strings.Contains(where, "Labels.Pack = 'test'") {
			_, _ = w.Write([]byte("[" + strings.Join(triggers, ",") + "]"))
			return
		}
		t.Errorf("unexpected request %s %s %s", r.Method, path, where)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPlanSeparation(t *testing.T) {
	c := installedServer(t)
	packs := []Pack{{Group: "other"}, testCatalog()}

	sep, err := PlanSeparation(context.Background(), c, "common", packs, "test-baseline", []string{"privileged"})
	if err != nil {
		t.Fatal(err)
	}
	if got := slugsOf(sep.rules); len(got) != 2 {
		t.Fatalf("rules = %v", got)
	}
	group, separated := ruleNamed(t, sep.rules, "test-baseline"), ruleNamed(t, sep.rules, "privileged")
	if !slices.Equal(group.Controls, []string{"CH-0302"}) || !slices.Equal(separated.Controls, []string{"CH-0301"}) {
		t.Errorf("controls = %v and %v", group.Controls, separated.Controls)
	}
	if !slices.Equal(separated.ExceptFrom, []string{"test-baseline"}) || sep.group.Slug != "test-baseline" {
		t.Errorf("separated rule = %+v, group %s", separated, sep.group.Slug)
	}

	for _, tc := range []struct {
		rule   string
		checks []string
		want   string
	}{
		{"test-baseline", []string{"privileged", "host-network"}, "leave it with none"},
		{"test-baseline", []string{"run-as-non-root"}, "does not run in rule test-baseline"},
		{"test-baseline", []string{"nonexistent"}, "no pack has a check"},
		{"test-schemas", []string{"test-schemas"}, "is not installed"},
		{"test-pss", []string{"test-pss"}, "no pack has a check"},
	} {
		if _, err := PlanSeparation(context.Background(), c, "common", packs, tc.rule, tc.checks); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("separate %v from %s: err = %v, want %q", tc.checks, tc.rule, err, tc.want)
		}
	}
}
