// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package diffview renders a ConfigDiff -- the server's account of what a change did, or on a
// dry run would do, to a Unit's configuration -- for a person reading a terminal: each resource
// that differs, and under it each changed path with its value on both sides. It is how `cub`
// shows `-o mutations`, and how a tool built on the SDK can show what one of its commands
// changes, without asking its user to run that command's functions by hand to find out.
//
// The diff itself comes from the API: [cubapi.Change] with Diff set returns one per Unit on
// [cubapi.UnitOutcome], and [cubapi.GetUnitDiff] and [cubapi.DiffConfigurations] read one
// between two Revisions or configurations.
//
// [cubapi.Change]: https://pkg.go.dev/github.com/confighub/sdk/core/cubapi#Change
// [cubapi.UnitOutcome]: https://pkg.go.dev/github.com/confighub/sdk/core/cubapi#UnitOutcome
// [cubapi.GetUnitDiff]: https://pkg.go.dev/github.com/confighub/sdk/core/cubapi#GetUnitDiff
// [cubapi.DiffConfigurations]: https://pkg.go.dev/github.com/confighub/sdk/core/cubapi#DiffConfigurations
package diffview

import (
	"fmt"
	"io"
	"os"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Style decides how a diff is drawn.
type Style struct {
	// Color draws additions green, deletions red, resource headers blue and context dim, with
	// ANSI escapes. Without it the layout is the same and the text plain.
	Color bool
}

// ColorFor reports whether output to w should be colored: w is a terminal and NO_COLOR is not
// set (https://no-color.org).
func ColorFor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset     = "\033[0m"
	ansiRed       = "\033[31m"
	ansiGreen     = "\033[32m"
	ansiLightBlue = "\033[94m"
	ansiDim       = "\033[2m"
)

// printer writes one diff in one Style.
type printer struct {
	w     io.Writer
	color bool
}

func (p printer) paint(code, s string) string {
	if !p.color {
		return s
	}
	return code + s + ansiReset
}

// line writes s with exactly one trailing newline and no leading ones.
func (p printer) line(s string) {
	_, _ = fmt.Fprint(p.w, strings.Trim(s, "\n")+"\n")
}

// WriteUnitChanges writes what a change did, or would do, to one Unit: a header naming the Unit,
// and the change's description when there is one, followed by the diff.
func WriteUnitChanges(w io.Writer, unitLabel, changeDescription string, diff *goclientnew.ConfigDiff, style Style) {
	p := printer{w: w, color: style.Color}
	header := "Changes to unit " + unitLabel
	if changeDescription != "" {
		header += " from " + changeDescription
	}
	p.line(header + ":")
	if diff == nil {
		p.line("The server returned no diff")
		return
	}
	p.configDiff(diff)
}

// WriteConfigDiff writes a ConfigDiff: each resource that differs, and under it each changed path
// with its value on both sides, and any unchanged path returned as context with its value.
func WriteConfigDiff(w io.Writer, diff *goclientnew.ConfigDiff, style Style) {
	printer{w: w, color: style.Color}.configDiff(diff)
}

func (p printer) configDiff(diff *goclientnew.ConfigDiff) {
	if diff == nil || len(diff.Resources) == 0 {
		p.line("No changes")
		return
	}
	for i := range diff.Resources {
		if i > 0 {
			p.line("")
		}
		p.resourceDiff(&diff.Resources[i])
	}
}

