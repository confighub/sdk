// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

const testPolicies = `# Adopted from the test library.
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: test-no-host-pid
spec:
  validations:
    - expression: "!has(object.spec.hostPID) || !object.spec.hostPID"
---
# Takes the allowed registries as parameters.
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: test-allowed-registries
spec:
  paramKind:
    apiVersion: example.com/v1
    kind: Settings
  validations:
    - expression: "true"
---
# A trailing comment only.
`

func testLibrary() PolicyLibrary {
	return PolicyLibrary{
		Name: "testlib", Source: "example/policies@abc123", License: "Apache-2.0",
		Policies: testPolicies,
		Params:   "apiVersion: example.com/v1\nkind: Settings\nmetadata:\n  name: defaults\nregistries: [ghcr.io]\n",
		ParamRef: "defaults",
	}
}

func vapCatalog() Pack {
	p := testCatalog()
	p.Libraries = []PolicyLibrary{testLibrary()}
	p.Checks = append(slices.Clone(p.Checks),
		Check{Control: "CH-0310", Name: "no-host-pid", Rule: "test-vap", Engine: EngineAdmissionPolicy, Priority: PriorityHigh,
			ResourceTypes: []string{controllers}, Library: "testlib", Policy: "test-no-host-pid"},
		Check{Control: "CH-0311", Name: "allowed-registries", Rule: "test-vap", Engine: EngineAdmissionPolicy, Priority: PriorityMedium,
			ResourceTypes: []string{controllers}, Library: "testlib", Policy: "test-allowed-registries"},
	)
	return p
}

func TestYAMLDocuments(t *testing.T) {
	docs := yamlDocuments(testPolicies)
	if len(docs) != 2 || !strings.HasPrefix(docs[0], "# Adopted") || !strings.Contains(docs[1], "test-allowed-registries") {
		t.Errorf("documents = %q", docs)
	}
}

// The checks of one library naming one rule run in it, their policies joined into its policy
// argument, and the rule names the library whose Units install passes it.
func TestAdmissionPolicyChecksShareARule(t *testing.T) {
	p := vapCatalog()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	rules, _, err := p.layout(nil, CheckSelection{})
	if err != nil {
		t.Fatal(err)
	}
	rule := ruleNamed(t, rules, "test-vap")
	fn, args := rule.invocation()
	if fn != AdmissionPolicyFunction || rule.Library != "testlib" || rule.Priority != PriorityHigh ||
		!slices.Equal(argument(rule, "policy"), []string{"test-no-host-pid,test-allowed-registries"}) || len(args) != 1 {
		t.Errorf("rule = %+v", rule)
	}
}

