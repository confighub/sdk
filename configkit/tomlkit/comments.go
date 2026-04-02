// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package tomlkit

import (
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/pelletier/go-toml/v2/unstable"
)

// extractTOMLComments parses raw TOML source using the pelletier/go-toml/v2 AST parser
// and returns a flat map of comment keys to comment text. The keys use dot-separated
// paths to indicate where in the nested structure the comment belongs.
//
// For example, given:
//
//	# App comment
//	[database]
//	# Connection info
//	server = "192.168.1.1"  # main server
//
// Returns:
//
//	"$comment$head$database"          -> "App comment"
//	"database.$comment$head$server"   -> "Connection info"
//	"database.$comment$line$server"   -> "main server"
func extractTOMLComments(source []byte) map[string]string {
	comments := make(map[string]string)

	p := unstable.Parser{KeepComments: true}
	p.Reset(source)

	// currentSection tracks the dot-separated path of the current [table] or [[array_table]].
	var currentSection []string
	// pendingComments accumulates standalone comment lines before the next expression.
	var pendingComments []string
	// arrayTableCounts tracks how many times each [[array_table]] path has been seen,
	// so we can associate comments with the correct array index.
	arrayTableCounts := make(map[string]int)

	for p.NextExpression() {
		e := p.Expression()

		switch e.Kind {
		case unstable.Comment:
			text := stripCommentPrefix(string(e.Data))
			pendingComments = append(pendingComments, text)

		case unstable.Table:
			keys := nodeKeys(e)
			if len(keys) == 0 {
				continue
			}

			// The comment before a [table] header is stored as a head comment on the
			// last key segment, placed inside the parent's map level.
			if len(pendingComments) > 0 {
				commentText := strings.Join(pendingComments, "\n")
				targetKey := keys[len(keys)-1]
				parentPath := keys[:len(keys)-1]
				commentKey := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
				storePath := joinPath(parentPath, commentKey)
				comments[storePath] = commentText
				pendingComments = nil
			}

			currentSection = keys

		case unstable.ArrayTable:
			keys := nodeKeys(e)
			if len(keys) == 0 {
				continue
			}

			arrayPath := strings.Join(keys, ".")
			index := arrayTableCounts[arrayPath]
			arrayTableCounts[arrayPath]++

			if len(pendingComments) > 0 {
				commentText := strings.Join(pendingComments, "\n")
				if index == 0 {
					// First occurrence: comments become a head comment on the array key at the parent level.
					targetKey := keys[len(keys)-1]
					parentPath := keys[:len(keys)-1]
					commentKey := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
					storePath := joinPath(parentPath, commentKey)
					comments[storePath] = commentText
				} else {
					// Subsequent entries: store as a self-referential head comment inside the element.
					commentKey := yamlkit.CommentKey(yamlkit.CommentHead, "")
					storePath := joinPath(keys, indexSegment(index), commentKey)
					comments[storePath] = commentText
				}
				pendingComments = nil
			}

			// For key lookups within this array table entry, set currentSection
			// to include the array index.
			currentSection = append(keys, indexSegment(index))

		case unstable.KeyValue:
			keys := nodeKeys(e)
			if len(keys) == 0 {
				continue
			}

			// Head comment (standalone comment lines before this key=value).
			if len(pendingComments) > 0 {
				commentText := strings.Join(pendingComments, "\n")
				targetKey := keys[len(keys)-1]
				parentPath := append(currentSection, keys[:len(keys)-1]...)
				commentKey := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
				storePath := joinPath(parentPath, commentKey)
				comments[storePath] = commentText
				pendingComments = nil
			}

			// Inline comment (text after value on the same line).
			inlineComment := extractInlineComment(source, e)
			if inlineComment != "" {
				targetKey := keys[len(keys)-1]
				parentPath := append(currentSection, keys[:len(keys)-1]...)
				commentKey := yamlkit.CommentKey(yamlkit.CommentLine, targetKey)
				storePath := joinPath(parentPath, commentKey)
				comments[storePath] = inlineComment
			}
		}
	}

	// Any trailing comments after the last expression become a foot comment
	// on the document root.
	if len(pendingComments) > 0 {
		commentText := strings.Join(pendingComments, "\n")
		commentKey := yamlkit.CommentKey(yamlkit.CommentFoot, "")
		comments[commentKey] = commentText
	}

	return comments
}

// convertTypedSlices recursively converts []map[string]any to []any so the
// generic comment merge/extract functions can handle them.
func convertTypedSlices(data any) any {
	switch v := data.(type) {
	case map[string]any:
		for key, val := range v {
			v[key] = convertTypedSlices(val)
		}
		return v
	case []map[string]any:
		result := make([]any, len(v))
		for i, val := range v {
			result[i] = convertTypedSlices(val)
		}
		return result
	case []any:
		for i, val := range v {
			v[i] = convertTypedSlices(val)
		}
		return v
	default:
		return data
	}
}

