// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package api implements the data types and messages exchanged by the ConfigHub
// function executor and its clients, in Go.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"strings"

	"github.com/confighub/sdk/core/workerapi"

	jsonschema "github.com/swaggest/jsonschema-go"
)

// RevisionHash represents a crc32.ChecksumIEEE of configuration data.
// Deprecated: Use DataHash (SHA256) instead for new code.
// In Go, conversion of uint32 to int32 doesn't lose information. The
// 32 bits are retained. We use int32 because a number of languages and
// systems don't handle unsigned integers.
type RevisionHash int32

func HashConfigData(data []byte) RevisionHash {
	//nolint:gosec // negative numbers are fine, they just need to be unique
	return RevisionHash(crc32.ChecksumIEEE(data))
}

// DataHash represents a SHA256 hash of configuration data, encoded as a hexadecimal string.
// This is the same hash algorithm used by git and container images.
type DataHash string

// HashConfigDataSHA256 computes the SHA256 hash of configuration data and returns it as a hex string.
func HashConfigDataSHA256(data []byte) DataHash {
	hash := sha256.Sum256(data)
	return DataHash(hex.EncodeToString(hash[:]))
}

// EmptyDataHash is the SHA256 hash of an empty byte slice.
var EmptyDataHash = HashConfigDataSHA256(nil)

// IsEmptyDataHash returns true if the given hash represents empty configuration data.
func IsEmptyDataHash(hash DataHash) bool {
	return hash == "" || hash == EmptyDataHash
}

// ConfigDataHasChanged returns true if the given data differs from the previous data
// identified by the hashes in the FunctionContext. It prefers PreviousDataHash (SHA256)
// when available, falling back to PreviousContentHash (CRC32) for legacy units.
func ConfigDataHasChanged(functionContext *FunctionContext, data []byte) bool {
	if functionContext.PreviousDataHash != "" {
		return HashConfigDataSHA256(data) != functionContext.PreviousDataHash
	}
	return HashConfigData(data) != functionContext.PreviousContentHash
}

// The worker API ToolchainType identifies the configuration serialization format
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
	workerapi.ToolchainOpenTofuHCL:         "/opentofu",
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
	workerapi.ToolchainOpenTofuHCL:         DataTypeHCL,
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
	FunctionName          string              `description:"Name of the function in kabob-case"`
	ToolchainType         workerapi.ToolchainType `json:",omitempty" swaggertype:"string" description:"Toolchain under which the function is registered"`
	Parameters            []FunctionParameter `description:"Function parameters, in order"`
	RequiredParameters    int                 `description:"Number of required parameters"`
	VarArgs               bool                `description:"Last parameter may be repeated"`
	OtherDataExpected     []OtherDataSource   `json:",omitempty" description:"If non-empty, specification of what source(s) are expected in OtherData; if empty, OtherData is not used"`
	OutputInfo            *FunctionOutput     `description:"Output description"`
	Mutating              bool                `description:"May change the configuration data"`
	Validating            bool                `description:"Returns ValidationResult"`
	Hermetic              bool                `description:"Does not call other systems"`
	Idempotent            bool                `description:"Will return the same result if invoked again"`
	Description           string              `description:"Description of the function"`
	FunctionType          FunctionType        `swaggertype:"string" description:"Implementation pattern of the function: PathVisitor or Custom"`
	AttributeName         AttributeName       `json:",omitempty" swaggertype:"string" description:"Attribute corresponding to registered paths, if a path visitor; optional"`
	AffectedResourceTypes []ResourceType      `json:",omitempty" description:"Resource types the function applies to; * if all"`
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
