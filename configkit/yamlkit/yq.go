// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	yqlogger "gopkg.in/op/go-logging.v1"
)

func EvalYQExpression(expr string, yamlString string) (string, error) {
	yqlogger.SetLevel(yqlogger.WARNING, "yq-lib")
	encoder := yqlib.NewYamlEncoder(yqlib.ConfiguredYamlPreferences)
	decoder := yqlib.NewYamlDecoder(yqlib.ConfiguredYamlPreferences)
	result, err := yqlib.NewStringEvaluator().EvaluateAll(expr, yamlString, encoder, decoder)
	if err != nil {
		return "", err
	}
	return result, nil
}
