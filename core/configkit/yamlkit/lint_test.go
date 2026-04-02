// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func parseLintNode(t *testing.T, input string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return &doc
}

func findingsByRule(findings []LintFinding, rule string) []LintFinding {
	var result []LintFinding
	for _, f := range findings {
		if f.Rule == rule {
			result = append(result, f)
		}
	}
	return result
}

func TestLintNoAnchors(t *testing.T) {
	input := `anchor: &anc
  key: val
alias: *anc
`
	node := parseLintNode(t, input)
	config := LintConfig{NoAnchors: true}
	findings := LintNode(node, config)
	anchorFindings := findingsByRule(findings, "no-anchors")
	if len(anchorFindings) != 2 {
		t.Fatalf("expected 2 anchor findings (1 anchor + 1 alias), got %d: %+v", len(anchorFindings), anchorFindings)
	}
}

func TestLintNoAnchorsClean(t *testing.T) {
	input := `key1: val1
key2: val2
`
	node := parseLintNode(t, input)
	config := LintConfig{NoAnchors: true}
	findings := LintNode(node, config)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestLintNoEmptyValues(t *testing.T) {
	input := `empty:
explicit_null: null
explicit_empty: ""
normal: value
`
	node := parseLintNode(t, input)
	config := LintConfig{NoEmptyValues: true}
	findings := LintNode(node, config)
	emptyFindings := findingsByRule(findings, "no-empty-values")
	if len(emptyFindings) != 1 {
		t.Fatalf("expected 1 empty-value finding (for 'empty:'), got %d: %+v", len(emptyFindings), emptyFindings)
	}
	if emptyFindings[0].Path != "empty" {
		t.Errorf("expected path 'empty', got %q", emptyFindings[0].Path)
	}
}

func TestLintNoDuplicateKeys(t *testing.T) {
	input := `foo: first
bar: value
foo: second
`
	node := parseLintNode(t, input)
	config := LintConfig{NoDuplicateKeys: true}
	findings := LintNode(node, config)
	dupFindings := findingsByRule(findings, "no-duplicate-keys")
	if len(dupFindings) != 1 {
		t.Fatalf("expected 1 duplicate-key finding, got %d: %+v", len(dupFindings), dupFindings)
	}
	if dupFindings[0].Path != "foo" {
		t.Errorf("expected path 'foo', got %q", dupFindings[0].Path)
	}
}

func TestLintNoDuplicateKeysNested(t *testing.T) {
	// Same key at different nesting levels is not a duplicate
	input := `parent1:
  name: a
parent2:
  name: b
`
	node := parseLintNode(t, input)
	config := LintConfig{NoDuplicateKeys: true}
	findings := LintNode(node, config)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for same keys at different nesting, got %d: %+v", len(findings), findings)
	}
}

func TestLintNoTruthy(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"yes", "enabled: yes\n", 1},
		{"no", "enabled: no\n", 1},
		{"on", "enabled: on\n", 1},
		{"off", "enabled: off\n", 1},
		{"y", "enabled: y\n", 1},
		{"n", "enabled: n\n", 1},
		{"YES", "enabled: YES\n", 1},
		{"Yes", "enabled: Yes\n", 1},
		{"ON", "enabled: ON\n", 1},
		{"true_is_ok", "enabled: true\n", 0},
		{"false_is_ok", "enabled: false\n", 0},
		{"quoted_yes", "enabled: \"yes\"\n", 0},
		{"single_quoted_yes", "enabled: 'yes'\n", 0},
		{"normal_string", "name: hello\n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := parseLintNode(t, tt.input)
			config := LintConfig{NoTruthy: true}
			findings := LintNode(node, config)
			truthyFindings := findingsByRule(findings, "no-truthy")
			if len(truthyFindings) != tt.expected {
				t.Errorf("expected %d truthy findings, got %d: %+v", tt.expected, len(truthyFindings), truthyFindings)
			}
		})
	}
}

