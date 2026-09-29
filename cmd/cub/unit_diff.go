// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
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
	colorDim       = "\033[2m"  // Dim, for text that annotates rather than reports

	// Diff segment types
	segEqual  = "equal"
	segDelete = "delete"
	segAdd    = "add"

	// Default revision references
	defaultFrom = "LastReleasedRevisionNum"
	defaultTo   = "HeadRevisionNum"
)

var unitDiffCmd = &cobra.Command{
	Use:   "diff <unit-slug> [fromRev] [toRev]",
	Short: "Show differences between revisions",
	Long: getCommandHelp(`Show differences between revisions of a unit, or between two units.

Revision References:
  - Absolute: 123, 456
  - Named: HeadRevisionNum, LastReleasedRevisionNum
  - Relative: -1, -2, -3 (N revisions back from HeadRevisionNum)
  - Tag: Tag:release-v1.0
  - ChangeSet: ChangeSet:feature-deploy

Output Formats:
  - Default: Line-numbered format with color
  - Unified: Use -u for unified diff format (like git diff)
  - Color: Use -c to enable color in unified diff
  - Mutations: Use -o mutations for a structured diff, path by path, that matches list
    elements by merge key -- containers and environment variables by name -- rather than by
    position; -o json, yaml, jq=<expr> or yq=<expr> print that diff as data

Examples:
`+"```"+`
  # Basic (defaults: LastReleasedRevisionNum vs HeadRevisionNum)
  cub unit diff my-unit

  # Specific revisions
  cub unit diff my-unit --from=123 --to=456
  cub unit diff my-unit 123 456

  # Named revisions
  cub unit diff my-unit --from=LastReleasedRevisionNum

  # Relative to head
  cub unit diff my-unit --from=-1
  cub unit diff my-unit --from=-2 --to=-1

  # Unified diff format
  cub unit diff -u my-unit
  cub unit diff -uc my-unit --from=-1

  # Cross-unit diff
  cub unit diff my-unit --with-unit other-unit

  # What uploading a local file would change, against the head or --from
  cub unit diff my-unit --file my-unit.yaml

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
	file             string
	displayMutations bool
}

func init() {
	unitDiffCmd.Flags().BoolVarP(&unitDiffArgs.unifiedDiff, "unified", "u", false, "output unified diff format")
	unitDiffCmd.Flags().BoolVarP(&unitDiffArgs.colorOutput, "color", "c", false, "colorize the unified diff output (default: true for numbered diff)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.fromRev, "from", defaultFrom, "source revision (defaults to LastReleasedRevisionNum)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.toRev, "to", defaultTo, "target revision (defaults to HeadRevisionNum)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.withUnit, "with-unit", "", "second unit for cross-unit diff (slug, space/slug, or UUID)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.file, "file", "", "diff the unit, at its head or --from, against this local file (- for stdin)")
	// Register -o locally with a constrained description: unit diff produces a text or a
	// structured diff, and json/yaml/jq/yq print the structured one as data.
	unitDiffCmd.Flags().StringVarP(&outputFormat, "output", "o", "",
		`Output format: "mutations" replaces the text diff with a structured, path-by-path diff; json, yaml, jq=<expr> and yq=<expr> print that diff as data.`)
	unitDiffCmd.Flags().BoolVar(&unitDiffArgs.displayMutations, "display-mutations", false, "display resource mutations instead of text diff")
	_ = unitDiffCmd.Flags().MarkDeprecated("display-mutations", "use -o mutations")
	enableOptionalSpace(unitDiffCmd)
	unitCmd.AddCommand(unitDiffCmd)
}

// TODO: Support [Before:] Tag, ChangeSet, and ChangeOrder
// See parseSelectedRevisionParameter.

// resolveRevisionNumber resolves a revision reference to an actual revision number
// Supports:
// - Absolute revision numbers: 123, 456
// - API field names: HeadRevisionNum, LastReleasedRevisionNum
// - Negative numbers (relative to HeadRevisionNum): -1, -2, -3
func resolveRevisionNumber(unitSlug string, revSpec string) (int64, error) {
	// Get unit data (we'll need it for most cases)
	unit, err := resolveUnit(unitSlug, selectedSpaceID, "*")
	if err != nil {
		return 0, fmt.Errorf("failed to get unit %s: %v", unitSlug, err)
	}

	// Check for API field names
	switch revSpec {
	case "HeadRevisionNum":
		return unit.Unit.HeadRevisionNum, nil
	case "LastReleasedRevisionNum":
		return unit.Unit.LastReleasedRevisionNum, nil
	}

	// Try parsing as a number (could be positive absolute or negative relative)
	num, err := strconv.ParseInt(revSpec, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid revision reference '%s': must be a revision number, -N (relative to head), or one of HeadRevisionNum/LastReleasedRevisionNum", revSpec)
	}

	// Handle negative numbers (relative to HeadRevisionNum)
	if num < 0 {
		resolved := unit.Unit.HeadRevisionNum + num
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

func printUnifiedDiff(segments []diffSegment, oldFile, newFile string, colorize bool) {
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
				if colorize {
					fmt.Printf("%s-%s%s\n", colorRed, l.Content, colorReset)
				} else {
					fmt.Printf("-%s\n", l.Content)
				}
			case segAdd:
				if colorize {
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

	// Validate -o: a text diff, a structured diff, or the structured diff as data.
	outputSpec, err := parseOutputFormat(outputFormat)
	if err != nil {
		return err
	}
	switch outputSpec.Kind {
	case OutputDefault, OutputMutations, OutputJSON, OutputYAML, OutputJQ, OutputYQ:
	default:
		return fmt.Errorf(`"cub unit diff" accepts -o mutations, json, yaml, jq=<expr> and yq=<expr>; %q is not supported`, outputFormat)
	}
	structuredDiff := unitDiffArgs.displayMutations || outputSpec.Kind != OutputDefault

	if unitDiffArgs.file != "" {
		if unitDiffArgs.withUnit != "" || len(args) > 1 || unitDiffArgs.toRev != defaultTo {
			return fmt.Errorf("--file is the to side, so it cannot be combined with --with-unit, --to, or revision arguments")
		}
		// What uploading the file would change is a comparison with what the unit holds now,
		// not with what was last released.
		if unitDiffArgs.fromRev == defaultFrom {
			unitDiffArgs.fromRev = defaultTo
		}
		return runFileDiff(unitSlug, unitDiffArgs.fromRev, unitDiffArgs.file, structuredDiff)
	}

	// Comparing two units means comparing what they say now, so the from side defaults to
	// their heads rather than to the live revision. The live default belongs to a diff of one
	// unit against itself; applied to a cross-unit diff it fails outright on a unit that has
	// never been applied, which every unit in a base space has in common.
	if unitDiffArgs.withUnit != "" && unitDiffArgs.fromRev == defaultFrom && len(args) == 1 {
		unitDiffArgs.fromRev = defaultTo
		revFrom = defaultTo
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
	unit, err := resolveUnit(unitSlug, selectedSpaceID, "*")
	if err != nil {
		return fmt.Errorf("failed to get unit %s: %v", unitSlug, err)
	}

	// Get the second unit if --with-unit is specified (cross-unit diff)
	var toUnit *goclientnew.Unit
	if unitDiffArgs.withUnit != "" {
		resolved, err := resolveUnit(unitDiffArgs.withUnit, defaultSpaceID(), "*")
		if err != nil {
			return fmt.Errorf("failed to get second unit %s: %w", unitDiffArgs.withUnit, err)
		}
		toUnit = resolved.Unit
	} else {
		toUnit = unit.Unit
	}

	// Resolve revision numbers using parseSelectedRevisionParameter
	fromFormatted, fromIsUUID, err := parseSelectedRevisionParameter(revFrom, serverResolvedRevision, unit.Unit.HeadRevisionNum)
	if err != nil {
		return err
	}

	toFormatted, toIsUUID, err := parseSelectedRevisionParameter(revTo, serverResolvedRevision, toUnit.HeadRevisionNum)
	if err != nil {
		return err
	}

	// Resolve to revision numbers for fetching data
	revFromNum, err := resolveFormattedRevision(fromFormatted, fromIsUUID, unit.Unit)
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

	// Get revision data for both revisions. Each revision is looked up in its own unit's
	// space: with --with-unit the second unit can live in another space, and looking its
	// revision up in the first unit's space finds nothing.
	revFromData, err := apiGetRevisionFromNumberInSpace(revFromNum, unit.Unit.UnitID.String(), unit.Unit.SpaceID.String(), "*")
	if err != nil {
		return fmt.Errorf("failed to get revision %d of %s: %v", revFromNum, unitSlug, err)
	}

	revToData, err := apiGetRevisionFromNumberInSpace(revToNum, toUnit.UnitID.String(), toUnit.SpaceID.String(), "*")
	if err != nil {
		return fmt.Errorf("failed to get revision %d of %s/%s: %v", revToNum,
			toUnit.SpaceSlug, toUnit.Slug, err)
	}

	// A Revision's configuration is read from its data endpoint, not off the entity.
	fromData, err := fetchRevisionData(unit.Unit.SpaceID, unit.Unit.UnitID, revFromData.RevisionID)
	if err != nil {
		return fmt.Errorf("failed to get revision %d data: %v", revFromNum, err)
	}
	toData, err := fetchRevisionData(toUnit.SpaceID, toUnit.UnitID, revToData.RevisionID)
	if err != nil {
		return fmt.Errorf("failed to get revision %d data: %v", revToNum, err)
	}

	if structuredDiff && toUnit.UnitID == unit.Unit.UnitID {
		unitDiff, err := fetchUnitDiff(unit.Unit.SpaceID, unit.Unit.UnitID,
			strconv.FormatInt(revFromNum, 10), strconv.FormatInt(revToNum, 10))
		if err != nil {
			return err
		}
		if !renderPayload(unitDiff) {
			displayConfigDiff(unitDiff.Diff)
		}
	} else if structuredDiff {
		result, err := diffConfigurations(goclientnew.DiffRequest{
			From: unitDiffSide(unit.Unit.UnitID, revFromNum),
			To:   unitDiffSide(toUnit.UnitID, revToNum),
		})
		if err != nil {
			return err
		}
		if !renderPayload(result) {
			displayConfigDiff(result.Diff)
		}
	} else {
		// Compute text diff
		diffSegments := ComputeStructuredDiff(fromData, toData)

		// Format file labels. Each side is named by its own unit's space, so a cross-space
		// diff says where the second unit actually lives instead of prefixing it with the
		// first one's space.
		fromLabel := formatDiffLabel(unit.Unit.SpaceSlug, unitSlug, revFromNum)
		toLabel := formatDiffLabel(toUnit.SpaceSlug, toUnit.Slug, revToNum)

		// Print diff in requested format
		if unitDiffArgs.unifiedDiff {
			printUnifiedDiff(diffSegments, fromLabel, toLabel, unitDiffArgs.colorOutput)
		} else {
			printNumberedDiff(diffSegments)
		}
	}

	return nil
}

func formatDiffLabel(spaceSlug, unitSlug string, revNum int64) string {
	if spaceSlug == "" {
		spaceSlug = selectedSpaceSlug
	}
	return fmt.Sprintf("%s/%s/%d", spaceSlug, unitSlug, revNum)
}

// resolveFormattedRevision converts a parseSelectedRevisionParameter result to a revision number.
func resolveFormattedRevision(formatted string, isUUID bool, unit *goclientnew.Unit) (int64, error) {
	if isUUID {
		// It's a revision UUID - look it up
		rev, err := apiGetRevisionFromUUID(formatted, unit.UnitID.String(), unit.SpaceID.String())
		if err != nil {
			return 0, err
		}
		return rev.RevisionNum, nil
	}

	// Check named revision values
	switch formatted {
	case "HeadRevisionNum":
		return unit.HeadRevisionNum, nil
	case "LastReleasedRevisionNum":
		return unit.LastReleasedRevisionNum, nil
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
func apiGetRevisionFromUUID(revisionUUID string, unitID string, spaceID string) (*goclientnew.Revision, error) {
	where := fmt.Sprintf("RevisionID = '%s'", revisionUUID)
	revisions, err := apiListRevisions(spaceID, unitID, where, "RevisionNum", "")
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, fmt.Errorf("revision %s not found", revisionUUID)
	}
	return revisions[0].Revision, nil
}

// runFileDiff diffs a unit, at the Revision fromRev names, against a local file: what uploading
// the file would change.
func runFileDiff(unitSlug, fromRev, file string, structuredDiff bool) error {
	var fileData []byte
	var err error
	if file == "-" {
		fileData, err = io.ReadAll(os.Stdin)
	} else {
		fileData, err = os.ReadFile(file)
	}
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", file, err)
	}
	unit, err := resolveUnit(unitSlug, selectedSpaceID, "*")
	if err != nil {
		return fmt.Errorf("failed to get unit %s: %v", unitSlug, err)
	}
	fromFormatted, fromIsUUID, err := parseSelectedRevisionParameter(fromRev, serverResolvedRevision, unit.Unit.HeadRevisionNum)
	if err != nil {
		return err
	}
	revFromNum, err := resolveFormattedRevision(fromFormatted, fromIsUUID, unit.Unit)
	if err != nil {
		return err
	}
	if revFromNum == 0 {
		return fmt.Errorf("revision %s not found or is invalid", fromRev)
	}

	if structuredDiff {
		result, err := diffConfigurations(goclientnew.DiffRequest{
			From: unitDiffSide(unit.Unit.UnitID, revFromNum),
			To:   inlineDiffSide(fileData, unit.Unit.ToolchainType),
		})
		if err != nil {
			return err
		}
		if !renderPayload(result) {
			displayConfigDiff(result.Diff)
		}
		return nil
	}

	revision, err := apiGetRevisionFromNumberInSpace(revFromNum, unit.Unit.UnitID.String(), unit.Unit.SpaceID.String(), "*")
	if err != nil {
		return fmt.Errorf("failed to get revision %d of %s: %v", revFromNum, unitSlug, err)
	}
	fromData, err := fetchRevisionData(unit.Unit.SpaceID, unit.Unit.UnitID, revision.RevisionID)
	if err != nil {
		return fmt.Errorf("failed to get revision %d data: %v", revFromNum, err)
	}
	diffSegments := ComputeStructuredDiff(fromData, string(fileData))
	if unitDiffArgs.unifiedDiff {
		printUnifiedDiff(diffSegments, formatDiffLabel(unit.Unit.SpaceSlug, unitSlug, revFromNum), file, unitDiffArgs.colorOutput)
	} else {
		printNumberedDiff(diffSegments)
	}
	return nil
}
