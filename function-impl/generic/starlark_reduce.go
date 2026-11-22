// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"go.starlark.net/starlark"
)

// registerReduceStarlark registers reduce-starlark, a reducer: it is passed the rows of one group
// of a reduce rather than configuration data.
func registerReduceStarlark(fh handler.FunctionRegistry, _ configkit.ConfigConverter, _ yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("reduce-starlark", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "reduce-starlark",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "program",
					Required:      true,
					Description: "Starlark program that summarizes the rows of one group. Must define a 'reduce(rows)' function that takes the group's rows and returns a list of dicts, each with fields: Path (string), Value (any), and optionally ResourceName, ResourceType, AttributeName, DataType and Comment (strings). " +
						"Each row is a dict with the fields of the value a function returned (ResourceName, ResourceType, Path, Value, AttributeName, DataType, FunctionName) and Origin, a dict saying where it came from: UnitID, UnitSlug, UnitLabels, SpaceID, SpaceSlug, SpaceLabels, TargetID, TargetSlug, TargetSpaceSlug and RevisionNum, all present: the label dicts are empty when there are none, and TargetID is None and the Target's slugs empty for a Unit with no Target. " +
						"The function is called once per group. 'group' is the group's key; the 'json' and 're' modules and the 'params' dict are available.",
					DataType: api.DataTypeStarlark,
					Example: `def reduce(rows):
  counts = {}
  for row in rows:
    space = row["Origin"]["SpaceSlug"]
    counts[space] = counts.get(space, 0) + 1
  return [{"Path": space, "Value": n} for space, n in counts.items()]`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the Starlark program as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs:   true,
			InputType: api.DataTypeAttributeValueList,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "summary",
				Description: "What the Starlark program's reduce function returned for the group",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:     false,
			Hermetic:     true,
			Idempotent:   true,
			Description:  "Summarizes the rows of one group of a reduce with a Starlark program that defines a reduce(rows) function returning an AttributeValueList. Called once per group.",
			FunctionType: api.FunctionTypeCustom,
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			output, err := genericFnReduceStarlark(fArgs.Rows, fArgs.GroupKey, fArgs.Arguments)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			return fArgs.ParsedData, output, nil
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnReduceStarlark(rows api.RowList, groupKey string, args []api.FunctionArgument) (api.AttributeValueList, error) {
	program := args[0].Value.(string)
	params, err := parseParams(args, 1)
	if err != nil {
		return nil, err
	}

	globals, err := compileStarlarkProgram("reduce-starlark", program, params, nil,
		starlark.StringDict{"group": starlark.String(groupKey)})
	if err != nil {
		return nil, err
	}
	reduceFn, ok := globals["reduce"]
	if !ok {
		return nil, errors.New("starlark program must define a 'reduce' function")
	}
	callable, ok := reduceFn.(starlark.Callable)
	if !ok {
		return nil, errors.New("'reduce' must be a callable function")
	}

	rowsValue, err := rowsToStarlark(rows)
	if err != nil {
		return nil, err
	}
	thread := &starlark.Thread{Name: "reduce-starlark-reduce"}
	result, err := starlark.Call(thread, callable, starlark.Tuple{rowsValue}, nil)
	if err != nil {
		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			return nil, fmt.Errorf("starlark error: %s", evalErr.Backtrace())
		}
		return nil, fmt.Errorf("starlark error: %w", err)
	}
	return parseAttributeValueList("what reduce returns", result)
}

// rowsToStarlark converts rows to a Starlark list of dicts with the rows' JSON field names, as a
// caller of the API sees them. Whole numbers stay integers, so a count read from a row is not
// turned into a float.
func rowsToStarlark(rows api.RowList) (starlark.Value, error) {
	encoded, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the rows: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("failed to decode the rows: %w", err)
	}
	return goToStarlark(integersFromJSONNumbers(decoded))
}

// integersFromJSONNumbers replaces the json.Numbers a decoder using UseNumber produces with int64
// where they are whole, and float64 otherwise.
func integersFromJSONNumbers(v any) any {
	switch val := v.(type) {
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i
		}
		f, _ := val.Float64()
		return f
	case []any:
		for i := range val {
			val[i] = integersFromJSONNumbers(val[i])
		}
		return val
	case map[string]any:
		for k := range val {
			val[k] = integersFromJSONNumbers(val[k])
		}
		return val
	}
	return v
}
