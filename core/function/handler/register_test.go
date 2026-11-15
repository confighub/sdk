// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/confighub/sdk/core/function/api"
)

func registrationFor(name string, validating bool, outputType api.OutputType, include ...api.FunctionContextPart) *FunctionRegistration {
	registration := &FunctionRegistration{FunctionSignature: api.FunctionSignature{
		FunctionName: name,
		Validating:   validating,
		Include:      include,
	}}
	if outputType != "" {
		registration.OutputInfo = &api.FunctionOutput{OutputType: outputType}
	}
	return registration
}

func TestRegisterFunctionValidating(t *testing.T) {
	fh := &FunctionHandler{functionMap: map[string]*FunctionRegistration{}}

	// A function returning a ValidationResult is validating only if it says so.
	filter := registrationFor("a-filter", false, api.OutputTypeValidationResult)
	require.NoError(t, fh.RegisterFunction(filter.FunctionName, filter))
	assert.False(t, fh.functionMap["a-filter"].Validating)

	validator := registrationFor("a-validator", true, api.OutputTypeValidationResult)
	require.NoError(t, fh.RegisterFunction(validator.FunctionName, validator))
	assert.True(t, fh.functionMap["a-validator"].Validating)

	// A validating function has to return a ValidationResult, and saying none is an error, not a panic.
	assert.ErrorContains(t, fh.RegisterFunction("wrong-output",
		registrationFor("wrong-output", true, api.OutputTypeAttributeValueList)), "AttributeValueList")
	assert.ErrorContains(t, fh.RegisterFunction("no-output", registrationFor("no-output", true, "")), "none")
}

func TestRegisterFunctionInclude(t *testing.T) {
	fh := &FunctionHandler{functionMap: map[string]*FunctionRegistration{}}

	conflicts := registrationFor("reads-conflicts", true, api.OutputTypeValidationResult, api.FunctionContextConflicts)
	require.NoError(t, fh.RegisterFunction(conflicts.FunctionName, conflicts))
	assert.True(t, fh.functionMap["reads-conflicts"].Includes(api.FunctionContextConflicts))
	assert.False(t, fh.functionMap["reads-conflicts"].Includes(api.FunctionContextLinkFindings))

	assert.ErrorContains(t, fh.RegisterFunction("reads-nonsense",
		registrationFor("reads-nonsense", true, api.OutputTypeValidationResult, "Nonsense")), "Nonsense")
}
