// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	dmp "github.com/sergi/go-diff/diffmatchpatch"
)

// Patch format constants used as the "format" field in structural patches.
const (
	PatchFormatJSON = "json"
	PatchFormatYAML = "yaml"
)

// structuralPatch is the JSON-serialized format for structural patches on embedded
// JSON or YAML string values. It is distinguished from a line-level text patch
// (unified diff format) by starting with '{'.
type structuralPatch struct {
	Format    string          `json:"format"`    // "json" or "yaml"
	Mutations api.MutationMap `json:"mutations"` // sub-path mutations within the embedded document
}

// ComputeScalarPatch computes a patch for a changed multi-line scalar string value.
// It tries to parse both values as JSON, then YAML, and computes a structural patch
// (sub-path mutations) for those formats. Falls back to a line-level text diff.
//
// Structural patches give true three-way merge for embedded JSON/YAML: individual
// field changes are tracked by path, so independent changes to different fields
// merge correctly. Line-level patches handle unstructured text (markdown, config
// files, etc.) with context-based fuzzy matching.
func ComputeScalarPatch(previous, modified string) string {
	// Try JSON first
	if patch := computeStructuralPatch(previous, modified, PatchFormatJSON); patch != "" {
		return patch
	}
	// YAML autodetection is disabled because YAML frontmatter in text
	// documents (AppConfig/Text) would be misidentified as structured
	// YAML content, causing incorrect structural patches.
	// Fall back to line-level text diff
	return ComputeLinePatch(previous, modified)
}

// computeStructuralPatch attempts to parse both strings in the given format and
// compute a structural diff. Returns "" if parsing fails or there are no mutations.
func computeStructuralPatch(previous, modified, format string) string {
	var prevDoc, modDoc *gaby.YamlDoc
	var err error

	switch format {
	case PatchFormatJSON:
		prevDoc, err = gaby.ParseJSON([]byte(previous))
		if err != nil {
			return ""
		}
		modDoc, err = gaby.ParseJSON([]byte(modified))
		if err != nil {
			return ""
		}
	case PatchFormatYAML:
		prevDoc, err = gaby.ParseYAML([]byte(previous))
		if err != nil {
			return ""
		}
		modDoc, err = gaby.ParseYAML([]byte(modified))
		if err != nil {
			return ""
		}
		// Only use structural YAML patch if both documents are maps or arrays
		// at the top level. Plain text (scalars) frequently parses as valid YAML,
		// so we require structured content to avoid misidentifying text as YAML.
		if !isMapOrArray(prevDoc) || !isMapOrArray(modDoc) {
			return ""
		}
	default:
		return ""
	}

	mutations := make(api.MutationMap)
	ComputeMutationsForDocs("", prevDoc, modDoc, 0, mutations, nil, nil, nil)

	if len(mutations) == 0 {
		return ""
	}

	patch := structuralPatch{
		Format:    format,
		Mutations: mutations,
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return ""
	}
	return string(data)
}

// ApplyScalarPatch applies a patch (produced by ComputeScalarPatch) to a target string.
// It auto-detects the patch format: structural patches (JSON-encoded, start with '{')
// are applied structurally; line-level text patches (unified diff) are applied with
// fuzzy context matching.
//
// Returns the patched string and true if the patch applied cleanly.
// On failure, returns the original target string and false.
func ApplyScalarPatch(target, patch string) (string, bool) {
	if strings.HasPrefix(patch, "{") {
		return applyStructuralPatch(target, patch)
	}
	return ApplyLinePatch(target, patch)
}

