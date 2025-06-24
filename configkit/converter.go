// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package configkit

import (
	"github.com/confighub/sdk/function/api"
)

type ConfigConverter interface {
	NativeToYAML(data []byte) ([]byte, error)
	YAMLToNative(yamlData []byte) ([]byte, error)
	DataType() api.DataType
}