func TestValidateRejectsBadAdmissionPolicyChecks(t *testing.T) {
	for name, mutate := range map[string]func(*Pack){
		"no such library": func(p *Pack) { p.Checks[6].Library = "other" },
		"no such policy":  func(p *Pack) { p.Checks[6].Policy = "test-missing" },
		"libraries differ": func(p *Pack) {
			other := testLibrary()
			other.Name = "other"
			p.Libraries = append(p.Libraries, other)
			p.Checks[7].Library = "other"
		},
		"no source":        func(p *Pack) { p.Libraries[0].Source = "" },
		"params missing":   func(p *Pack) { p.Libraries[0].Params = "" },
		"not a policy":     func(p *Pack) { p.Libraries[0].Policies = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n" },
		"library twice":    func(p *Pack) { p.Libraries = append(p.Libraries, testLibrary()) },
		"bad library name": func(p *Pack) { p.Libraries[0].Name = "Test Lib" },
	} {
		p := vapCatalog()
		p.Libraries = slices.Clone(p.Libraries)
		mutate(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: Validate passed", name)
		}
	}
}

// The library Unit holds the policies and a binding of each that denies, with the library's
// parameters for a policy taking some, and no matchResources.
func TestLibraryUnitData(t *testing.T) {
	data, err := testLibrary().unitData()
	if err != nil {
		t.Fatal(err)
	}
	docs := yamlDocuments(data)
	if len(docs) != 4 {
		t.Fatalf("%d documents:\n%s", len(docs), data)
	}
	bindings := map[string]map[string]any{}
	for _, doc := range docs[2:] {
		var b map[string]any
		if err := yaml.Unmarshal([]byte(doc), &b); err != nil {
			t.Fatal(err)
		}
		if b["kind"] != admissionBindingType {
			t.Fatalf("not a binding: %s", doc)
		}
		bindings[b["metadata"].(map[string]any)["name"].(string)] = b["spec"].(map[string]any)
	}
	plain, withParams := bindings["test-no-host-pid"], bindings["test-allowed-registries"]
	if plain["policyName"] != "test-no-host-pid" || plain["paramRef"] != nil || plain["matchResources"] != nil {
		t.Errorf("binding = %v", plain)
	}
	if ref, _ := withParams["paramRef"].(map[string]any); ref["name"] != "defaults" || ref["parameterNotFoundAction"] != "Deny" {
		t.Errorf("binding = %v", withParams)
	}
	if !slices.Equal(withParams["validationActions"].([]any), []any{"Deny"}) {
		t.Errorf("binding = %v", withParams)
	}
}

func TestPlanNamesLibraryUnits(t *testing.T) {
	p := vapCatalog()
	if got, want := p.LibraryUnit("testlib"), "guardrails-test-testlib"; got != want {
		t.Errorf("LibraryUnit = %s", got)
	}
	if got := ParamsUnit("testlib"); got != "guardrails-testlib-params" {
		t.Errorf("ParamsUnit = %s", got)
	}
}

// libraryServer serves the policy Space testSpaceID, selecting Triggers by whereTrigger or by a
// filter, holding no Units. It records each Unit created and each patch of the Space.
func libraryServer(t *testing.T, whereTrigger string, filter bool) (*cubapi.Client, *[]string, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var created, patches []string
	space := map[string]any{"SpaceID": testSpaceID, "Slug": "common", "WhereTrigger": whereTrigger}
	if filter {
		space["TriggerFilterID"] = uuid.NewString()
	}
	spaceJSON, _ := json.Marshal(space)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		switch {
		case r.Method == http.MethodGet && (path == "/space" || path == "/space/"+testSpaceID):
			if path == "/space" {
				fmt.Fprintf(w, `[{"Space":%s}]`, spaceJSON)
			} else {
				_, _ = w.Write(spaceJSON)
			}
		case r.Method == http.MethodPatch && path == "/space/"+testSpaceID:
			body, _ := io.ReadAll(r.Body)
			patches = append(patches, string(body))
			_, _ = w.Write(spaceJSON)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/unit"):
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && path == "/space/"+testSpaceID+"/unit":
			var u map[string]any
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &u)
			slug, _ := u["Slug"].(string)
			created = append(created, slug)
			fmt.Fprintf(w, `{"Unit":{"UnitID":%q,"SpaceID":%q,"Slug":%q}}`, unitIDOf(slug), testSpaceID, slug)
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/data"):
			fmt.Fprintf(w, `{"Unit":{"UnitID":%q,"SpaceID":%q}}`, strings.Split(path, "/")[4], testSpaceID)
		default:
			t.Errorf("unexpected request %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return c, &created, &patches
}

// unitIDOf is the ID libraryServer gives the Unit slug.
func unitIDOf(slug string) string { return uuid.NewSHA1(uuid.NameSpaceOID, []byte(slug)).String() }

// ensureLibraries writes each library's Units once, passes them to every rule running its
// policies and to no other rule, and writes no params Unit for a library without parameters.
func TestEnsureLibraries(t *testing.T) {
	p := vapCatalog()
	bare := testLibrary()
	bare.Name, bare.Params, bare.ParamRef = "bare", "", ""
	bare.Policies = yamlDocuments(testPolicies)[0]
	p.Libraries = append(p.Libraries, bare)
	rules := []Rule{
		{Slug: "test-vap", Library: "testlib"}, {Slug: "no-host-pid", Library: "testlib"},
		{Slug: "test-bare", Library: "bare"}, {Slug: "test-baseline"},
	}
	c, created, _ := libraryServer(t, "", false)
	if err := p.ensureLibraries(context.Background(), c, goclientnew.UUID(uuid.MustParse(testSpaceID)), rules); err != nil {
		t.Fatal(err)
	}
	if want := []string{"guardrails-test-testlib", "guardrails-testlib-params", "guardrails-test-bare"}; !slices.Equal(*created, want) {
		t.Errorf("created %v, want %v", *created, want)
	}
	testlib := map[string]string{PolicyOtherData: unitIDOf("guardrails-test-testlib"), ParamsOtherData: unitIDOf("guardrails-testlib-params")}
	for i, want := range []map[string]string{testlib, testlib, {PolicyOtherData: unitIDOf("guardrails-test-bare")}, nil} {
		got := map[string]string{}
		for key, id := range rules[i].OtherUnitIDs {
			got[key] = id.String()
		}
		if len(got) != len(want) || !maps.Equal(got, want) {
			t.Errorf("%s: OtherUnitIDs = %v, want %v", rules[i].Slug, got, want)
		}
	}
}

// Writing the libraries narrows a policy Space selecting just its own Triggers to those in no
// group, and leaves one selecting Triggers another way alone.
func TestPolicySpaceSkipsRules(t *testing.T) {
	narrowed := fmt.Sprintf(`{"WhereTrigger":"SpaceID = '%s' AND Labels.Pack IS NULL"}`, testSpaceID)
	for name, tc := range map[string]struct {
		whereTrigger string
		filter       bool
		want         []string
	}{
		"default":  {"", false, []string{narrowed}},
		"own":      {fmt.Sprintf("SpaceID = '%s'", testSpaceID), false, []string{narrowed}},
		"narrowed": {fmt.Sprintf("SpaceID = '%s' AND Labels.Pack IS NULL", testSpaceID), false, nil},
		"custom":   {"Labels.Team = 'platform'", false, nil},
		"filter":   {"", true, nil},
	} {
		c, _, patches := libraryServer(t, tc.whereTrigger, tc.filter)
		rules := []Rule{{Slug: "test-vap", Library: "testlib"}}
		if err := vapCatalog().ensureLibraries(context.Background(), c, goclientnew.UUID(uuid.MustParse(testSpaceID)), rules); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(*patches, tc.want) {
			t.Errorf("%s: patches %q, want %q", name, *patches, tc.want)
		}
	}
	// A pack running no library's policies leaves the Space alone.
	c, _, patches := libraryServer(t, "", false)
	if err := vapCatalog().ensureLibraries(context.Background(), c, goclientnew.UUID(uuid.MustParse(testSpaceID)), []Rule{{Slug: "test-baseline"}}); err != nil || len(*patches) != 0 {
		t.Errorf("no library: patches %q, err %v", *patches, err)
	}
}