// applyStructuralPatch applies a JSON-encoded structural patch to the target string.
func applyStructuralPatch(target, patchStr string) (string, bool) {
	var patch structuralPatch
	if err := json.Unmarshal([]byte(patchStr), &patch); err != nil {
		return target, false
	}

	var targetDoc *gaby.YamlDoc
	var err error
	switch patch.Format {
	case PatchFormatJSON:
		targetDoc, err = gaby.ParseJSON([]byte(target))
	case PatchFormatYAML:
		targetDoc, err = gaby.ParseYAML([]byte(target))
	default:
		return target, false
	}
	if err != nil {
		return target, false
	}

	// Apply each sub-path mutation to the parsed document.
	// Sort paths so parents are processed before children.
	patches := api.SortedMutationMapEntries(patch.Mutations)
	for _, entry := range patches {
		subPath := string(entry.Path)
		mutation := entry.MutationInfo

		switch mutation.MutationType {
		case api.MutationTypeAdd, api.MutationTypeReplace:
			valueDoc, parseErr := gaby.ParseYAML([]byte(mutation.Value))
			if parseErr != nil {
				slog.Debug("structural patch: error parsing value", "path", subPath, "error", parseErr)
				continue
			}
			_, setErr := targetDoc.SetDocP(valueDoc, subPath)
			if setErr != nil {
				slog.Debug("structural patch: error setting value", "path", subPath, "error", setErr)
			}
		case api.MutationTypeUpdate:
			valueDoc, parseErr := gaby.ParseYAML([]byte(mutation.Value))
			if parseErr != nil {
				slog.Debug("structural patch: error parsing value", "path", subPath, "error", parseErr)
				continue
			}
			_, setErr := targetDoc.SetDocP(valueDoc, subPath)
			if setErr != nil {
				slog.Debug("structural patch: error setting value", "path", subPath, "error", setErr)
			}
		case api.MutationTypeDelete:
			delErr := targetDoc.DeleteP(subPath)
			if delErr != nil {
				slog.Debug("structural patch: error deleting path", "path", subPath, "error", delErr)
			}
		}
	}

	// Reserialize in the original format.
	var out []byte
	switch patch.Format {
	case PatchFormatJSON:
		out, err = targetDoc.MarshalJSON()
	case PatchFormatYAML:
		out, err = targetDoc.MarshalYAML()
	}
	if err != nil {
		return target, false
	}

	result := string(out)
	// For JSON, trim trailing newline to match typical JSON string values.
	if patch.Format == PatchFormatJSON {
		result = strings.TrimRight(result, "\n")
	}

	return result, true
}

// ComputeLinePatch computes a line-level diff between two multi-line strings and returns
// a patch in unified diff text format. The patch can be serialized as a string in MutationInfo.Patch
// and later applied with ApplyLinePatch.
//
// This uses the Myers diff algorithm via go-diff's DiffLinesToChars to tokenize at line
// boundaries, producing a minimal edit script that correctly identifies inserted, deleted,
// and unchanged lines.
func ComputeLinePatch(previous, modified string) string {
	d := dmp.New()

	// Tokenize at line boundaries for line-level diffing
	chars1, chars2, lineArray := d.DiffLinesToChars(previous, modified)
	diffs := d.DiffMain(chars1, chars2, false)
	diffs = d.DiffCharsToLines(diffs, lineArray)
	diffs = d.DiffCleanupSemantic(diffs)

	// Create patches from the diffs
	patches := d.PatchMake(previous, diffs)
	if len(patches) == 0 {
		return ""
	}

	return d.PatchToText(patches)
}

// ApplyLinePatch applies a line-level patch (produced by ComputeLinePatch) to a target string.
// Returns the patched string and true if all hunks applied cleanly.
// If any hunk fails to apply, returns the partially patched string and false.
func ApplyLinePatch(target, patch string) (string, bool) {
	d := dmp.New()

	patches, err := d.PatchFromText(patch)
	if err != nil || len(patches) == 0 {
		return target, false
	}

	result, applied := d.PatchApply(patches, target)

	// Check if all hunks applied
	allApplied := true
	for _, ok := range applied {
		if !ok {
			allApplied = false
			break
		}
	}

	return result, allApplied
}

// IsPatchableString returns true if the string may benefit from structured or
// line-level patching rather than wholesale replacement. This includes:
//   - Multi-line strings (for line-level text diff)
//   - JSON objects or arrays (for structural JSON diff)
//   - Strings with YAML structure (for structural YAML diff)
func IsPatchableString(s string) bool {
	if IsMultiLineString(s) {
		return true
	}
	trimmed := strings.TrimSpace(s)
	// JSON objects or arrays
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	return false
}

// isMapOrArray returns true if the document's top-level node is a YAML map or array.
func isMapOrArray(doc *gaby.YamlDoc) bool {
	return len(doc.ChildrenMap()) > 0 || doc.IsArray()
}

// IsMultiLineString returns true if the string contains embedded newlines
// (not just a trailing newline), indicating it is a multi-line string value
// that may benefit from line-level diffing. YAML scalar serialization via
// gaby appends a trailing newline, so a single-line value like "alice" becomes
// "alice\n" — this function correctly returns false for such values.
func IsMultiLineString(s string) bool {
	return strings.ContainsRune(strings.TrimRight(s, "\n"), '\n')
}
