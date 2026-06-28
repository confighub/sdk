// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package cliutil provides output rendering and cobra flag bundles for building
// ConfigHub CLIs and `cub` plugins with a consistent UX. It depends only on
// cobra, gojq, and yqkit — not on the ConfigHub API client — so API-only tools
// don't pay for it and UX-only code doesn't drag in the client. Combine it with
// github.com/confighub/sdk/core/cubapi to build a full tool.
//
// It has no global state: every function takes its inputs (an io.Writer, an
// [OutputSpec], a *cobra.Command) explicitly.
package cliutil

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/confighub/sdk/configkit/yqkit"
	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"
)

// OutputKind enumerates the supported -o/--output formats.
type OutputKind int

const (
	OutputDefault OutputKind = iota
	OutputJSON
	OutputYAML
	OutputName
	OutputJQ
	OutputYQ
	OutputWide
	OutputCustomColumns
	OutputTable
)

// OutputSpec is the parsed form of an --output value. Arg is populated only for
// formats that take an expression or spec (jq, yq, custom-columns).
type OutputSpec struct {
	Kind OutputKind
	Arg  string
}

// ParseOutput parses an --output/-o value into an OutputSpec. Supported values:
// "" (default), json, yaml, name, wide, table, jq=<expr>, yq=<expr>,
// custom-columns=<spec>.
func ParseOutput(s string) (OutputSpec, error) {
	if s == "" {
		return OutputSpec{Kind: OutputDefault}, nil
	}
	kindStr, arg, hasArg := strings.Cut(s, "=")
	kindStr = strings.TrimSpace(kindStr)

	noArg := func(k OutputKind) (OutputSpec, error) {
		if hasArg {
			return OutputSpec{}, fmt.Errorf("--output=%q does not accept an argument", s)
		}
		return OutputSpec{Kind: k}, nil
	}
	withArg := func(k OutputKind, usage string) (OutputSpec, error) {
		if !hasArg {
			return OutputSpec{}, fmt.Errorf("--output=%s requires %s", kindStr, usage)
		}
		return OutputSpec{Kind: k, Arg: arg}, nil
	}

	switch kindStr {
	case "json":
		return noArg(OutputJSON)
	case "yaml":
		return noArg(OutputYAML)
	case "name":
		return noArg(OutputName)
	case "wide":
		return noArg(OutputWide)
	case "table":
		return noArg(OutputTable)
	case "jq":
		return withArg(OutputJQ, "an expression: use --output=jq=<expr>")
	case "yq":
		return withArg(OutputYQ, "an expression: use --output=yq=<expr>")
	case "custom-columns":
		return withArg(OutputCustomColumns, "a spec: use --output=custom-columns=<spec>")
	default:
		return OutputSpec{}, fmt.Errorf("unknown --output value %q; valid: json, yaml, name, wide, table, jq=<expr>, yq=<expr>, custom-columns=<spec>", s)
	}
}

// Render writes v to w in the spec's format. It returns handled=true for the
// formats it renders directly (json, yaml, jq, yq); for table/name/wide/
// custom-columns/default it returns false so the caller can render its own
// (typically tabular) view.
func (s OutputSpec) Render(w io.Writer, v any) (handled bool, err error) {
	switch s.Kind {
	case OutputJSON:
		return true, PrintJSON(w, v)
	case OutputYAML:
		return true, PrintYAML(w, v)
	case OutputJQ:
		return true, RenderJQ(w, v, s.Arg)
	case OutputYQ:
		return true, RenderYQ(w, v, s.Arg)
	default:
		return false, nil
	}
}

// PrintJSON writes v as indented JSON (HTML escaping disabled).
func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// PrintYAML writes v as YAML.
func PrintYAML(w io.Writer, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// RenderJQ evaluates a jq expression against v (marshaled to JSON) and writes
// each result to w — scalars on their own line, structured values as JSON.
func RenderJQ(w io.Writer, v any, expr string) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		return err
	}
	query, err := gojq.Parse(expr)
	if err != nil {
		return fmt.Errorf("parse jq expression: %w", err)
	}
	iter := query.Run(tree)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if e, ok := value.(error); ok {
			if h, ok := e.(*gojq.HaltError); ok && h.Value() == nil {
				break
			}
			return e
		}
		switch vv := value.(type) {
		case string, int, float64, bool, nil:
			if _, err := fmt.Fprintln(w, vv); err != nil {
				return err
			}
		default:
			if err := PrintJSON(w, value); err != nil {
				return err
			}
		}
	}
	return nil
}

// RenderYQ evaluates a yq expression against v (marshaled to YAML) and writes
// the result to w.
func RenderYQ(w io.Writer, v any, expr string) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	out, err := yqkit.EvalYQExpression(expr, string(b))
	if err != nil {
		return fmt.Errorf("evaluate yq expression: %w", err)
	}
	_, err = io.WriteString(w, out)
	return err
}
