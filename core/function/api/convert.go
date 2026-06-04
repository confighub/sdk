// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"strconv"

	"github.com/cockroachdb/errors"
)

// ConvertStringToDataType coerces a string value to the Go value that
// matches the given DataType. The supported destination types are
// DataTypeString (passes the string through), DataTypeInt (parsed via
// strconv.Atoi), and DataTypeBool (parsed via strconv.ParseBool). Any other
// DataType returns an error so callers can reject unsupported targets at
// runtime.
func ConvertStringToDataType(s string, dataType DataType) (any, error) {
	switch dataType {
	case "", DataTypeString:
		return s, nil
	case DataTypeInt:
		intVal, err := strconv.Atoi(s)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot convert %q to int", s)
		}
		return intVal, nil
	case DataTypeBool:
		boolVal, err := strconv.ParseBool(s)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot convert %q to bool", s)
		}
		return boolVal, nil
	default:
		return nil, fmt.Errorf("cannot convert %q to data type %s", s, dataType)
	}
}
