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

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestReduceWiring(t *testing.T) {
	var path, where, filter, resourceType string
	var sent goclientnew.FunctionReduceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		where = r.URL.Query().Get("where")
		filter = r.URL.Query().Get("filter")
		resourceType = r.URL.Query().Get("resource_type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Groups":[{"Key":"prod","NumRows":2,"NumUnits":2,"Success":true,
			"OutputType":"AttributeValueList","Output":[{"Path":"replicas","Value":5}]}],
			"NumUnits":2,"NumRows":2,"Success":true}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	response, err := Reduce(context.Background(), c,
		Selector{Where: "Space.Slug = 'prod'", Filter: "prod/deployments", ResourceType: "apps/v1/Deployment", WhereResource: "ConfigHub.ResourceType = 'apps/v1/Deployment'"},
		[]api.FunctionInvocation{{FunctionName: "get-replicas"}},
		"row.Origin.SpaceSlug",
		api.FunctionInvocation{FunctionName: "reduce-starlark", Arguments: []api.FunctionArgument{{Value: "def reduce(rows): return []"}, {Value: "k=v"}}})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if path != "/api/function/reduce" || where != "Space.Slug = 'prod'" || filter != "prod/deployments" || resourceType != "apps/v1/Deployment" {
		t.Errorf("request: path %q, where %q, filter %q, resource_type %q", path, where, filter, resourceType)
	}
	if sent.GroupBy != "row.Origin.SpaceSlug" || sent.ToolchainType != DefaultToolchainType ||
		sent.WhereResource != "ConfigHub.ResourceType = 'apps/v1/Deployment'" {
		t.Errorf("body: %+v", sent)
	}
	if sent.FunctionInvocations == nil || (*sent.FunctionInvocations)[0].FunctionName != "get-replicas" {
		t.Errorf("map: %+v", sent.FunctionInvocations)
	}
	if sent.Reduce == nil || sent.Reduce.FunctionName != "reduce-starlark" || len(sent.Reduce.Arguments) != 2 {
		t.Errorf("reducer: %+v", sent.Reduce)
	}

	values, err := ReduceGroupValues(response.Groups[0])
	if err != nil {
		t.Fatalf("ReduceGroupValues: %v", err)
	}
	if len(values) != 1 || values[0].Path != "replicas" || values[0].Value != float64(5) {
		t.Errorf("values: %+v", values)
	}
}

func TestReduceGroupValuesOfAFailedGroup(t *testing.T) {
	_, err := ReduceGroupValues(goclientnew.FunctionReduceGroup{Key: "prod", ErrorMessages: []string{"starlark error"}})
	if err == nil {
		t.Fatal("expected an error for a group the reducer failed on")
	}
}
