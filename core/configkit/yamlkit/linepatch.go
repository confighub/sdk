// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	dmp "github.com/sergi/go-diff/diffmatchpatch"
	"sigs.k8s.io/kustomize/kyaml/yaml"
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
		// Read as YAML, which JSON is, so that the document keeps its keys in the order the
		// target has them. ParseJSON reads it through a map. JSON that YAML does not take, an
		// escaped "/" for one, is read as JSON and loses its order.
		targetDoc, err = gaby.ParseYAML([]byte(target))
		if err != nil {
			targetDoc, err = gaby.ParseJSON([]byte(target))
		}
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
	if patch.Format == PatchFormatJSON {
		out, err := jsonLaidOutLike(target, targetDoc)
		if err != nil {
			return target, false
		}
		return out, true
	}
	out, err := targetDoc.MarshalYAML()
	if err != nil {
		return target, false
	}
	return string(out), true
}

// jsonLaidOutLike writes doc as JSON laid out the way target is: in the order the document has its
// keys, indented as target's lines are indented, all on one line when target is, with a space
// after its colons and commas when target has one, and ending in a newline when target does. A patch changes values, so the rest of the text should read as it
// did: an embedded JSON file that came back compact, with its keys sorted, would show every line
// of it as changed.
func jsonLaidOutLike(target string, doc *gaby.YamlDoc) (string, error) {
	var compact bytes.Buffer
	if err := writeJSONNode(&compact, doc.YNode()); err != nil {
		return "", err
	}
	out := compact.String()
	if indent := jsonIndentation(target); indent != "" {
		var indented bytes.Buffer
		if err := json.Indent(&indented, compact.Bytes(), "", indent); err != nil {
			return "", err
		}
		out = indented.String()
	} else if colonSpace, commaSpace := jsonSeparatorSpacing(target); colonSpace || commaSpace {
		out = spaceJSONSeparators(out, colonSpace, commaSpace)
	}
	if strings.HasSuffix(target, "\n") {
		out += "\n"
	}
	return out, nil
}

// jsonIndentation is the indentation of the first indented line of a JSON text, and empty when no
// line is indented.
func jsonIndentation(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines[1:] {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed != "" && len(trimmed) < len(line) {
			return line[:len(line)-len(trimmed)]
		}
	}
	return ""
}

// jsonSeparatorSpacing reports whether a JSON text on one line follows its colons, and its
// commas, with a space, as {"a": 1, "b": 2} does, outside its strings.
func jsonSeparatorSpacing(text string) (colonSpace, commaSpace bool) {
	inString, escaped := false, false
	for i := 0; i+1 < len(text); i++ {
		c := text[i]
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && c == ':' && text[i+1] == ' ':
			colonSpace = true
		case !inString && c == ',' && text[i+1] == ' ':
			commaSpace = true
		}
	}
	return colonSpace, commaSpace
}

// spaceJSONSeparators puts a space after each colon, and each comma, of compact JSON that is not
// in a string.
func spaceJSONSeparators(compact string, colonSpace, commaSpace bool) string {
	var out strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(compact); i++ {
		c := compact[i]
		out.WriteByte(c)
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && c == ':' && colonSpace, !inString && c == ',' && commaSpace:
			out.WriteByte(' ')
		}
	}
	return out.String()
}

// writeJSONNode writes a YAML node as compact JSON, keeping the order of the keys of its
// mappings and writing numbers as the text has them, which encoding/json, given the decoded
// values, does not: it sorts the keys of a map, and writes 1.0 as 1. Characters HTML treats
// specially are written as they are, not escaped.
func writeJSONNode(buf *bytes.Buffer, node *yaml.Node) error {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			buf.WriteString("null")
			return nil
		}
		return writeJSONNode(buf, node.Content[0])
	case yaml.AliasNode:
		return writeJSONNode(buf, node.Alias)
	case yaml.MappingNode:
		buf.WriteByte('{')
		for i := 0; i+1 < len(node.Content); i += 2 {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSONValue(buf, node.Content[i].Value); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeJSONNode(buf, node.Content[i+1]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case yaml.SequenceNode:
		buf.WriteByte('[')
		for i, item := range node.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSONNode(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	}
	switch node.ShortTag() {
	case yaml.NodeTagString:
		return writeJSONValue(buf, node.Value)
	case yaml.NodeTagNull:
		buf.WriteString("null")
		return nil
	case yaml.NodeTagInt, yaml.NodeTagFloat, yaml.NodeTagBool:
		if json.Valid([]byte(node.Value)) {
			buf.WriteString(node.Value)
			return nil
		}
	}
	// A scalar JSON has no literal for, such as a YAML hexadecimal integer, is written as the
	// value it decodes to.
	var value any
	if err := node.Decode(&value); err != nil {
		return err
	}
	return writeJSONValue(buf, value)
}

func writeJSONValue(buf *bytes.Buffer, value any) error {
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	// Encode ends every value with a newline.
	buf.Truncate(buf.Len() - 1)
	return nil
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
