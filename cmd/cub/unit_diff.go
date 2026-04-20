// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/spf13/cobra"
)

type diffSegment struct {
	Type         string `json:"type"`
	Content      string `json:"content"`
	StartLineOld int    `json:",omitempty"`
	EndLineOld   int    `json:",omitempty"`
	StartLineNew int    `json:",omitempty"`
	EndLineNew   int    `json:",omitempty"`
}

const (
	colorReset     = "\033[0m"
	colorRed       = "\033[31m"
	colorGreen     = "\033[32m"
	colorLightBlue = "\033[94m" // Light blue for line numbers

	// Diff segment types
	segEqual  = "equal"
	segDelete = "delete"
	segAdd    = "add"

	// Default revision references
	defaultFrom = "LiveRevisionNum"
	defaultTo   = "HeadRevisionNum"
)

var unitDiffCmd = &cobra.Command{
	Use:   "diff <unit-slug> [fromRev] [toRev]",
	Short: "Show differences between revisions",
	Long: getCommandHelp(`Show differences between revisions of a unit, or between two units.

Revision References:
  - Absolute: 123, 456
  - Named: HeadRevisionNum, LiveRevisionNum, LastAppliedRevisionNum, PreviousLiveRevisionNum
  - Relative: -1, -2, -3 (N revisions back from HeadRevisionNum)
  - Tag: Tag:release-v1.0
  - ChangeSet: ChangeSet:feature-deploy

Output Formats:
  - Default: Line-numbered format with color
  - Unified: Use -u for unified diff format (like git diff)
  - Color: Use -c to enable color in unified diff
  - Mutations: Use -o mutations for structured mutation display

Examples:
`+"```"+`
  # Basic (defaults: LiveRevisionNum vs HeadRevisionNum)
  cub unit diff my-unit

  # Specific revisions
  cub unit diff my-unit --from=123 --to=456
  cub unit diff my-unit 123 456

  # Named revisions
  cub unit diff my-unit --from=LastAppliedRevisionNum
  cub unit diff my-unit --from=PreviousLiveRevisionNum

  # Relative to head
  cub unit diff my-unit --from=-1
  cub unit diff my-unit --from=-2 --to=-1

  # Unified diff format
  cub unit diff -u my-unit
  cub unit diff -uc my-unit --from=-1

  # Cross-unit diff
  cub unit diff my-unit --with-unit other-unit

  # Show mutations instead of text diff
  cub unit diff my-unit -o mutations
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(1, 3),
	RunE: runRevisionDiff,
}

var unitDiffArgs struct {
	unifiedDiff      bool
	colorOutput      bool
	fromRev          string
	toRev            string
	withUnit         string
	displayMutations bool
}

func init() {
	unitDiffCmd.Flags().BoolVarP(&unitDiffArgs.unifiedDiff, "unified", "u", false, "output unified diff format")
	unitDiffCmd.Flags().BoolVarP(&unitDiffArgs.colorOutput, "color", "c", false, "colorize the unified diff output (default: true for numbered diff)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.fromRev, "from", defaultFrom, "source revision (defaults to LiveRevisionNum)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.toRev, "to", defaultTo, "target revision (defaults to HeadRevisionNum)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.withUnit, "with-unit", "", "second unit for cross-unit diff (slug, space/slug, or UUID)")
	// Register -o locally with a constrained description: unit diff produces a
	// text or mutations diff, not a structured entity payload, so json/yaml/jq/yq
	// don't apply here.
	unitDiffCmd.Flags().StringVarP(&outputFormat, "output", "o", "",
		`Output format. Only "mutations" is supported; replaces the text diff with a resource-mutations diff.`)
	unitDiffCmd.Flags().BoolVar(&unitDiffArgs.displayMutations, "display-mutations", false, "display resource mutations instead of text diff")
	_ = unitDiffCmd.Flags().MarkDeprecated("display-mutations", "use -o mutations")
	unitCmd.AddCommand(unitDiffCmd)
}

// resolveRevisionNumber resolves a revision reference to an actual revision number
// Supports:
// - Absolute revision numbers: 123, 456
// - API field names: HeadRevisionNum, LiveRevisionNum, LastAppliedRevisionNum, PreviousLiveRevisionNum
// - Negative numbers (relative to HeadRevisionNum): -1, -2, -3
func resolveRevisionNumber(unitSlug string, revSpec string) (int64, error) {
	// Get unit data (we'll need it for most cases)
	unit, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return 0, fmt.Errorf("failed to get unit %s: %v", unitSlug, err)
	}

	// Check for API field names
	switch revSpec {
	case "HeadRevisionNum":
		return unit.HeadRevisionNum, nil
	case "LiveRevisionNum":
		return unit.LiveRevisionNum, nil
	case "LastAppliedRevisionNum":
		return unit.LastAppliedRevisionNum, nil
	case "PreviousLiveRevisionNum":
		return unit.PreviousLiveRevisionNum, nil
	}

	// Try parsing as a number (could be positive absolute or negative relative)
	num, err := strconv.ParseInt(revSpec, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid revision reference '%s': must be a revision number, -N (relative to head), or one of HeadRevisionNum/LiveRevisionNum/LastAppliedRevisionNum/PreviousLiveRevisionNum", revSpec)
	}

	// Handle negative numbers (relative to HeadRevisionNum)
	if num < 0 {
		resolved := unit.HeadRevisionNum + num
		if resolved < 1 {
			return 0, fmt.Errorf("revision delta %d results in revision %d which is out of range (must be >= 1)", num, resolved)
		}
		return resolved, nil
	}

	// Positive number - treat as absolute revision number
	return num, nil
}

func ComputeStructuredDiff(oldText, newText string) []diffSegment {
	dmp := diffmatchpatch.New()
	c1, c2, lineArray := dmp.DiffLinesToChars(oldText, newText)
	diffs := dmp.DiffMain(c1, c2, true)

	newDiffs := dmp.DiffCharsToLines(diffs, lineArray)
	structured := []diffSegment{}
	currentOldLine := 1
	currentNewLine := 1

	for _, diff := range newDiffs {
		// Split the diff text into lines (without line endings)
		lines := strings.Split(diff.Text, "\n")
		numLines := len(lines)
		if numLines == 0 {
			continue
		}

		segment := diffSegment{
			Type:    segEqual,
			Content: diff.Text,
		}

		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			segment.Type = segAdd
			segment.StartLineNew = currentNewLine
			segment.EndLineNew = currentNewLine + numLines - 1
			currentNewLine += numLines
		case diffmatchpatch.DiffDelete:
			segment.Type = segDelete
			segment.StartLineOld = currentOldLine
			segment.EndLineOld = currentOldLine + numLines - 1
			currentOldLine += numLines
		default: // DiffEqual
			segment.Type = segEqual
			segment.StartLineOld = currentOldLine
			segment.EndLineOld = currentOldLine + numLines - 1
			segment.StartLineNew = currentNewLine
			segment.EndLineNew = currentNewLine + numLines - 1
			currentOldLine += numLines
			currentNewLine += numLines
		}

		structured = append(structured, segment)
	}

	return structured
}

func findMaxLine(segments []diffSegment) int {
	maxLine := 0
	for _, segment := range segments {
		if segment.EndLineNew > maxLine {
			maxLine = segment.EndLineNew
		}
		if segment.EndLineOld > maxLine {
			maxLine = segment.EndLineOld
		}
	}
	return maxLine
}

func printNumberedDiff(segments []diffSegment) {
	maxLine := findMaxLine(segments)
	lineWidth := len(fmt.Sprintf("%d", maxLine))
	lineFormat := fmt.Sprintf("%%%dd: ", lineWidth)

	currentOldLine := 1
	currentNewLine := 1

	for _, segment := range segments {
		lines := strings.Split(strings.TrimSuffix(segment.Content, "\n"), "\n")

		for _, line := range lines {
			lineContent := line
			if line == "" {
				lineContent = " " // Convert empty lines to a single space to maintain formatting
			}

			switch segment.Type {
			case segEqual:
				fmt.Printf("%s"+lineFormat+"%s", colorLightBlue, currentNewLine, colorReset)
				fmt.Printf("  %s\n", lineContent)
				currentOldLine++
				currentNewLine++
			case segDelete:
				fmt.Printf("%s"+lineFormat+"%s", colorLightBlue, currentOldLine, colorReset)
				fmt.Printf("%s-%s%s\n", colorRed, lineContent, colorReset)
				currentOldLine++
			case segAdd:
				fmt.Printf("%s"+lineFormat+"%s", colorLightBlue, currentNewLine, colorReset)
				fmt.Printf("%s+%s%s\n", colorGreen, lineContent, colorReset)
				currentNewLine++
			}
		}
	}
}

func printUnifiedDiff(segments []diffSegment, oldFile, newFile string) {
	// Check if there are any actual changes
	hasChanges := false
	for _, seg := range segments {
		if seg.Type == segAdd || seg.Type == segDelete {
			hasChanges = true
			break
		}
	}

	// If no changes, return without printing anything
	if !hasChanges {
		return
	}

	fmt.Printf("--- %s\n", oldFile)
	fmt.Printf("+++ %s\n", newFile)

	type Line struct {
		Type    string
		OldLine int
		NewLine int
		Content string
	}

	var lines []Line
	for _, seg := range segments {
		content := strings.TrimSuffix(seg.Content, "\n")
		segLines := strings.Split(content, "\n")
		for i, lineContent := range segLines {
			l := Line{Content: lineContent}
			switch seg.Type {
			case segEqual:
				l.Type = segEqual
				l.OldLine = seg.StartLineOld + i
				l.NewLine = seg.StartLineNew + i
			case segDelete:
				l.Type = segDelete
				l.OldLine = seg.StartLineOld + i
				l.NewLine = 0
			case segAdd:
				l.Type = segAdd
				l.OldLine = 0
				l.NewLine = seg.StartLineNew + i
			}
			lines = append(lines, l)
		}
	}

	// Mark lines that should be included in hunks (changed lines and context)
	inHunk := make([]bool, len(lines))
	for i, line := range lines {
		if line.Type == segAdd || line.Type == segDelete {
			inHunk[i] = true

			// Include 3 lines of context before
			for j := i - 1; j >= 0 && j >= i-3; j-- {
				if lines[j].Type == segEqual {
					inHunk[j] = true
				}
			}

			// Include 3 lines of context after
			for j := i + 1; j < len(lines) && j <= i+3; j++ {
				if lines[j].Type == segEqual {
					inHunk[j] = true
				}
			}
		}
	}

	// Group lines into hunks
	var hunks [][]Line
	var currentHunk []Line
	for i, line := range lines {
		if inHunk[i] {
			currentHunk = append(currentHunk, line)
		} else if len(currentHunk) > 0 {
			hunks = append(hunks, currentHunk)
			currentHunk = nil
		}
	}
	if len(currentHunk) > 0 {
		hunks = append(hunks, currentHunk)
	}

	// Print hunks
	for _, hunk := range hunks {
		if len(hunk) == 0 {
			continue
		}

		// Calculate hunk header
		var oldStart, oldCount, newStart, newCount int
		for _, l := range hunk {
			switch l.Type {
			case segEqual, segDelete:
				if oldStart == 0 {
					oldStart = l.OldLine
				}
				oldCount++
			case segAdd:
				if newStart == 0 {
					newStart = l.NewLine
				}
				newCount++
			}
		}

		fmt.Printf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)

		// Print lines in the hunk
		for _, l := range hunk {
			switch l.Type {
			case segEqual:
				fmt.Printf(" %s\n", l.Content)
			case segDelete:
				if unitDiffArgs.colorOutput {
					fmt.Printf("%s-%s%s\n", colorRed, l.Content, colorReset)
				} else {
					fmt.Printf("-%s\n", l.Content)
				}
			case segAdd:
				if unitDiffArgs.colorOutput {
					fmt.Printf("%s+%s%s\n", colorGreen, l.Content, colorReset)
				} else {
					fmt.Printf("+%s\n", l.Content)
				}
			}
		}
	}
}

func runRevisionDiff(cmd *cobra.Command, args []string) error {
	unitSlug := args[0]
	revFrom := unitDiffArgs.fromRev
	revTo := unitDiffArgs.toRev

	// Validate -o: only "mutations" is meaningful for this command.
	if outputFormat != "" && outputFormat != "mutations" {
		return fmt.Errorf(`"cub unit diff" only accepts "-o mutations"; %q is not supported`, outputFormat)
	}

	// Prevent mixing positional arguments with --from/--to flags
	if len(args) > 1 && (unitDiffArgs.fromRev != defaultFrom || unitDiffArgs.toRev != defaultTo) {
		return fmt.Errorf("cannot mix positional arguments with --from/--to flags")
	}

	// Handle flag-based revision specification
	if unitDiffArgs.fromRev != defaultFrom || unitDiffArgs.toRev != defaultTo {
		// If either flag is set, use flag values with defaults
		if unitDiffArgs.fromRev == "" {
			unitDiffArgs.fromRev = defaultFrom
		}
		if unitDiffArgs.toRev == "" {
			unitDiffArgs.toRev = defaultTo
		}
	} else {
		// Handle positional arguments
		revFrom = defaultFrom
		revTo = defaultTo
		if len(args) == 2 {
			revTo = args[1]
		} else if len(args) == 3 {
			revFrom = args[1]
			revTo = args[2]
		}
	}

	// Get the first unit
	unit, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return fmt.Errorf("failed to get unit %s: %v", unitSlug, err)
	}

	// Get the second unit if --with-unit is specified (cross-unit diff)
	var toUnit *goclientnew.Unit
	if unitDiffArgs.withUnit != "" {
		toUnit, err = parseEntityIdentifierSingleAsEntity[goclientnew.Unit](
			unitDiffArgs.withUnit,
			"unit",
			"*",
			apiGetUnitFromSlugInSpace,
			func(u *goclientnew.Unit) string { return u.UnitID.String() },
		)
		if err != nil {
			return fmt.Errorf("failed to get second unit %s: %w", unitDiffArgs.withUnit, err)
		}
	} else {
		toUnit = unit
	}

	// Resolve revision numbers using parseSelectedRevisionParameter
	fromFormatted, fromIsUUID, err := parseSelectedRevisionParameter(revFrom, unit.UnitID, unit.SpaceID.String(), unit.HeadRevisionNum)
	if err != nil {
		return err
	}

	toFormatted, toIsUUID, err := parseSelectedRevisionParameter(revTo, toUnit.UnitID, toUnit.SpaceID.String(), toUnit.HeadRevisionNum)
	if err != nil {
		return err
	}

	// Resolve to revision numbers for fetching data
	revFromNum, err := resolveFormattedRevision(fromFormatted, fromIsUUID, unit)
	if err != nil {
		return err
	}
	if revFromNum == 0 {
		return fmt.Errorf("revision %s not found or is invalid", revFrom)
	}

	revToNum, err := resolveFormattedRevision(toFormatted, toIsUUID, toUnit)
	if err != nil {
		return err
	}
	if revToNum == 0 {
		return fmt.Errorf("revision %s not found or is invalid", revTo)
	}

	// Get revision data for both revisions
	revFromData, err := apiGetRevisionFromNumber(revFromNum, unit.UnitID.String(), "*")
	if err != nil {
		return fmt.Errorf("failed to get revision %d: %v", revFromNum, err)
	}

	revToData, err := apiGetRevisionFromNumber(revToNum, toUnit.UnitID.String(), "*")
	if err != nil {
		return fmt.Errorf("failed to get revision %d: %v", revToNum, err)
	}

	// Decode base64 data
	fromData, err := base64.StdEncoding.DecodeString(revFromData.Data)
	if err != nil {
		return fmt.Errorf("failed to decode revision %d data: %v", revFromNum, err)
	}

	toData, err := base64.StdEncoding.DecodeString(revToData.Data)
	if err != nil {
		return fmt.Errorf("failed to decode revision %d data: %v", revToNum, err)
	}

	if unitDiffArgs.displayMutations || outputFormat == "mutations" {
		// Display mutations instead of text diff
		lookupMutationsUnitID = toUnit.UnitID.String()
		displayMutationsFromDryRun(revFromData.Data, revToData.Data, toUnit.SpaceID.String(), "diff")
	} else {
		// Compute text diff
		diffSegments := ComputeStructuredDiff(string(fromData), string(toData))

		// Format file labels
		fromLabel := formatDiffLabel(unitSlug, revFromNum)
		toLabel := formatDiffLabel(unitSlugOrWith(unitSlug, unitDiffArgs.withUnit), revToNum)

		// Print diff in requested format
		if unitDiffArgs.unifiedDiff {
			printUnifiedDiff(diffSegments, fromLabel, toLabel)
		} else {
			printNumberedDiff(diffSegments)
		}
	}

	return nil
}

func formatDiffLabel(unitSlug string, revNum int64) string {
	return fmt.Sprintf("%s/%s/%d", selectedSpaceSlug, unitSlug, revNum)
}

func unitSlugOrWith(slug, withUnit string) string {
	if withUnit != "" {
		return withUnit
	}
	return slug
}

// resolveFormattedRevision converts a parseSelectedRevisionParameter result to a revision number.
func resolveFormattedRevision(formatted string, isUUID bool, unit *goclientnew.Unit) (int64, error) {
	if isUUID {
		// It's a revision UUID - look it up
		rev, err := apiGetRevisionFromUUID(formatted, unit.UnitID.String())
		if err != nil {
			return 0, err
		}
		return rev.RevisionNum, nil
	}

	// Check named revision values
	switch formatted {
	case "HeadRevisionNum":
		return unit.HeadRevisionNum, nil
	case "LiveRevisionNum":
		return unit.LiveRevisionNum, nil
	case "LastAppliedRevisionNum":
		return unit.LastAppliedRevisionNum, nil
	case "PreviousLiveRevisionNum":
		return unit.PreviousLiveRevisionNum, nil
	}

	// Check for Tag: or ChangeSet: prefix - these need API lookup
	if strings.HasPrefix(formatted, "Tag:") || strings.HasPrefix(formatted, "ChangeSet:") ||
		strings.HasPrefix(formatted, "Before:") {
		// Use the API restore parameter to let the server resolve this
		// For now, fall back to resolveRevisionNumber for simple numeric cases
		return resolveRevisionNumber(unit.Slug, formatted)
	}

	// Try parsing as number
	num, err := strconv.ParseInt(formatted, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("could not resolve revision '%s'", formatted)
	}
	return num, nil
}

// apiGetRevisionFromUUID fetches a revision by UUID.
func apiGetRevisionFromUUID(revisionUUID string, unitID string) (*goclientnew.Revision, error) {
	where := fmt.Sprintf("RevisionID = '%s'", revisionUUID)
	revisions, err := apiListRevisions(selectedSpaceID, unitID, where, "RevisionNum", "")
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, fmt.Errorf("revision %s not found", revisionUUID)
	}
	return revisions[0].Revision, nil
}
