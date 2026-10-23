// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

const (
	testBaseID   = "11111111-1111-1111-1111-111111111111"
	testNewID    = "22222222-2222-2222-2222-222222222222"
	testTargetID = "33333333-3333-3333-3333-333333333333"
	testUnitID   = "44444444-4444-4444-4444-444444444444"
)

// A recorded request to the fake hub.
type hubCall struct {
	method string
	path   string
	query  url.Values
	body   string
}

// variantServer is a fake hub with one base Space and one unit, that records every call and
// answers each as the real one would. invokeResponse is the function invocation's answer.
func variantServer(t *testing.T, invokeResponse string) (*Client, *[]hubCall) {
	t.Helper()
	var calls []hubCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, hubCall{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: string(body)})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/space/"+testBaseID:
			_, _ = io.WriteString(w, `{"Space":{"SpaceID":"`+testBaseID+`","Slug":"base","Labels":{"Namespace":"confighubplaceholder"}}}`)
		case r.Method == "POST" && r.URL.Path == "/api/_space":
			_, _ = io.WriteString(w, `[{"Space":{"SpaceID":"`+testNewID+`","Slug":"pr-7"}}]`)
		case r.Method == "PATCH" && r.URL.Path == "/api/space/"+testNewID:
			_, _ = io.WriteString(w, `{"SpaceID":"`+testNewID+`","Slug":"pr-7"}`)
		case r.Method == "POST" && r.URL.Path == "/api/unit":
			_, _ = io.WriteString(w, `[{"Unit":{"UnitID":"`+testUnitID+`","Slug":"deployment"}}]`)
		case r.Method == "POST" && r.URL.Path == "/api/function/invoke":
			_, _ = io.WriteString(w, invokeResponse)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"not found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return c, &calls
}

func testUUID(s string) goclientnew.UUID { return goclientnew.UUID(uuid.MustParse(s)) }

func findCall(calls []hubCall, method, path string) *hubCall {
	for i := range calls {
		if calls[i].method == method && calls[i].path == path {
			return &calls[i]
		}
	}
	return nil
}

