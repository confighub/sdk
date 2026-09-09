// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package changeworkflow

import (
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/google/cel-go/cel"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// CELPrerequisitePrefix marks a prerequisite as an expression its author wrote
// rather than one of the gates the system knows how to compute. A prerequisite
// is one or the other, and everything past the prefix is the expression.
const CELPrerequisitePrefix = "cel:"

// What an expression binds. Every name is capitalized because the entities reach
// CEL as the JSON the API returns, whose fields are capitalized too, so an
// expression spells a field the way every other surface spells it:
// Space.Annotations, not space.annotations.
const (
	celSpaceVariable       = "Space"
	celChangeOrderVariable = "ChangeOrder"
	celReleaseVariable     = "Release"
)

// CELPrerequisiteExpression returns the expression a prerequisite carries and
// whether it carries one, so a caller can tell a name it does not recognize from
// an expression it should evaluate.
func CELPrerequisiteExpression(prerequisite string) (string, bool) {
	return strings.CutPrefix(prerequisite, CELPrerequisitePrefix)
}

// ValidateCELPrerequisite reports whether an expression can run at all. Authoring
// asks before storing a definition, so an expression that could never answer is
// refused while it is a line being typed rather than a gate holding a rollout.
func ValidateCELPrerequisite(expression string) error {
	_, err := celPrerequisiteProgram(expression)
	return err
}

// EvaluateCELPrerequisite reports whether space satisfies expression.
//
// The entities are bound as the JSON the API returns rather than as Go values,
// which is what puts Annotations within reach: an expression reads whatever has
// been written there, so anything outside ConfigHub that can annotate a Space or
// a Release can gate a promotion without the named vocabulary growing a case for
// it.
//
// A nil release is one there is none of -- the Space has published no Release of
// the change being gated -- and binds as null, so an expression that reads it
// fails rather than answering from some other Release. One that does not read it
// is unaffected.
//
// An expression that does not produce a bool is an error rather than a false: a
// gate that could not answer must not read as a gate that answered no.
func EvaluateCELPrerequisite(expression string, space *goclientnew.Space,
	changeOrder *goclientnew.ChangeOrder, release *goclientnew.Release) (bool, error) {
	program, err := celPrerequisiteProgram(expression)
	if err != nil {
		return false, err
	}

	spaceValue, err := celEntityValue(space)
	if err != nil {
		return false, errors.Wrap(err, "encode Space for prerequisite expression")
	}
	changeOrderValue, err := celEntityValue(changeOrder)
	if err != nil {
		return false, errors.Wrap(err, "encode ChangeOrder for prerequisite expression")
	}
	releaseValue, err := celEntityValue(release)
	if err != nil {
		return false, errors.Wrap(err, "encode Release for prerequisite expression")
	}

	value, _, err := program.Eval(map[string]any{
		celSpaceVariable:       spaceValue,
		celChangeOrderVariable: changeOrderValue,
		celReleaseVariable:     releaseValue,
	})
	if err != nil {
		return false, errors.Wrapf(err, "evaluate prerequisite expression %q", expression)
	}

	satisfied, ok := value.Value().(bool)
	if !ok {
		return false, errors.Newf("prerequisite expression %q evaluated to %v, not a bool", expression, value.Value())
	}
	return satisfied, nil
}

func celPrerequisiteProgram(expression string) (cel.Program, error) {
	env, err := cel.NewEnv(
		cel.Variable(celSpaceVariable, cel.DynType),
		cel.Variable(celChangeOrderVariable, cel.DynType),
		cel.Variable(celReleaseVariable, cel.DynType),
	)
	if err != nil {
		return nil, errors.Wrap(err, "create CEL environment")
	}
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, errors.Wrapf(issues.Err(), "compile prerequisite expression %q", expression)
	}
	// The entities are bound as dyn, so an expression reading their fields is
	// typed dyn as often as bool. Only one that is certainly something else -- a
	// string, a number -- can be refused before it runs.
	if output := ast.OutputType(); !output.IsExactType(cel.BoolType) && !output.IsExactType(cel.DynType) {
		return nil, errors.Newf("prerequisite expression %q is not a predicate: it evaluates to %s, not a bool", expression, output)
	}
	program, err := env.Program(ast)
	if err != nil {
		return nil, errors.Wrapf(err, "build program for prerequisite expression %q", expression)
	}
	return program, nil
}

// celEntityValue is the entity as CEL sees it: the JSON the API returns, decoded
// into plain maps, so a field named in an expression is the field the API names.
func celEntityValue(entity any) (any, error) {
	encoded, err := json.Marshal(entity)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}
