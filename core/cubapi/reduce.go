// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Reduce summarizes what functions return across the units sel selects, in the server: it runs
// fns, the map, on each unit, puts each value they return in the groups groupBy names, and runs
// reducer once on each group's values. Only the reducer's output for each group comes back, rather
// than every unit's configuration. groupBy is a CEL expression over each value, as row, with where
// it came from in row.Origin; empty puts every value in one group. Nothing is written.
//
// A unit the map or groupBy fails on is left out, and reported in the response's UnitErrors; a
// group the reducer fails on is reported in the group. Neither is an error here: check Success.
func Reduce(ctx context.Context, c *Client, sel Selector, fns []api.FunctionInvocation, groupBy string, reducer api.FunctionInvocation) (*goclientnew.FunctionReduceResponse, error) {
	body := goclientnew.FunctionReduceRequest{
		FunctionInvocations: FunctionInvocations(fns...),
		GroupBy:             groupBy,
		Reduce:              &(*FunctionInvocations(reducer))[0],
		ToolchainType:       sel.toolchain(),
		WhereResource:       sel.WhereResource,
	}
	params := &goclientnew.ReduceFunctionsOnOrgParams{
		Where:         ptrIf(sel.Where),
		Filter:        ptrIf(sel.Filter),
		WhereData:     ptrIf(sel.WhereData),
		ResourceType:  ptrIf(sel.ResourceType),
		ExecutorSpace: ptrIf(sel.ExecutorSpace),
		IncludeHidden: ptrIf(sel.IncludeHidden),
	}
	res, err := c.API.ReduceFunctionsOnOrgWithResponse(ctx, params, body)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("reduce returned no response")
	}
	return res.JSON200, nil
}

// ReduceGroupValues reads the values a reducer returned for a group, for a reducer whose output is
// an AttributeValueList, as reduce-starlark's is.
func ReduceGroupValues(group goclientnew.FunctionReduceGroup) (api.AttributeValueList, error) {
	if !group.Success {
		return nil, fmt.Errorf("the reducer failed on group %q: %v", group.Key, group.ErrorMessages)
	}
	if group.OutputType != string(api.OutputTypeAttributeValueList) {
		return nil, fmt.Errorf("the reducer returned %s for group %q, not %s", group.OutputType, group.Key, api.OutputTypeAttributeValueList)
	}
	encoded, err := json.Marshal(group.Output)
	if err != nil {
		return nil, fmt.Errorf("encode the output of group %q: %w", group.Key, err)
	}
	var values api.AttributeValueList
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, fmt.Errorf("read the output of group %q: %w", group.Key, err)
	}
	return values, nil
}