func TestCreateVariantMakesTheCallsVariantCreateMakes(t *testing.T) {
	c, calls := variantServer(t, `[{"UnitID":"`+testUnitID+`","UnitSlug":"deployment","Success":true}]`)
	target := testUUID(testTargetID)
	changeset := testUUID("55555555-5555-5555-5555-555555555555")
	res, err := CreateVariant(context.Background(), c, VariantSpec{
		Upstream:          testUUID(testBaseID),
		Name:              "PR 7",
		VariantLabels:     []string{"Stage=preview"},
		SpacePattern:      "template:pr-7",
		SpacePatch:        json.RawMessage(`{"Labels":{"PR":"7"},"Annotations":{"team":"a"}}`),
		UnitPatch:         json.RawMessage(`{"DeleteGates":{"critical":true}}`),
		Target:            &target,
		Namespace:         "pr-7",
		ChangeSetID:       &changeset,
		ChangeDescription: "preview pr-7: clone of base",
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	if res.Space.Slug != "pr-7" || res.UnitsStatus != 200 || len(res.Units) != 1 || len(res.UnitErrors()) != 0 {
		t.Errorf("result = %+v", res)
	}

	// The Space: cloned from the base with the variant, namespace and caller's labels, the
	// slug pattern, and the caller's patch with the upstream annotation layered in.
	spaceCreate := findCall(*calls, "POST", "/api/_space")
	if spaceCreate == nil {
		t.Fatalf("no space clone among %+v", *calls)
	}
	if got := spaceCreate.query.Get("variant_labels"); got != "Variant=PR 7,Namespace=pr-7,Stage=preview" {
		t.Errorf("variant_labels = %q", got)
	}
	if got := spaceCreate.query.Get("name_pattern"); got != "template:pr-7" {
		t.Errorf("name_pattern = %q", got)
	}
	var spacePatch struct{ Labels, Annotations map[string]string }
	_ = json.Unmarshal([]byte(spaceCreate.body), &spacePatch)
	if spacePatch.Labels["PR"] != "7" || spacePatch.Annotations["team"] != "a" || spacePatch.Annotations[AnnotationUpstreamSpaceID] != testBaseID {
		t.Errorf("space patch = %s", spaceCreate.body)
	}

	// The release target, defaulted from Target.
	patch := findCall(*calls, "PATCH", "/api/space/"+testNewID)
	if patch == nil || !strings.Contains(patch.body, testTargetID) {
		t.Errorf("release target patch = %+v", patch)
	}

	// The units: from the base into the new Space, the caller's patch with the target, the
	// description and the changeset layered in.
	unitCreate := findCall(*calls, "POST", "/api/unit")
	if unitCreate == nil || !strings.Contains(unitCreate.query.Get("where"), testBaseID) || !strings.Contains(unitCreate.query.Get("where_space"), testNewID) {
		t.Fatalf("unit clone = %+v", unitCreate)
	}
	var unitPatch struct {
		TargetID, LastChangeDescription, ChangeSetID string
		DeleteGates                                  map[string]bool
	}
	_ = json.Unmarshal([]byte(unitCreate.body), &unitPatch)
	if unitPatch.TargetID != testTargetID || unitPatch.LastChangeDescription != "preview pr-7: clone of base" ||
		unitPatch.ChangeSetID != changeset.String() || !unitPatch.DeleteGates["critical"] {
		t.Errorf("unit patch = %s", unitCreate.body)
	}

	// set-namespace over the new Space, in the changeset, moving only the base's namespace.
	invoke := findCall(*calls, "POST", "/api/function/invoke")
	if invoke == nil || !strings.Contains(invoke.query.Get("where"), testNewID) || invoke.query.Get("change_set_id") != changeset.String() {
		t.Fatalf("invoke = %+v", invoke)
	}
	var req goclientnew.FunctionInvocationsRequest
	_ = json.Unmarshal([]byte(invoke.body), &req)
	if req.FunctionInvocations == nil || len(*req.FunctionInvocations) != 1 || (*req.FunctionInvocations)[0].FunctionName != "set-namespace" {
		t.Fatalf("invoke body = %s", invoke.body)
	}
	args := (*req.FunctionInvocations)[0].Arguments
	if len(args) != 2 || args[1].ParameterName == nil || *args[1].ParameterName != "old-namespace" {
		t.Errorf("set-namespace args = %+v", args)
	}
}

// Without a namespace, a target or a patch, the clone sends the no-op patch and nothing else.
func TestCreateVariantMinimal(t *testing.T) {
	c, calls := variantServer(t, `[]`)
	if _, err := CreateVariant(context.Background(), c, VariantSpec{Upstream: testUUID(testBaseID), Name: "dev"}); err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	if unitCreate := findCall(*calls, "POST", "/api/unit"); unitCreate == nil || unitCreate.body != "null" {
		t.Errorf("unit clone = %+v", unitCreate)
	}
	if findCall(*calls, "PATCH", "/api/space/"+testNewID) != nil || findCall(*calls, "POST", "/api/function/invoke") != nil {
		t.Errorf("unexpected calls: %+v", *calls)
	}
}

// A unit set-namespace fails on is an error that names it, with the Space in the result.
func TestCreateVariantReportsAFailedNamespace(t *testing.T) {
	c, _ := variantServer(t, `[{"UnitID":"`+testUnitID+`","UnitSlug":"deployment","Success":false,"Error":{"Message":"bad yaml"}}]`)
	res, err := CreateVariant(context.Background(), c, VariantSpec{Upstream: testUUID(testBaseID), Name: "dev", Namespace: "dev"})
	if err == nil || !strings.Contains(err.Error(), "deployment") || !strings.Contains(err.Error(), "bad yaml") {
		t.Errorf("err = %v", err)
	}
	if res == nil || res.Space == nil || res.Space.Slug != "pr-7" {
		t.Errorf("result = %+v, want the created Space", res)
	}
}

func TestResolveOAuthClientFallsBackToTheName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/oauth_client/ui-pr-7", "/api/oauth_client/nope":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"not found"}`)
		case "/api/oauth_client":
			_, _ = io.WriteString(w, `[{"ClientID":"oac-x-ui-pr-7","Name":"ui-pr-7","RedirectURIs":["https://ui-pr-7.example/"]}]`)
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := ResolveOAuthClient(context.Background(), c, "ui-pr-7")
	if err != nil {
		t.Fatalf("ResolveOAuthClient: %v", err)
	}
	if client.ClientID != "oac-x-ui-pr-7" {
		t.Errorf("client = %+v", client)
	}
	_, err = ResolveOAuthClient(context.Background(), c, "nope")
	if !IsNotFoundError(err) {
		t.Errorf("missing client: err = %v, want not found", err)
	}
}
