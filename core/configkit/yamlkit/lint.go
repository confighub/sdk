// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// LintFinding represents a single lint rule violation found in a YAML document.
type LintFinding struct {
	Rule    string // Rule identifier, e.g., "no-anchors", "no-truthy"
	Message string // Human-readable description of the violation
	Path    string // Dot-notation path to the offending node, e.g., "spec.containers.0.image"
	Line    int    // 1-based line number from the source YAML
	Column  int    // 1-based column number from the source YAML
}

// LintConfig controls which lint rules are enabled.
type LintConfig struct {
	NoAnchors       bool // Ban anchors (&anchor) and aliases (*alias)
	NoEmptyValues   bool // Ban implicit null values (key: with no value)
	NoDuplicateKeys bool // Ban duplicate keys in mappings
	NoTruthy        bool // Ban unquoted yes/no/on/off/y/n (YAML 1.1 booleans)
	NoOctalValues   bool // Ban old-style octal integers (0755, 010)
}

// DefaultLintConfig returns a LintConfig with all rules enabled.
func DefaultLintConfig() LintConfig {
	return LintConfig{
		NoAnchors:       true,
		NoEmptyValues:   true,
		NoDuplicateKeys: true,
		NoTruthy:        true,
		NoOctalValues:   true,
	}
}

// truthyValues are unquoted string values that YAML 1.1 interprets as booleans
// but YAML 1.2 (used by Go's yaml.v3) treats as strings. These cause
// cross-parser surprises (the "Norway problem").
var truthyValues = map[string]bool{
	"yes": true, "no": true,
	"on": true, "off": true,
	"y": true, "n": true,
}

// LintNode walks a yaml.Node tree and returns all lint findings.
// The node may be a DocumentNode (from yaml.Unmarshal) or a MappingNode/SequenceNode
// (from kyaml's Parse or gaby's YNode).
func LintNode(node *yaml.Node, config LintConfig) []LintFinding {
	var findings []LintFinding
	lintWalk(node, "", config, &findings)
	return findings
}

// LintBytes parses raw YAML bytes into a yaml.Node tree and lints it.
// This is a convenience function for standalone use outside of gaby.
// It handles a single YAML document; for multi-document YAML, use LintNode
// with each document's node separately.
func LintBytes(data []byte) ([]LintFinding, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	return LintNode(&doc, DefaultLintConfig()), nil
}

// lintWalk recursively walks a yaml.Node tree, checking enabled lint rules
// and appending findings. path tracks the current dot-notation location.
func lintWalk(node *yaml.Node, path string, config LintConfig, findings *[]LintFinding) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) > 0 {
			lintWalk(node.Content[0], path, config, findings)
		}

	case yaml.MappingNode:
		lintCheckAnchor(node, path, config, findings)
		seen := map[string]bool{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			keyName := keyNode.Value
			childPath := joinLintPath(path, keyName)

			// Duplicate keys
			if config.NoDuplicateKeys && seen[keyName] {
				*findings = append(*findings, LintFinding{
					Rule:    "no-duplicate-keys",
					Message: fmt.Sprintf("duplicate key %q", keyName),
					Path:    childPath,
					Line:    keyNode.Line,
					Column:  keyNode.Column,
				})
			}
			seen[keyName] = true

			// Empty values: implicit null from "key:" with no value
			if config.NoEmptyValues && valNode.Tag == "!!null" && valNode.Value == "" {
				*findings = append(*findings, LintFinding{
					Rule:    "no-empty-values",
					Message: fmt.Sprintf("empty value for key %q; use an explicit value or \"null\"", keyName),
					Path:    childPath,
					Line:    valNode.Line,
					Column:  valNode.Column,
				})
			}

			// Check scalar rules on the value node directly (before recursing)
			lintCheckScalar(valNode, childPath, config, findings)
			lintWalk(valNode, childPath, config, findings)
		}

	case yaml.SequenceNode:
		lintCheckAnchor(node, path, config, findings)
		for i, elem := range node.Content {
			elemPath := joinLintPath(path, fmt.Sprintf("%d", i))
			lintCheckScalar(elem, elemPath, config, findings)
			lintWalk(elem, elemPath, config, findings)
		}

	case yaml.ScalarNode:
		lintCheckAnchor(node, path, config, findings)
		// Scalar checks are handled by lintCheckScalar called from the parent context

	case yaml.AliasNode:
		if config.NoAnchors {
			*findings = append(*findings, LintFinding{
				Rule:    "no-anchors",
				Message: "alias is not allowed; inline the value instead",
				Path:    path,
				Line:    node.Line,
				Column:  node.Column,
			})
		}
	}
}

// lintCheckAnchor reports if a node has an anchor defined.
func lintCheckAnchor(node *yaml.Node, path string, config LintConfig, findings *[]LintFinding) {
	if config.NoAnchors && node.Anchor != "" {
		*findings = append(*findings, LintFinding{
			Rule:    "no-anchors",
			Message: fmt.Sprintf("anchor %q is not allowed; inline the value instead", node.Anchor),
			Path:    path,
			Line:    node.Line,
			Column:  node.Column,
		})
	}
}

// lintCheckScalar checks truthy and octal rules on a scalar node.
// Called from the parent (mapping or sequence) context so that non-scalar
// nodes are silently skipped.
func lintCheckScalar(node *yaml.Node, path string, config LintConfig, findings *[]LintFinding) {
	if node.Kind != yaml.ScalarNode {
		return
	}

	// Truthy: unquoted string values that YAML 1.1 treats as booleans
	if config.NoTruthy && node.Tag == "!!str" && node.Style == 0 {
		if truthyValues[strings.ToLower(node.Value)] {
			*findings = append(*findings, LintFinding{
				Rule:    "no-truthy",
				Message: fmt.Sprintf("value %q is ambiguous; YAML 1.1 interprets it as a boolean; quote it to be safe", node.Value),
				Path:    path,
				Line:    node.Line,
				Column:  node.Column,
			})
		}
	}

	// Octal: old-style 0-prefixed integers (0755, 010) but not 0o755 or plain 0
	if config.NoOctalValues && node.Tag == "!!int" && isOldStyleOctal(node.Value) {
		*findings = append(*findings, LintFinding{
			Rule:    "no-octal-values",
			Message: fmt.Sprintf("value %q uses old-style octal notation; use 0o%s prefix for clarity or quote it if a string was intended", node.Value, node.Value[1:]),
			Path:    path,
			Line:    node.Line,
			Column:  node.Column,
		})
	}
}

// isOldStyleOctal returns true for integer representations like "0755" or "010"
// (leading zero followed by digits). Returns false for "0", "0o755", "0O755".
func isOldStyleOctal(value string) bool {
	if len(value) < 2 || value[0] != '0' {
		return false
	}
	// 0o or 0O prefix is YAML 1.2 explicit octal — fine
	if value[1] == 'o' || value[1] == 'O' {
		return false
	}
	// 0x or 0X prefix is hex — not octal
	if value[1] == 'x' || value[1] == 'X' {
		return false
	}
	// Remaining: leading 0 followed by at least one digit
	for i := 1; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// joinLintPath builds a dot-notation path from a parent path and a segment.
func joinLintPath(parent, segment string) string {
	if parent == "" {
		return segment
	}
	return parent + "." + segment
}
