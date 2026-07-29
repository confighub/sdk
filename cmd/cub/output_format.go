// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Thin aliases to keep call sites readable and to allow test substitution.
var (
	osMkdirAll   = os.MkdirAll
	osWriteFile  = os.WriteFile
	filepathDir  = filepath.Dir
)

// OutputKind identifies a rendering of a response payload.
// Introduced as part of the `-o/--output` flag that replaces the several
// mutually-exclusive boolean/string flags (--json, --yaml, --jq, --yq, --names,
// --columns, --display-mutations). See docs/dev/cli.md and cub-overview.md.
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
	OutputMutations
)

// OutputSpec is the parsed form of the --output flag value. Arg is populated
// only for formats that take an expression or spec (jq, yq, custom-columns).
type OutputSpec struct {
	Kind OutputKind
	Arg  string
}

// outputFormat is the raw value of --output/-o; parsed by effectiveOutput.
var outputFormat string

// parseOutputFormat parses the --output flag value.
// Accepted forms: "json", "yaml", "name", "wide", "mutations",
// "jq=<expr>", "yq=<expr>", "custom-columns=<spec>".
func parseOutputFormat(s string) (OutputSpec, error) {
	if s == "" {
		return OutputSpec{Kind: OutputDefault}, nil
	}

	kindStr, arg, hasArg := strings.Cut(s, "=")
	kindStr = strings.TrimSpace(kindStr)

	switch kindStr {
	case "json":
		if hasArg {
			return OutputSpec{}, fmt.Errorf("--output=%q does not accept an argument", s)
		}
		return OutputSpec{Kind: OutputJSON}, nil
	case "yaml":
		if hasArg {
			return OutputSpec{}, fmt.Errorf("--output=%q does not accept an argument", s)
		}
		return OutputSpec{Kind: OutputYAML}, nil
	case "name":
		if hasArg {
			return OutputSpec{}, fmt.Errorf("--output=%q does not accept an argument", s)
		}
		return OutputSpec{Kind: OutputName}, nil
	case "wide":
		if hasArg {
			return OutputSpec{}, fmt.Errorf("--output=%q does not accept an argument", s)
		}
		return OutputSpec{Kind: OutputWide}, nil
	case "mutations":
		if hasArg {
			return OutputSpec{}, fmt.Errorf("--output=%q does not accept an argument", s)
		}
		return OutputSpec{Kind: OutputMutations}, nil
	case "jq":
		if !hasArg {
			return OutputSpec{}, fmt.Errorf("--output=jq requires an expression: use --output=jq=<expr>")
		}
		return OutputSpec{Kind: OutputJQ, Arg: arg}, nil
	case "yq":
		if !hasArg {
			return OutputSpec{}, fmt.Errorf("--output=yq requires an expression: use --output=yq=<expr>")
		}
		return OutputSpec{Kind: OutputYQ, Arg: arg}, nil
	case "custom-columns":
		if !hasArg {
			return OutputSpec{}, fmt.Errorf("--output=custom-columns requires a spec: use --output=custom-columns=<spec>")
		}
		return OutputSpec{Kind: OutputCustomColumns, Arg: arg}, nil
	default:
		return OutputSpec{}, fmt.Errorf("unknown --output value %q; valid: json, yaml, name, wide, mutations, jq=<expr>, yq=<expr>, custom-columns=<spec>", s)
	}
}

// effectiveOutput returns the OutputSpec that should drive display.
// If --output is set, it wins. Otherwise, the deprecated flags
// (--json, --yaml, --jq, --yq, --names) are consulted in that order.
func effectiveOutput() OutputSpec {
	if outputFormat != "" {
		spec, err := parseOutputFormat(outputFormat)
		failOnError(err)
		return spec
	}
	if jsonOutput {
		return OutputSpec{Kind: OutputJSON}
	}
	if yamlOutput {
		return OutputSpec{Kind: OutputYAML}
	}
	if jq != "" {
		return OutputSpec{Kind: OutputJQ, Arg: jq}
	}
	if yq != "" {
		return OutputSpec{Kind: OutputYQ, Arg: yq}
	}
	if names {
		return OutputSpec{Kind: OutputName}
	}
	return OutputSpec{Kind: OutputDefault}
}

// isAlternativeOutput returns true if the effective output is anything
// other than the default human view. Used to suppress default output when
// an alternative format is selected.
//
// OutputWide and OutputCustomColumns are column-selection modifiers that
// still render through the default table path, so they don't suppress it.
func isAlternativeOutput() bool {
	k := effectiveOutput().Kind
	return k != OutputDefault && k != OutputWide && k != OutputCustomColumns
}

// isSerializedOutput returns true if the effective output serializes the whole
// entity (json, yaml) or lets the user address arbitrary fields of it (jq, yq).
// These formats aren't limited to the displayed columns, so field auto-selection
// should be skipped and all fields requested from the API.
func isSerializedOutput() bool {
	switch effectiveOutput().Kind {
	case OutputJSON, OutputYAML, OutputJQ, OutputYQ:
		return true
	default:
		return false
	}
}

// prefixedSlug returns "<spaceSlug>/<slug>" when spaceSlug is non-empty,
// else just slug. Used by -o name for space-resident entities.
func prefixedSlug(spaceSlug, slug string) string {
	if spaceSlug == "" {
		return slug
	}
	return spaceSlug + "/" + slug
}

// outputFile is the raw value of -O/--output-file. When non-empty, raw-bytes
// sub-payloads (--show data on function commands; the blob-specific
// subcommands) write to a file whose path is derived by substituting
// {space}, {unit}, and {section} placeholders.
var outputFile string

// enableOutputFileFlag registers -O/--output-file on a command.
// The flag accepts either a literal path or a template containing any of
// {space}, {unit}, {section}. Directory components are created as needed.
func enableOutputFileFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&outputFile, "output-file", "O", "",
		"Write payload to FILE. Accepts {space}, {unit}, {section} placeholders.")
}

// expandOutputPath substitutes placeholders in a path template.
func expandOutputPath(tmpl, space, unit, section string) string {
	p := tmpl
	p = strings.ReplaceAll(p, "{space}", space)
	p = strings.ReplaceAll(p, "{unit}", unit)
	p = strings.ReplaceAll(p, "{section}", section)
	return p
}

// writeOrPrint writes data to outputFile (after template expansion) when set,
// or prints it to stdout. Creates parent directories as needed.
func writeOrPrint(data []byte, space, unit, section string) error {
	if outputFile == "" {
		tprintRaw(string(data))
		return nil
	}
	path := expandOutputPath(outputFile, space, unit, section)
	if err := osMkdirAll(filepathDir(path), 0o755); err != nil {
		return err
	}
	if err := osWriteFile(path, data, 0o644); err != nil {
		return err
	}
	if !quiet {
		tprint("Wrote %s", path)
	}
	return nil
}

// effectiveColumns resolves the columns spec for list commands that support
// dynamic columns. It prefers an explicit --columns flag value, falling back
// to parsing -o custom-columns=<spec> when present.
//
// The custom-columns spec format mirrors --columns: a comma-separated list of
// field names (e.g. "Slug,Labels.Environment"). kubectl's custom-columns
// accepts "HEADER:.jsonpath"; that variant is not yet supported here.
func effectiveColumns() []string {
	if len(columns) > 0 {
		return columns
	}
	spec := effectiveOutput()
	if spec.Kind == OutputCustomColumns {
		parts := strings.Split(spec.Arg, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}