// injectTOMLComments post-processes TOML encoder output to re-insert comments.
// It scans the output line by line, matching [section] headers and key = value lines
// to their comment entries.
func injectTOMLComments(tomlOutput []byte, comments map[string]string) []byte {
	if len(comments) == 0 {
		return tomlOutput
	}

	// Build lookup structures indexed by the TOML path that the comment is attached to.
	// headComments maps "section.key" -> text for comments before a key/section.
	// lineComments maps "section.key" -> text for inline comments.
	// footComment holds the document-level foot comment.
	headComments := make(map[string]string)
	lineComments := make(map[string]string)
	selfHeadComments := make(map[string]string) // for array table elements: path.$comment$head$
	var footComment string

	for commentPath, text := range comments {
		// Split the comment path to find the comment key portion.
		lastDot := strings.LastIndex(commentPath, ".$comment$")
		var parentPath, commentKey string
		if lastDot >= 0 {
			parentPath = commentPath[:lastDot]
			commentKey = commentPath[lastDot+1:]
		} else if strings.HasPrefix(commentPath, "$comment$") {
			parentPath = ""
			commentKey = commentPath
		} else {
			continue
		}

		ct, target, ok := yamlkit.ParseCommentKey(commentKey)
		if !ok {
			continue
		}

		var fullPath string
		if target == "" {
			// Self-referential comment (document-level or array element).
			if ct == yamlkit.CommentFoot {
				footComment = text
			} else {
				selfHeadComments[parentPath] = text
			}
			continue
		}
		if parentPath != "" {
			fullPath = parentPath + "." + target
		} else {
			fullPath = target
		}

		switch ct {
		case yamlkit.CommentHead:
			headComments[fullPath] = text
		case yamlkit.CommentLine:
			lineComments[fullPath] = text
		}
	}

	lines := strings.Split(string(tomlOutput), "\n")
	var result []string
	var currentSection string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := yamlkit.LeadingWhitespace(line)

		if isTableHeader(trimmed) {
			tablePath := parseTablePath(trimmed)

			// Check for head comment on this table.
			if text, ok := headComments[tablePath]; ok {
				result = yamlkit.AppendCommentLines(result, indent, text)
				delete(headComments, tablePath)
			}

			result = append(result, line)
			currentSection = tablePath

		} else if isArrayTableHeader(trimmed) {
			tablePath := parseArrayTablePath(trimmed)

			// Array table elements use self-referential head comments.
			// The comment path includes the array index, but the TOML header doesn't.
			// Look for self-head comments for this array path.
			for path, text := range selfHeadComments {
				// Match paths like "servers.0" for [[servers]]
				if strings.HasPrefix(path, tablePath+".") {
					result = yamlkit.AppendCommentLines(result, indent, text)
					delete(selfHeadComments, path)
					break // Only one comment per array table header
				}
			}

			result = append(result, line)
			currentSection = tablePath

		} else if key, ok := parseKeyLine(trimmed); ok {
			var fullPath string
			if currentSection != "" {
				fullPath = currentSection + "." + key
			} else {
				fullPath = key
			}

			// Insert head comment before the key line.
			if text, ok := headComments[fullPath]; ok {
				result = yamlkit.AppendCommentLines(result, indent, text)
				delete(headComments, fullPath)
			}

			// Append inline comment.
			if text, ok := lineComments[fullPath]; ok {
				line = line + " # " + text
				delete(lineComments, fullPath)
			}

			result = append(result, line)
		} else {
			result = append(result, line)
		}
	}

	// Append foot comment at the end.
	if footComment != "" {
		result = yamlkit.AppendCommentLines(result, "", footComment)
	}

	return []byte(strings.Join(result, "\n"))
}

// --- helper functions ---

// nodeKeys extracts the key components from a Table, ArrayTable, or KeyValue node.
func nodeKeys(n *unstable.Node) []string {
	var keys []string
	it := n.Key()
	for it.Next() {
		keys = append(keys, string(it.Node().Data))
	}
	return keys
}

// stripCommentPrefix removes the leading "# " or "#" from a TOML comment line.
func stripCommentPrefix(s string) string {
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimPrefix(s, " ")
	return s
}

// extractInlineComment finds inline comment text after a KeyValue expression's raw range.
func extractInlineComment(source []byte, e *unstable.Node) string {
	end := int(e.Raw.Offset + e.Raw.Length)
	// Scan forward to end of line looking for '#'.
	for i := end; i < len(source) && source[i] != '\n'; i++ {
		if source[i] == '#' {
			comment := strings.TrimSpace(string(source[i+1:]))
			// Find end of line
			for j := i + 1; j < len(source); j++ {
				if source[j] == '\n' {
					comment = strings.TrimSpace(string(source[i+1 : j]))
					break
				}
			}
			return comment
		}
	}
	return ""
}

// joinPath joins path segments with dots, filtering empty strings.
func joinPath(parts ...any) string {
	var segments []string
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			if v != "" {
				segments = append(segments, v)
			}
		case []string:
			for _, s := range v {
				if s != "" {
					segments = append(segments, s)
				}
			}
		}
	}
	return strings.Join(segments, ".")
}

// indexSegment returns the string representation of an array index for use in paths.
func indexSegment(i int) string {
	return strconv.Itoa(i)
}

// isTableHeader checks if a line is a TOML table header like [database].
func isTableHeader(line string) bool {
	return strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]") && !strings.HasSuffix(line, "]]")
}

// isArrayTableHeader checks if a line is a TOML array table header like [[servers]].
func isArrayTableHeader(line string) bool {
	return strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]")
}

// parseTablePath extracts the table path from a [table.path] header line.
func parseTablePath(line string) string {
	// Strip [ and ]
	return strings.TrimSpace(line[1 : len(line)-1])
}

// parseArrayTablePath extracts the table path from a [[table.path]] header line.
func parseArrayTablePath(line string) string {
	// Strip [[ and ]]
	return strings.TrimSpace(line[2 : len(line)-2])
}

// parseKeyLine extracts the key name from a TOML key = value line.
// Returns the key and true if the line is a key-value line.
func parseKeyLine(line string) (string, bool) {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
		return "", false
	}
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return "", false
	}
	key := strings.TrimSpace(line[:eqIdx])
	// Handle quoted keys
	if strings.HasPrefix(key, "\"") && strings.HasSuffix(key, "\"") {
		key = key[1 : len(key)-1]
	}
	if key == "" {
		return "", false
	}
	return key, true
}

