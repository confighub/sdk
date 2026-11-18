// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestIsTemporary(t *testing.T) {
	for reason, want := range map[string]bool{
		TemporaryReason("images rebuilt in Q1"): true,
		"ToBeFixed":                             true,
		"ToBeFixed by the platform team":        true,
		"  ToBeFixed: x":                        true,
		"ToBeFixedLater":                        false,
		"singleton by design":                   false,
		"":                                      false,
	} {
		if got := IsTemporary(reason); got != want {
			t.Errorf("IsTemporary(%q) = %v, want %v", reason, got, want)
		}
	}
	if got := TemporaryReason("  images rebuilt in Q1 "); got != "ToBeFixed: images rebuilt in Q1" {
		t.Errorf("TemporaryReason = %q", got)
	}
}

func failedAttrs(attrs ...goclientnew.AttributeValue) *goclientnew.AttributeValueList {
	list := goclientnew.AttributeValueList(attrs)
	return &list
}

func TestFailedResourcesOf(t *testing.T) {
	got := failedResourcesOf([]goclientnew.ValidationResult{
		{Passed: true, FailedAttributes: failedAttrs(goclientnew.AttributeValue{ResourceName: "shop/passed"})},
		{Passed: false, FailedAttributes: failedAttrs(
			goclientnew.AttributeValue{ResourceType: "apps/v1/Deployment", ResourceName: "shop/web", Path: "spec.replicas"},
			goclientnew.AttributeValue{ResourceType: "apps/v1/Deployment", ResourceName: "shop/web", Path: "spec.template"},
			goclientnew.AttributeValue{Path: "no.resource"},
		)},
		{Passed: false, FailedAttributes: failedAttrs(
			goclientnew.AttributeValue{ResourceType: "rbac.authorization.k8s.io/v1/ClusterRole", ResourceName: "/admin"},
		)},
		// vet-cel names a resource whose expression returned false only in a detail.
		{Passed: false, Details: []string{
			"resource admin failed validation",
			"resource shop/api failed validation",
			"resource shop/web failed validation expression r.x",
			"something else",
		}},
	})
	want := []failedResource{
		{resourceType: "apps/v1/Deployment", resourceName: "shop/web"},
		{resourceType: "rbac.authorization.k8s.io/v1/ClusterRole", resourceName: "/admin"},
		{resourceName: "shop/api"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("failedResourcesOf = %+v, want %+v", got, want)
	}
}

// The cases of resourcesToExcept, which the UI's spec of its own copy of the
// rule repeats case for case, so that the two cannot drift apart unnoticed.
func TestResourcesToExcept(t *testing.T) {
	key := ExceptionAnnotation("rule")
	web := unitResource{resourceType: "apps/v1/Deployment", resourceName: "shop/web"}
	svc := unitResource{resourceType: "v1/Service", resourceName: "shop/web"}
	api := unitResource{resourceType: "apps/v1/Deployment", resourceName: "shop/api"}
	role := unitResource{resourceType: "rbac.authorization.k8s.io/v1/ClusterRole", resourceName: "/admin"}
	excepted := func(r unitResource) unitResource {
		r.annotations = map[string]any{key: "ToBeFixed: x"}
		return r
	}
	for _, tc := range []struct {
		name   string
		failed []failedResource
		all    []unitResource
		want   []string
		kind   NotExceptedKind
	}{
		{name: "named with its type", failed: []failedResource{{"apps/v1/Deployment", "shop/web"}}, all: []unitResource{web, svc, api},
			want: []string{"apps/v1/Deployment/shop/web"}},
		{name: "named without its type", failed: []failedResource{{"", "shop/api"}}, all: []unitResource{web, svc, api},
			want: []string{"apps/v1/Deployment/shop/api"}},
		{name: "cluster-scoped, named without a slash", failed: []failedResource{{"", "admin"}}, all: []unitResource{role, web},
			want: []string{"rbac.authorization.k8s.io/v1/ClusterRole/admin"}},
		{name: "two failing", failed: []failedResource{{"apps/v1/Deployment", "shop/web"}, {"", "shop/api"}}, all: []unitResource{web, svc, api},
			want: []string{"apps/v1/Deployment/shop/web", "apps/v1/Deployment/shop/api"}},
		{name: "unnamed, one resource", all: []unitResource{web}, want: []string{"apps/v1/Deployment/shop/web"}},
		{name: "unnamed, several", all: []unitResource{web, api}, kind: NotExceptedUnnamed},
		{name: "ambiguous name", failed: []failedResource{{"", "shop/web"}}, all: []unitResource{web, svc}, kind: NotExceptedAmbiguous},
		// A Unit is excepted from all of its failures or none, so a Unit left out still fails.
		{name: "one of two not held", failed: []failedResource{{"apps/v1/Deployment", "shop/web"}, {"", "shop/gone"}}, all: []unitResource{web},
			kind: NotExceptedNotHeld},
		{name: "already excepted", failed: []failedResource{{"apps/v1/Deployment", "shop/web"}}, all: []unitResource{excepted(web), api},
			kind: NotExceptedAlreadyExcepted},
		{name: "already excepted is left out", failed: []failedResource{{"apps/v1/Deployment", "shop/web"}, {"apps/v1/Deployment", "shop/api"}},
			all: []unitResource{excepted(web), api}, want: []string{"apps/v1/Deployment/shop/api"}},
	} {
		got, skip := resourcesToExcept(tc.failed, tc.all, key)
		if tc.kind != "" {
			if skip == nil || skip.kind != tc.kind || got != nil {
				t.Errorf("%s: got %v, %+v; want %s", tc.name, refs(got), skip, tc.kind)
			}
			continue
		}
		if skip != nil || !slices.Equal(refs(got), tc.want) {
			t.Errorf("%s: got %v, %+v; want %v", tc.name, refs(got), skip, tc.want)
		}
	}
}

const (
	ruleTriggerID  = "44444444-4444-4444-4444-444444444444"
	otherTrigger   = "55555555-5555-5555-5555-555555555555"
	unitA, unitB   = "a0000000-0000-0000-0000-00000000000a", "b0000000-0000-0000-0000-00000000000b"
	unitC, unitD   = "c0000000-0000-0000-0000-00000000000c", "d0000000-0000-0000-0000-00000000000d"
	appSpaceID     = "66666666-6666-6666-6666-666666666666"
	gateOfRule     = "policy/rule/vet-cel"
	gateOfOtherOne = "policy/other/vet-cel"
)

// temporaryServer answers what PlanTemporaryExceptions reads, and records the
// function invocations ExecuteTemporaryExceptions sends, by their where.
//
// Units a and b fail the rule on their Deployment web, each also holding a
// Service; c fails it with results naming nothing and holds two resources; d
// fails only another rule.
func temporaryServer(t *testing.T) (*cubapi.Client, map[string]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	invoked := map[string]map[string]any{}
	results := `{"` + gateOfRule + `":[{"Passed":false,"Index":0,"FailedAttributes":[{"ResourceType":"apps/v1/Deployment","ResourceName":"shop/web","Path":"spec"}]}]}`
	unit := func(id, slug, gate, results string) string {
		return fmt.Sprintf(`{"Unit":{"UnitID":%q,"Slug":%q,"SpaceID":%q,"ValidationWarnings":{%q:true},"ValidationTriggerIDs":{%q:%q},"ValidationResults":%s},"Space":{"SpaceID":%q,"Slug":"app"}}`,
			id, slug, appSpaceID, gate, gate, map[string]string{gateOfRule: ruleTriggerID, gateOfOtherOne: otherTrigger}[gate], results, appSpaceID)
	}
	units := []string{
		unit(unitA, "a", gateOfRule, results),
		unit(unitB, "b", gateOfRule, results),
		unit(unitC, "c", gateOfRule, `{"`+gateOfRule+`":[{"Passed":false,"Index":0,"Issues":[{"Identifier":"x","Message":"y"}]}]}`),
		unit(unitD, "d", gateOfOtherOne, `{}`),
	}
	resource := func(unitID, typ, name string) string {
		return fmt.Sprintf(`{"Resource":{"UnitID":%q,"SpaceSlug":"app","ResourceType":%q,"ResourceName":%q,"Data":{}}}`, unitID, typ, name)
	}
	var resources []string
	for _, id := range []string{unitA, unitB, unitC} {
		resources = append(resources, resource(id, "apps/v1/Deployment", "shop/web"), resource(id, "v1/Service", "shop/web"))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		where := r.URL.Query().Get("where")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		switch {
		case r.Method == http.MethodGet && path == "/trigger":
			if !strings.Contains(where, "Slug = 'rule'") {
				t.Errorf("triggers listed by %q", where)
			}
			fmt.Fprintf(w, `[{"Trigger":{"TriggerID":%q,"Slug":"rule"}}]`, ruleTriggerID)
		case r.Method == http.MethodGet && path == "/unit" && strings.Contains(where, "LEN(ValidationWarnings)"):
			fmt.Fprintf(w, "[%s]", strings.Join(units, ","))
		case r.Method == http.MethodGet && path == "/unit" && strings.Contains(where, "LEN(ValidationErrors)"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && path == "/unit" && strings.Contains(where, "UnitID IN"):
			var out []string
			for i, id := range []string{unitA, unitB, unitC, unitD} {
				if strings.Contains(where, id) {
					out = append(out, units[i])
				}
			}
			fmt.Fprintf(w, "[%s]", strings.Join(out, ","))
		case r.Method == http.MethodGet && path == "/resource":
			var out []string
			for _, res := range resources {
				var er struct{ Resource struct{ UnitID string } }
				_ = json.Unmarshal([]byte(res), &er)
				if strings.Contains(where, er.Resource.UnitID) {
					out = append(out, res)
				}
			}
			fmt.Fprintf(w, "[%s]", strings.Join(out, ","))
		case r.Method == http.MethodPost && path == "/function/invoke":
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			invoked[where] = m
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, path, r.URL.RawQuery)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c, invoked
}

// The resources a rule fails are excepted Unit by Unit, with one request for
// the Units failing on the same resources; a Unit whose failure cannot be
// placed is reported, and a Unit failing only another rule is not touched.
func TestTemporaryExceptions(t *testing.T) {
	c, invoked := temporaryServer(t)
	ctx := context.Background()
	plan, err := PlanTemporaryExceptions(ctx, c, "rule", []string{"test"}, "")
	if err != nil {
		t.Fatalf("PlanTemporaryExceptions: %v", err)
	}
	var excepted []string
	for _, e := range plan.Excepted {
		excepted = append(excepted, e.Unit+":"+e.Resource())
	}
	if want := []string{"a:apps/v1/Deployment/shop/web", "b:apps/v1/Deployment/shop/web"}; !slices.Equal(excepted, want) {
		t.Errorf("Excepted = %v, want %v", excepted, want)
	}
	if len(plan.NotExcepted) != 1 || plan.NotExcepted[0].Unit != "c" || plan.NotExcepted[0].Space != "app" || plan.NotExcepted[0].Kind != NotExceptedUnnamed {
		t.Errorf("NotExcepted = %+v, want c alone", plan.NotExcepted)
	}

	if _, err := ExecuteTemporaryExceptions(ctx, c, plan, TemporaryReason("later"), cubapi.Change{Description: "d"}); err != nil {
		t.Fatalf("ExecuteTemporaryExceptions: %v", err)
	}
	if len(invoked) != 1 {
		t.Fatalf("invoked %d times, want once for a and b: %v", len(invoked), invoked)
	}
	for where, body := range invoked {
		if !strings.Contains(where, unitA) || !strings.Contains(where, unitB) || strings.Contains(where, unitC) {
			t.Errorf("invoked on %q, want a and b", where)
		}
		fns, _ := body["FunctionInvocations"].([]any)
		if len(fns) != 1 {
			t.Fatalf("invocations = %v, want one", body["FunctionInvocations"])
		}
		fn, _ := fns[0].(map[string]any)
		args, err := json.Marshal(fn["Arguments"])
		if err != nil {
			t.Fatalf("arguments: %v", err)
		}
		if !strings.Contains(string(args), ExceptionAnnotation("rule")) || !strings.Contains(string(args), "ToBeFixed: later") {
			t.Errorf("arguments = %s", args)
		}
		if w, _ := fn["WhereResource"].(string); !strings.Contains(w, "apps/v1/Deployment") || !strings.Contains(w, "shop/web") {
			t.Errorf("WhereResource = %q", w)
		}
	}
}