func (p printer) resourceDiff(resource *goclientnew.ResourceDiff) {
	changeType := goclientnew.None
	if resource.ChangeType != nil {
		changeType = *resource.ChangeType
	}
	header := p.paint(ansiLightBlue, "Resource: "+resourceLabel(resource.Resource))
	switch changeType {
	case goclientnew.Add:
		header += "  " + p.paint(ansiGreen, "(added)")
	case goclientnew.Delete:
		header += "  " + p.paint(ansiRed, "(deleted)")
	case goclientnew.None:
		header += "  (unchanged)"
	case goclientnew.Update, goclientnew.Replace:
		// The changed paths below say what changed.
	}
	if resource.PreviousResource != nil {
		header += "  (was " + resourceLabel(resource.PreviousResource) + ")"
	}
	p.line(header + attributionLabel(resource.Attribution))

	switch changeType {
	case goclientnew.Add:
		p.line(p.paint(ansiGreen, indentLines(trimValue(resource.ToValue), "    ")))
	case goclientnew.Delete:
		p.line(p.paint(ansiRed, indentLines(trimValue(resource.FromValue), "    ")))
	case goclientnew.None, goclientnew.Update, goclientnew.Replace:
		// Shown path by path below rather than whole.
	}
	for i := range resource.Changes {
		p.pathChange(&resource.Changes[i])
	}
}

func resourceLabel(resource *goclientnew.ResourceInfo) string {
	if resource == nil {
		return ""
	}
	return resource.ResourceType + " " + resource.ResourceName
}

func (p printer) pathChange(change *goclientnew.PathChange) {
	const indent = "      "
	label := change.DisplayPath
	if change.Comment != "" {
		comment := strings.ToLower(change.Comment) + " comment"
		if label == "" {
			label = "(" + comment + " on the resource)"
		} else {
			label += " (" + comment + ")"
		}
	}
	if change.ChangeType == "None" {
		// Context: the unchanged value, dimmed, where a change would be.
		value := trimValue(change.ToValue)
		if strings.Contains(value, "\n") {
			p.line(p.paint(ansiDim, "    "+label+":"))
			p.line(p.paint(ansiDim, indentLines(value, indent)))
		} else {
			p.line(p.paint(ansiDim, "    "+label+": "+value))
		}
		return
	}
	var symbol string
	switch change.ChangeType {
	case "Add":
		symbol = p.paint(ansiGreen, "+")
	case "Delete":
		symbol = p.paint(ansiRed, "-")
	case "Replace":
		symbol = p.paint(ansiGreen, "!")
	default:
		symbol = p.paint(ansiGreen, "~")
	}
	p.line(fmt.Sprintf("  %s [%s] %s%s", symbol, change.ChangeType, label, attributionLabel(change.Attribution)))

	fromValue, toValue := trimValue(change.FromValue), trimValue(change.ToValue)
	switch {
	case change.ChangeType == "Add":
		p.line(p.paint(ansiGreen, indentLines(toValue, indent)))
	case change.ChangeType == "Delete":
		p.line(p.paint(ansiRed, indentLines(fromValue, indent)))
	case change.Patch != "":
		p.patch(change.Patch, indent)
	case strings.Contains(fromValue, "\n") || strings.Contains(toValue, "\n"):
		p.line(p.paint(ansiRed, indentLines(fromValue, indent)))
		p.line(indent + "→")
		p.line(p.paint(ansiGreen, indentLines(toValue, indent)))
	default:
		p.line(indent + p.paint(ansiRed, fromValue) + " → " + p.paint(ansiGreen, toValue))
	}
}

// patch writes a unified line diff, colored by line.
func (p printer) patch(patch, indent string) {
	for _, line := range strings.Split(strings.TrimRight(patch, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			p.line(indent + p.paint(ansiGreen, line))
		case strings.HasPrefix(line, "-"):
			p.line(indent + p.paint(ansiRed, line))
		default:
			p.line(indent + line)
		}
	}
}

// attributionLabel says what set a value, when the diff was asked for with Attribution.
func attributionLabel(info *goclientnew.MutationInfo) string {
	if info == nil {
		return ""
	}
	return fmt.Sprintf("  (#%d)", info.Index)
}

func trimValue(value string) string {
	return strings.TrimRight(value, "\n")
}

// indentLines adds indent to each line of a possibly multi-line string.
func indentLines(s, indent string) string {
	return indent + strings.ReplaceAll(s, "\n", "\n"+indent)
}
