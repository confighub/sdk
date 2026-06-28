// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestChangeDryRun(t *testing.T) {
	if !(Change{}).DryRun() {
		t.Fatal("empty Change should be dry-run")
	}
	if (Change{Description: "x"}).DryRun() {
		t.Fatal("Change with description should not be dry-run")
	}
}

func TestArguments(t *testing.T) {
	args := Arguments([]api.FunctionArgument{
		{Value: "json"},
		{ParameterName: "key", Value: "cloned"},
		{ParameterName: "role", Value: "{{ .Params.role }}", Evaluator: api.EvaluatorTemplate},
		{ParameterName: "n", Value: 3},
	})
	if len(args) != 4 {
		t.Fatalf("len = %d, want 4", len(args))
	}
	if args[0].ParameterName != nil {
		t.Fatal("positional arg should have nil ParameterName")
	}
	if got, _ := args[0].Value.AsFunctionArgumentValue0(); got != "json" {
		t.Fatalf("arg0 = %q", got)
	}
	if args[2].Evaluator == nil || *args[2].Evaluator != api.EvaluatorTemplate {
		t.Fatalf("arg2 evaluator = %v", args[2].Evaluator)
	}
	if n, _ := args[3].Value.AsFunctionArgumentValue1(); n != 3 {
		t.Fatalf("arg3 int = %d, want 3", n)
	}
}

type mutCapture struct {
	method  string
	dryRun  string
	reqBody []byte
}

func invokeServer(t *testing.T, cap *mutCapture, invokeBody, patchBody string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.dryRun = r.URL.Query().Get("dry_run")
		cap.reqBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodPatch {
			_, _ = w.Write([]byte(patchBody))
		} else {
			_, _ = w.Write([]byte(invokeBody))
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestInvokeFunctionDryRunWiring(t *testing.T) {
	body := `[{"UnitSlug":"checkout","SpaceSlug":"prod","Success":true,"Outputs":{"resources":"[]"}}]`
	var cap mutCapture
	c := invokeServer(t, &cap, body, "")

	res, err := InvokeFunction(context.Background(), c,
		api.FunctionInvocation{FunctionName: "get-resources", Arguments: []api.FunctionArgument{{Value: "json"}}},
		Selector{Where: "ToolchainType = 'Kubernetes/YAML'"},
		Change{})
	if err != nil {
		t.Fatalf("InvokeFunction: %v", err)
	}
	if !res.DryRun || cap.dryRun != "true" {
		t.Fatalf("dryRun result=%v query=%q", res.DryRun, cap.dryRun)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Outputs["resources"] != "[]" {
		t.Fatalf("outcomes = %+v", res.Outcomes)
	}
	var sent goclientnew.FunctionInvocationsRequest
	if err := json.Unmarshal(cap.reqBody, &sent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sent.ToolchainType != DefaultToolchainType {
		t.Fatalf("toolchain = %q", sent.ToolchainType)
	}
	if sent.FunctionInvocations == nil || (*sent.FunctionInvocations)[0].FunctionName != "get-resources" {
		t.Fatalf("invocations = %+v", sent.FunctionInvocations)
	}
}

func TestInvokeFunctionCommitWiring(t *testing.T) {
	var cap mutCapture
	c := invokeServer(t, &cap, `[{"UnitSlug":"checkout","Success":true}]`, "")
	_, err := InvokeFunction(context.Background(), c,
		api.FunctionInvocation{FunctionName: "set-replicas", Arguments: []api.FunctionArgument{{Value: "3"}}},
		Selector{Where: "Slug = 'checkout'"}, Change{Description: "scale checkout to 3"})
	if err != nil {
		t.Fatalf("InvokeFunction: %v", err)
	}
	if cap.dryRun != "" {
		t.Fatalf("dry_run on commit = %q", cap.dryRun)
	}
	var sent goclientnew.FunctionInvocationsRequest
	_ = json.Unmarshal(cap.reqBody, &sent)
	if sent.ChangeDescription != "scale checkout to 3" {
		t.Fatalf("change description = %q", sent.ChangeDescription)
	}
}

func TestInvokeStoredInvocation(t *testing.T) {
	var cap mutCapture
	c := invokeServer(t, &cap, `[{"UnitSlug":"checkout","Success":true,"HasNewMutations":true}]`, "")
	id := goclientnew.UUID(uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	res, err := InvokeStoredInvocation(context.Background(), c, id, map[string]any{"verb": "get"},
		Selector{Where: "Slug = 'checkout'"}, Change{Description: "add get verb"})
	if err != nil {
		t.Fatalf("InvokeStoredInvocation: %v", err)
	}
	if !res.Outcomes[0].HasMutations {
		t.Fatal("expected HasMutations")
	}
	var sent goclientnew.FunctionInvocationsRequest
	_ = json.Unmarshal(cap.reqBody, &sent)
	if len(sent.ParameterizedInvocations) != 1 || sent.ParameterizedInvocations[0].InvocationID != id {
		t.Fatalf("parameterized = %+v", sent.ParameterizedInvocations)
	}
}

func TestUpgradeUnits(t *testing.T) {
	var cap mutCapture
	c := invokeServer(t, &cap, "", `[{"Unit":{"Slug":"checkout","SpaceSlug":"prod"}},{"Error":{"Message":"conflict"}}]`)
	res, err := UpgradeUnits(context.Background(), c, "SpaceID = '00000000-0000-0000-0000-000000000000'", Change{Description: "promote base"})
	if err != nil {
		t.Fatalf("UpgradeUnits: %v", err)
	}
	if cap.method != http.MethodPatch {
		t.Fatalf("method = %s, want PATCH", cap.method)
	}
	if len(res.Outcomes) != 2 || !res.Outcomes[0].Success || res.Outcomes[0].UnitSlug != "checkout" {
		t.Fatalf("outcomes = %+v", res.Outcomes)
	}
	if failed := res.Failed(); len(failed) != 1 || failed[0].Error != "conflict" {
		t.Fatalf("failed = %+v", failed)
	}
}
