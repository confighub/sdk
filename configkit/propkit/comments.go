// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package propkit

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
)

// extractPropertiesComments parses raw Properties source line-by-line and returns
// a flat map of comment keys to comment text. Properties comments start with '#' or '!'.
// Since properties files use dot-separated keys for nesting (e.g., database.host=localhost),
// comments are associated with the leaf key within the appropriate nested path.
func extractPropertiesComments(source []byte) map[string]string {
	comments := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(source))

	var pendingComments []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			text := trimmed[1:]
			text = strings.TrimPrefix(text, " ")
			pendingComments = append(pendingComments, text)
			continue
		}

		// Handle line continuations
		for strings.HasSuffix(trimmed, "\\") {
			trimmed = trimmed[:len(trimmed)-1]
			if scanner.Scan() {
				trimmed += strings.TrimSpace(scanner.Text())
			}
		}

		// Parse key from key=value or key:value
		key := parsePropertyKey(trimmed)
		if key == "" {
			continue
		}

		// Detect inline comment from raw value
		rawValue := extractPropertyValue(trimmed)
		if _, inlineComment := yamlkit.SplitInlineComment(rawValue); inlineComment != "" {
			parts := strings.Split(key, ".")
			targetKey := parts[len(parts)-1]
			parentPath := strings.Join(parts[:len(parts)-1], ".")
			commentKey := yamlkit.CommentKey(yamlkit.CommentLine, targetKey)
			if parentPath != "" {
				comments[parentPath+"."+commentKey] = inlineComment
			} else {
				comments[commentKey] = inlineComment
			}
		}

		if len(pendingComments) > 0 {
			commentText := strings.Join(pendingComments, "\n")
			// Properties use dotted keys like "database.host". The comment belongs
			// as a head comment on the leaf key, inside the parent path.
			parts := strings.Split(key, ".")
			targetKey := parts[len(parts)-1]
			parentPath := strings.Join(parts[:len(parts)-1], ".")
			commentKey := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
			if parentPath != "" {
				comments[parentPath+"."+commentKey] = commentText
			} else {
				comments[commentKey] = commentText
			}
			pendingComments = nil
		}
	}

	// Trailing comments
	if len(pendingComments) > 0 {
		commentText := strings.Join(pendingComments, "\n")
		commentKey := yamlkit.CommentKey(yamlkit.CommentFoot, "")
		comments[commentKey] = commentText
	}

	return comments
}

// extractPropertyValue extracts the value portion from a properties line.
func extractPropertyValue(line string) string {
	for i, ch := range line {
		if (ch == '=' || ch == ':') && (i == 0 || line[i-1] != '\\') {
			return strings.TrimSpace(line[i+1:])
		}
	}
	return ""
}

// parsePropertyKey extracts the key from a properties line (key=value or key:value).
func parsePropertyKey(line string) string {
	for i, ch := range line {
		if (ch == '=' || ch == ':') && (i == 0 || line[i-1] != '\\') {
			return strings.TrimSpace(line[:i])
		}
	}
	return ""
}

// injectPropertiesComments post-processes Properties encoder output to re-insert comments.
func injectPropertiesComments(propsOutput []byte, comments map[string]string) []byte {
	if len(comments) == 0 {
		return propsOutput
	}

	headComments := make(map[string]string)
	lineComments := make(map[string]string)
	var footComment string

	for commentPath, text := range comments {
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

		if target == "" && ct == yamlkit.CommentFoot {
			footComment = text
			continue
		}

		var fullPath string
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

	lines := strings.Split(string(propsOutput), "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		key := parsePropertyKey(trimmed)
		if key != "" {
			if text, ok := headComments[key]; ok {
				result = yamlkit.AppendCommentLines(result, "", text)
				delete(headComments, key)
			}
			if text, ok := lineComments[key]; ok {
				line = line + " # " + text
				delete(lineComments, key)
			}
		}
		result = append(result, line)
	}

	if footComment != "" {
		result = yamlkit.AppendCommentLines(result, "", footComment)
	}

	return []byte(strings.Join(result, "\n"))
}
