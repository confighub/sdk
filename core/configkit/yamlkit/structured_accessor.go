// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// JSONAccessor is an EmbeddedAccessor that accesses fields within a JSON string
// value embedded in a YAML scalar. The path uses dot-separated segments to navigate
// the parsed JSON structure.
//
// For example, given a YAML field containing the JSON string '{"a":{"b":"hello"}}':
//   - Extract(jsonStr, "a.b") returns "hello"
//   - Replace(jsonStr, "world", "a.b") returns '{"a":{"b":"world"}}'
//
// The config string is not used (pass "" when creating).
type JSONAccessor struct{}

func newJSONAccessor(_ string) (*JSONAccessor, error) {
	return &JSONAccessor{}, nil
}

func (ja *JSONAccessor) ExistsP(scalarYamlDoc *gaby.YamlDoc, path string) bool {
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return false
	}
	doc, err := gaby.ParseJSON([]byte(value))
	if err != nil {
		return false
	}
	return doc.Path(path) != nil
}

func (ja *JSONAccessor) Data(scalarYamlDoc *gaby.YamlDoc, path string) (any, error) {
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return "", EmbeddedPathNotFound
	}
	return ja.Extract(value, path)
}

func (ja *JSONAccessor) Extract(currentFieldValue, path string) (any, error) {
	doc, err := gaby.ParseJSON([]byte(currentFieldValue))
	if err != nil {
		return "", errors.Wrap(err, "failed to parse embedded JSON")
	}
	node := doc.Path(path)
	if node == nil {
		return "", errors.Mark(fmt.Errorf("path %s not found in embedded JSON", path), EmbeddedPathNotFound)
	}
	data := node.Data()
	if data == nil {
		return "", errors.Mark(fmt.Errorf("path %s is null in embedded JSON", path), EmbeddedPathNotFound)
	}
	return data, nil
}

func (ja *JSONAccessor) Replace(currentFieldValue string, value any, path string) (string, error) {
	doc, err := gaby.ParseJSON([]byte(currentFieldValue))
	if err != nil {
		return currentFieldValue, errors.Wrap(err, "failed to parse embedded JSON")
	}
	_, err = doc.SetP(value, path)
	if err != nil {
		return currentFieldValue, errors.Wrap(err, fmt.Sprintf("failed to set path %s in embedded JSON", path))
	}
	out, err := doc.MarshalJSON()
	if err != nil {
		return currentFieldValue, errors.Wrap(err, "failed to marshal embedded JSON")
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (ja *JSONAccessor) SetP(scalarYamlDoc *gaby.YamlDoc, value any, path string) error {
	currentFieldValue, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return errors.Mark(fmt.Errorf("path %s not found", path), EmbeddedPathNotFound)
	}
	newFieldValue, err := ja.Replace(currentFieldValue, value, path)
	if err != nil {
		return err
	}
	if newFieldValue == currentFieldValue {
		return nil
	}
	ynode := scalarYamlDoc.YNode()
	ynode.Value = newFieldValue
	return nil
}

// YAMLAccessor is an EmbeddedAccessor that accesses fields within a YAML string
// value embedded in a YAML scalar. The path uses dot-separated segments to navigate
// the parsed YAML structure.
//
// For example, given a YAML field containing the string "a:\n  b: hello\n":
//   - Extract(yamlStr, "a.b") returns "hello"
//   - Replace(yamlStr, "world", "a.b") returns "a:\n  b: world\n"
//
// The config string is not used (pass "" when creating).
type YAMLAccessor struct{}

func newYAMLAccessor(_ string) (*YAMLAccessor, error) {
	return &YAMLAccessor{}, nil
}

func (ya *YAMLAccessor) ExistsP(scalarYamlDoc *gaby.YamlDoc, path string) bool {
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return false
	}
	doc, err := gaby.ParseYAML([]byte(value))
	if err != nil {
		return false
	}
	return doc.Path(path) != nil
}

func (ya *YAMLAccessor) Data(scalarYamlDoc *gaby.YamlDoc, path string) (any, error) {
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return "", EmbeddedPathNotFound
	}
	return ya.Extract(value, path)
}

func (ya *YAMLAccessor) Extract(currentFieldValue, path string) (any, error) {
	doc, err := gaby.ParseYAML([]byte(currentFieldValue))
	if err != nil {
		return "", errors.Wrap(err, "failed to parse embedded YAML")
	}
	node := doc.Path(path)
	if node == nil {
		return "", errors.Mark(fmt.Errorf("path %s not found in embedded YAML", path), EmbeddedPathNotFound)
	}
	data := node.Data()
	if data == nil {
		return "", errors.Mark(fmt.Errorf("path %s is null in embedded YAML", path), EmbeddedPathNotFound)
	}
	return data, nil
}

func (ya *YAMLAccessor) Replace(currentFieldValue string, value any, path string) (string, error) {
	doc, err := gaby.ParseYAML([]byte(currentFieldValue))
	if err != nil {
		return currentFieldValue, errors.Wrap(err, "failed to parse embedded YAML")
	}
	_, err = doc.SetP(value, path)
	if err != nil {
		return currentFieldValue, errors.Wrap(err, fmt.Sprintf("failed to set path %s in embedded YAML", path))
	}
	out, err := doc.MarshalYAML()
	if err != nil {
		return currentFieldValue, errors.Wrap(err, "failed to marshal embedded YAML")
	}
	return string(out), nil
}

func (ya *YAMLAccessor) SetP(scalarYamlDoc *gaby.YamlDoc, value any, path string) error {
	currentFieldValue, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return errors.Mark(fmt.Errorf("path %s not found", path), EmbeddedPathNotFound)
	}
	newFieldValue, err := ya.Replace(currentFieldValue, value, path)
	if err != nil {
		return err
	}
	if newFieldValue == currentFieldValue {
		return nil
	}
	ynode := scalarYamlDoc.YNode()
	ynode.Value = newFieldValue
	return nil
}
