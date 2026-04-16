// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// TODO: Unify DataType and OutputType.

// DataType represents the data type of a function parameter or configuration attribute.
// The data type can be a scalar type (string, int, bool), a structured format (JSON, YAML),
// or a well known structured data type (e.g., AttributeValueList).
type DataType string

const (
	// Basic scalar types
	DataTypeNone   = DataType("")
	DataTypeString = DataType("string")
	DataTypeInt    = DataType("int")
	DataTypeBool   = DataType("bool")
	DataTypeEnum   = DataType("enum")

	// Additional Storage types
	DataTypeUUID                    = DataType("uuid")
	DataTypeTime                    = DataType("time")
	DataTypeStringArray             = DataType("[]string")
	DataTypeStringMap               = DataType("map[string]string")
	DataTypeStringBoolMap           = DataType("map[string]bool")
	DataTypeUUIDArray               = DataType("[]uuid")
	DataTypeUUIDStringMap           = DataType("map[uuid]string")
	DataTypeStringStringUUIDBoolMap = DataType("map[string]map[string]map[uuid]bool")

	// Structured data types
	DataTypeAttributeValueList   = DataType("AttributeValueList")
	DataTypeAttributeInfoList    = DataType("AttributeInfoList")
	DataTypePatchMap             = DataType("PatchMap")
	DataTypeResourceMutationList = DataType("ResourceMutationList")
	DataTypeResourceList         = DataType("ResourceList")
	DataTypeValueFilter          = DataType("ValueFilter")

	// Configuration format types
	DataTypeJSON       = DataType("JSON")
	DataTypeYAML       = DataType("YAML")
	DataTypeProperties = DataType("Properties")
	DataTypeTOML       = DataType("TOML")
	DataTypeINI        = DataType("INI")
	DataTypeEnv        = DataType("Env")
	DataTypeText       = DataType("Text")
	DataTypeHCL        = DataType("HCL")
	DataTypeCEL        = DataType("CEL")
)

// OutputType represents the type of output produced by a function. It is either a well
// known structured type (e.g., AttributeValueList), a structured format (JSON), or
// Opaque (unparsed).
type OutputType string

const (
	OutputTypeValidationResult     = OutputType("ValidationResult")
	OutputTypeValidationResultList = OutputType("ValidationResultList")
	OutputTypeAttributeValueList   = OutputType("AttributeValueList")
	OutputTypeResourceInfoList     = OutputType("ResourceInfoList")
	OutputTypeResourceList         = OutputType("ResourceList")
	OutputTypePatchMap             = OutputType("PatchMap")
	OutputTypeCustomJSON           = OutputType("JSON")
	OutputTypeYAML                 = OutputType("YAML")
	OutputTypeOpaque               = OutputType("Opaque")
	OutputTypeResourceMutationList = OutputType("ResourceMutationList")
)

const MaxDataTypeLength = 32

type YAMLPayload struct {
	Payload string
}

// All types except int and bool are always serialized as strings.
func DataTypeIsSerializedAsString(dataType DataType) bool {
	switch dataType {
	case DataTypeInt, DataTypeBool:
		return false
	}
	return true
}
