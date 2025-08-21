// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

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

	// Revision references
	revLIVE = "live"
	revHEAD = "head"

	// Diff segment types
	segEqual  = "equal"
	segDelete = "delete"
	segAdd    = "add"
)

var unitDiffCmd = &cobra.Command{
	Use:   "diff <unit-slug> [fromRev] [toRev]",
	Short: "Show differences between revisions",
	Long: `Show differences between revisions of a unit.

Usage Modes:
  1. Positional Arguments (cannot be mixed with flags):
     cub unit diff <unit-slug>               # Compare live vs head
     cub unit diff <unit-slug> <rev1>        # Compare live vs rev1
     cub unit diff <unit-slug> <rev1> <rev2> # Compare rev1 vs rev2

  2. Flag-based (cannot be mixed with positionals):
     --from  Source revision (defaults to live)
     --to    Target revision (defaults to head)

Output Formats:
  - Default: Line-numbered format with color
  - Unified: Use -u for unified diff format (like git diff)
  - Color:   Use -c to enable color in unified diff

Examples:
  # Basic Comparisons
  cub unit diff my-unit                     # live    vs head
  cub unit diff my-unit --from=123          # rev 123 vs head
  cub unit diff my-unit --to=456            # live    vs rev 456
  cub unit diff my-unit --from=123 --to=456 # rev 123 vs rev 456

  # With Unified Diff
  cub unit diff -u  my-unit                 # Unified format
  cub unit diff -uc my-unit                 # Unified format with color`,
	Args: cobra.RangeArgs(1, 3),
	RunE: runRevisionDiff,
}

var unitDiffArgs struct {
	unifiedDiff bool
	colorOutput bool
	fromRev     string
	toRev       string
}

func init() {
	unitDiffCmd.Flags().BoolVarP(&unitDiffArgs.unifiedDiff, "unified", "u", false, "output unified diff format")
	unitDiffCmd.Flags().BoolVarP(&unitDiffArgs.colorOutput, "color", "c", false, "colorize the unified diff output (default: true for numbered diff)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.fromRev, "from", revLIVE, "source revision (defaults to live)")
	unitDiffCmd.Flags().StringVar(&unitDiffArgs.toRev, "to", revHEAD, "target revision (defaults to head)")
	unitCmd.AddCommand(unitDiffCmd)
}

// resolveRevisionNumber gets the actual revision number for HEAD or numeric reference
func resolveRevisionNumber(unitSlug string, base string) (int64, error) {
	if base == revHEAD || base == revLIVE {
		unit, err := apiGetUnitFromSlug(unitSlug, "*") // get all fields for now
		if err != nil {
			return 0, fmt.Errorf("failed to get unit %s: %v", unitSlug, err)
		}
		if base == revHEAD {
			return unit.HeadRevisionNum, nil
		} else if base == revLIVE {
			return unit.LiveRevisionNum, nil
		}
	}

	// Try parsing as number
	num, err := strconv.ParseInt(base, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid revision reference: %s", base)
	}
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

	// Prevent mixing positional arguments with --from/--to flags
	if len(args) > 1 && (unitDiffArgs.fromRev != revLIVE || unitDiffArgs.toRev != revHEAD) {
		return fmt.Errorf("cannot mix positional arguments with --from/--to flags")
	}

	// Handle flag-based revision specification
	if unitDiffArgs.fromRev != revLIVE || unitDiffArgs.toRev != revHEAD {
		// If either flag is set, use flag values with defaults
		if unitDiffArgs.fromRev == "" {
			unitDiffArgs.fromRev = revLIVE
		}
		if unitDiffArgs.toRev == "" {
			unitDiffArgs.toRev = revHEAD
		}
	} else {
		// Handle positional arguments
		revFrom = revLIVE
		revTo = revHEAD
		if len(args) == 2 {
			revTo = args[1]
		} else if len(args) == 3 {
			revFrom = args[1]
			revTo = args[2]
		}
	}

	// Resolve revision numbers
	revFromNum, err := resolveRevisionNumber(unitSlug, revFrom)
	if err != nil {
		return err
	}
	if revFromNum == 0 {
		return fmt.Errorf("revision %s not found or is invalid", revFrom)
	}

	revToNum, err := resolveRevisionNumber(unitSlug, revTo)
	if err != nil {
		return err
	}
	if revToNum == 0 {
		return fmt.Errorf("revision %s not found or is invalid", revTo)
	}

	// Get unit ID
	unit, err := apiGetUnitFromSlug(unitSlug, "*") // get all fields for now
	if err != nil {
		return fmt.Errorf("failed to get unit %s: %v", unitSlug, err)
	}

	// Get revision data for both revisions
	revFromData, err := apiGetRevisionFromNumber(revFromNum, unit.UnitID.String(), "*") // get all fields for now
	if err != nil {
		return fmt.Errorf("failed to get revision %d: %v", revFromNum, err)
	}

	revToData, err := apiGetRevisionFromNumber(revToNum, unit.UnitID.String(), "*") //get all fields for now
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

	// Compute diff
	// toData is the base content
	// fromData is newer content
	diffSegments := ComputeStructuredDiff(string(toData), string(fromData))

	// Print diff in requested format
	if unitDiffArgs.unifiedDiff {
		oldFile := fmt.Sprintf("%s/%s/%d", selectedSpaceSlug, unitSlug, revFromNum)
		newFile := fmt.Sprintf("%s/%s/%d", selectedSpaceSlug, unitSlug, revToNum)
		printUnifiedDiff(diffSegments, oldFile, newFile)
	} else {
		printNumberedDiff(diffSegments)
	}

	return nil
}
