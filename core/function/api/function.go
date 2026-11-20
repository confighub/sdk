// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package api implements the data types and messages exchanged by the ConfigHub
// function executor and its clients, in Go.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/workerapi"

	jsonschema "github.com/swaggest/jsonschema-go"
)

// DataHash represents a SHA256 hash of configuration data, encoded as a hexadecimal string.
// This is the same hash algorithm used by git and container images.
type DataHash string

// HashConfigDataSHA256 computes the SHA256 hash of configuration data and returns it as a hex string.
func HashConfigDataSHA256(data string) DataHash {
	hash := sha256.Sum256([]byte(data))
	return DataHash(hex.EncodeToString(hash[:]))
}

// EmptyDataHash is the SHA256 hash of empty configuration data.
var EmptyDataHash = HashConfigDataSHA256("")

// IsEmptyDataHash returns true if the given hash represents empty configuration data.
// Every stored row carries a real hash, so the empty string is not a value this needs
// to accept: a CHECK constraint on units.data_hash and revisions.data_hash forbids it.
func IsEmptyDataHash(hash DataHash) bool {
	return hash == EmptyDataHash
}

// ToolchainType identifies the configuration serialization format
// (YAML, TOML, HCL, etc.) and family of configuration entities and their schemas.
// Kubernetes is ToolchainKubernetesYAML.

var SupportedToolchains = map[workerapi.ToolchainType]string{
	workerapi.ToolchainConfigHubYAML:       "/confighub",
	workerapi.ToolchainKubernetesYAML:      "/kubernetes",
	workerapi.ToolchainAppConfigProperties: "/properties",
	workerapi.ToolchainAppConfigYAML:       "/yaml",
	workerapi.ToolchainAppConfigTOML:       "/toml",
	workerapi.ToolchainAppConfigINI:        "/ini",
	workerapi.ToolchainAppConfigJSON:       "/json",
	workerapi.ToolchainAppConfigEnv:        "/env",
	workerapi.ToolchainAppConfigText:       "/text",
}

// ToolchainTypeToDataType maps each supported ToolchainType to the DataType
// of its serialization format.
var ToolchainTypeToDataType = map[workerapi.ToolchainType]DataType{
	workerapi.ToolchainConfigHubYAML:       DataTypeYAML,
	workerapi.ToolchainKubernetesYAML:      DataTypeYAML,
	workerapi.ToolchainAppConfigProperties: DataTypeProperties,
	workerapi.ToolchainAppConfigYAML:       DataTypeYAML,
	workerapi.ToolchainAppConfigTOML:       DataTypeTOML,
	workerapi.ToolchainAppConfigINI:        DataTypeINI,
	workerapi.ToolchainAppConfigJSON:       DataTypeJSON,
	workerapi.ToolchainAppConfigEnv:        DataTypeEnv,
	workerapi.ToolchainAppConfigText:       DataTypeText,
}

func IsSupportedToolchain(toolchain workerapi.ToolchainType) bool {
	_, supported := SupportedToolchains[toolchain]
	return supported
}

func GetToolchainPath(toolchain workerapi.ToolchainType) string {
	return SupportedToolchains[toolchain]
}

func SupportedToolchainsToString() string {
	s := ""
	for toolchain := range SupportedToolchains {
		s += ", " + string(toolchain)
	}
	return strings.TrimPrefix(s, ", ")
}

