// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yqkit

import (
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	yqlogger "gopkg.in/op/go-logging.v1"
)

func init() {
	yqlogger.SetLevel(yqlogger.WARNING, "yq-lib")
}

// EvalYQExpression evaluates a yq expression against a YAML string and returns the result.
func EvalYQExpression(expr string, yamlString string) (string, error) {
	encoder := yqlib.NewYamlEncoder(yqlib.ConfiguredYamlPreferences)
	decoder := yqlib.NewYamlDecoder(yqlib.ConfiguredYamlPreferences)
	result, err := yqlib.NewStringEvaluator().EvaluateAll(expr, yamlString, encoder, decoder)
	if err != nil {
		return "", err
	}
	return result, nil
}