func TestLintNoOctalValues(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"old_octal_0755", "mode: 0755\n", 1},
		{"old_octal_010", "val: 010\n", 1},
		{"new_octal_0o755", "mode: 0o755\n", 0},
		{"plain_zero", "val: 0\n", 0},
		{"normal_int", "port: 8080\n", 0},
		{"hex", "val: 0xff\n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := parseLintNode(t, tt.input)
			config := LintConfig{NoOctalValues: true}
			findings := LintNode(node, config)
			octalFindings := findingsByRule(findings, "no-octal-values")
			if len(octalFindings) != tt.expected {
				t.Errorf("expected %d octal findings, got %d: %+v", tt.expected, len(octalFindings), octalFindings)
			}
		})
	}
}

func TestLintPaths(t *testing.T) {
	input := `spec:
  containers:
    - name: app
      enabled: yes
      mode: 0755
`
	node := parseLintNode(t, input)
	config := DefaultLintConfig()
	findings := LintNode(node, config)

	pathsByRule := map[string]string{}
	for _, f := range findings {
		pathsByRule[f.Rule] = f.Path
	}

	if p, ok := pathsByRule["no-truthy"]; !ok || p != "spec.containers.0.enabled" {
		t.Errorf("expected truthy finding at 'spec.containers.0.enabled', got %q (present=%v)", p, ok)
	}
	if p, ok := pathsByRule["no-octal-values"]; !ok || p != "spec.containers.0.mode" {
		t.Errorf("expected octal finding at 'spec.containers.0.mode', got %q (present=%v)", p, ok)
	}
}

func TestLintBytes(t *testing.T) {
	input := []byte("dup: first\ndup: second\nenabled: yes\n")
	findings, err := LintBytes(input)
	if err != nil {
		t.Fatalf("LintBytes error: %v", err)
	}
	if len(findings) < 2 {
		t.Fatalf("expected at least 2 findings (dup + truthy), got %d: %+v", len(findings), findings)
	}
}

func TestLintConfigDisableRule(t *testing.T) {
	input := `enabled: yes
mode: 0755
`
	node := parseLintNode(t, input)
	config := DefaultLintConfig()
	config.NoTruthy = false

	findings := LintNode(node, config)
	truthyFindings := findingsByRule(findings, "no-truthy")
	octalFindings := findingsByRule(findings, "no-octal-values")
	if len(truthyFindings) != 0 {
		t.Errorf("expected 0 truthy findings with rule disabled, got %d", len(truthyFindings))
	}
	if len(octalFindings) != 1 {
		t.Errorf("expected 1 octal finding, got %d", len(octalFindings))
	}
}

func TestLintLineNumbers(t *testing.T) {
	input := `first: value
second: yes
third: value
`
	node := parseLintNode(t, input)
	config := LintConfig{NoTruthy: true}
	findings := LintNode(node, config)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Line != 2 {
		t.Errorf("expected line 2, got %d", findings[0].Line)
	}
}

func TestLintAllRulesCombined(t *testing.T) {
	input := `anchor: &anc
  key: val
alias: *anc
dup: first
dup: second
empty:
truthy: yes
octal: 0755
`
	node := parseLintNode(t, input)
	config := DefaultLintConfig()
	findings := LintNode(node, config)

	rules := map[string]int{}
	for _, f := range findings {
		rules[f.Rule]++
	}
	if rules["no-anchors"] < 2 {
		t.Errorf("expected at least 2 anchor findings, got %d", rules["no-anchors"])
	}
	if rules["no-duplicate-keys"] != 1 {
		t.Errorf("expected 1 duplicate-key finding, got %d", rules["no-duplicate-keys"])
	}
	if rules["no-empty-values"] != 1 {
		t.Errorf("expected 1 empty-value finding, got %d", rules["no-empty-values"])
	}
	if rules["no-truthy"] != 1 {
		t.Errorf("expected 1 truthy finding, got %d", rules["no-truthy"])
	}
	if rules["no-octal-values"] != 1 {
		t.Errorf("expected 1 octal finding, got %d", rules["no-octal-values"])
	}
}
