// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"strings"
)

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

// ParseStringArrayCSV parses a DataTypeStringArray argument value serialized as
// a comma-separated string into a []string. Whitespace around each element is
// trimmed and empty elements are dropped. A nil value or empty string returns
// a nil slice.
func ParseStringArrayCSV(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	s, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("expected string for %s, got %T", DataTypeStringArray, value)
	}
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, nil
}
