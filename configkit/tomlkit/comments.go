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