// FunctionSignature specifies the parameter names and values, required and optional parameters,
// OutputType, kind of function (mutating/readonly or validating), and description of the function.
type FunctionSignature struct {
	FunctionName       string                  `description:"Name of the function in kabob-case"`
	ToolchainType      workerapi.ToolchainType `json:",omitempty" swaggertype:"string" description:"Toolchain under which the function is registered"`
	Parameters         []FunctionParameter     `description:"Function parameters, in order"`
	RequiredParameters int                     `description:"Number of required parameters"`
	VarArgs            bool                    `description:"Last parameter may be repeated"`
	OtherDataExpected  []OtherDataSource       `json:",omitempty" description:"The keys of the OtherData the function reads, if any: a revision specifier such as LastReleasedRevisionNum, which a caller that names no revision is passed, or a key such as schema under which the caller passes another Unit. Letters, digits, '-' and '_'."`
	OtherDataAnyKey    bool                    `json:",omitempty" description:"The function reads OtherData under any key its caller passes, as a general-purpose function such as vet-cel does, whose expression names the keys: such as the Units a Trigger's OtherUnitSelections pass. Besides those in OtherDataExpected"`
	OutputInfo         *FunctionOutput         `description:"Output description"`
	Mutating           bool                    `description:"May change the configuration data"`
	// Validating says the function returns a ValidationResult judging the resources its
	// WhereResource selects, so it passes when it selects none, unless it declares in Include
	// that it judges something else. A Trigger running it is not invoked on a Unit with nothing
	// selected. A function that returns a ValidationResult answering some other question, as
	// where-filter does, is not validating.
	Validating bool `description:"Returns a ValidationResult judging the resources selected, passing when none is selected unless it judges something it declares in Include"`
	Hermetic   bool `description:"Does not call other systems"`
	Idempotent bool `description:"Will return the same result if invoked again"`
	// Replayable says this function means the same thing run against a different Unit, so a
	// merge may re-execute a stored invocation of it against a downstream variant instead of
	// transporting the patch it produced. It is a claim about generalizing: the function
	// selects what it changes by identity rather than position, and its arguments name what to
	// change rather than carrying one Unit's data.
	//
	// Declared rather than inferred. No other flag here is a reliable proxy, and a function
	// nobody has considered should not be replayed by default. Idempotent in particular is a
	// different question -- that is about applying an operation twice to the same data, while
	// replay applies it once to different data. See docs/design/function-replayability.md.
	Replayable            bool           `json:",omitempty" description:"Means the same thing run against a different Unit, so a merge may re-execute a stored invocation of it against a downstream variant"`
	Description           string         `description:"Description of the function"`
	FunctionType          FunctionType   `swaggertype:"string" description:"Implementation pattern of the function: PathVisitor or Custom"`
	AttributeName         AttributeName  `json:",omitempty" swaggertype:"string" description:"Attribute corresponding to registered paths, if a path visitor; optional"`
	AffectedResourceTypes []ResourceType `json:",omitempty" description:"Resource types the function applies to; * if all"`
	// Include names the parts of the FunctionContext the function reads that are passed only
	// to the functions that declare them.
	Include []FunctionContextPart `json:",omitempty" description:"Parts of the FunctionContext passed only to the functions that read them: Conflicts, the Unit's outstanding merge conflicts, and LinkFindings, what the resolves of its Links could not do"`
}

// FunctionContextPart names a part of the FunctionContext that is passed only to the functions
// whose signatures Include it, because it can be large in a way the rest of the context is not,
// and few functions read it.
type FunctionContextPart string

const (
	FunctionContextConflicts    FunctionContextPart = "Conflicts"
	FunctionContextLinkFindings FunctionContextPart = "LinkFindings"
)

// Includes reports whether the function reads part of the FunctionContext.
func (fs *FunctionSignature) Includes(part FunctionContextPart) bool {
	return slices.Contains(fs.Include, part)
}

// ValidateFunctionContextPart checks that part names a part of the FunctionContext a signature
// can Include.
func ValidateFunctionContextPart(part FunctionContextPart) error {
	switch part {
	case FunctionContextConflicts, FunctionContextLinkFindings:
		return nil
	}
	return fmt.Errorf("%q is not a part of the FunctionContext a function can include: %s or %s",
		part, FunctionContextConflicts, FunctionContextLinkFindings)
}

// FunctionParameter organizing metadata
// NOTE: I am aware of the similarity to OpenAPI and JSONSchema.

// FunctionParameter specifies the parameter name, description, required vs optional, data type, and example.
type FunctionParameter struct {
	ParameterName string   `description:"Name of the parameter in kabob-case"`
	Description   string   `description:"Description of the parameter"`
	Required      bool     `description:"Whether the parameter is required"`
	DataType      DataType `swaggertype:"string" description:"Data type of the parameter"`
	Example       string   `json:",omitempty" description:"Example value"`
	// DefaultValue is what the function uses when the argument is omitted, when that is one
	// fixed value. It is written as an argument is passed, as a string whatever DataType is, so
	// an int is "10" and a bool "false"; the handler converts an argument by DataType, and a
	// reader of this converts it the same way. A default that depends on the configuration or
	// its context -- every check, the Kubernetes version of the Unit's Target -- has none here,
	// and Description says it.
	DefaultValue string `json:",omitempty" description:"The value the function uses when the argument is omitted, when that is one fixed value. It is a string whatever DataType is, as an argument is passed: an int is \"10\" and a bool \"false\", to convert by DataType. A default that depends on the configuration or its context is described in Description instead."`
	ValueConstraints
}

// ValueConstraints specifies constraints on a parameter's value.
type ValueConstraints struct {
	Regexp     string             `json:",omitempty" description:"Regular expression matching valid values; applies to string parameters"`
	Min        *int               `json:",omitempty" description:"Minimum allowed value; applies to int parameters"`
	Max        *int               `json:",omitempty" description:"Maximum allowed value; applies to int parameters"`
	EnumValues []string           `json:",omitempty" description:"List of valid enum values; applies to enum parameters"`
	Schema     *jsonschema.Schema `json:",omitempty" description:"JSON schema (for embedded JSON values)"`
}

// FunctionOutput specifies the name and description of the result and its OutputType.
type FunctionOutput struct {
	ResultName  string             `description:"Name of the result in kabob-case"`
	Description string             `description:"Description of the result"`
	OutputType  OutputType         `swaggertype:"string" description:"Data type of the JSON embedded in the output"`
	Schema      *jsonschema.Schema `json:",omitempty" description:"JSON schema of the output type"`
}
