// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package inikit

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
)

// extractINIComments parses raw INI source line-by-line and returns a flat map
// of comment keys to comment text. INI comments start with '#' or ';'.
func extractINIComments(source []byte) map[string]string {
	comments := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(source))

	var currentSection string
	var pendingComments []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			text := strings.TrimPrefix(trimmed, "#")
			text = strings.TrimPrefix(text, ";")
			text = strings.TrimPrefix(text, " ")
			pendingComments = append(pendingComments, text)
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionName := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if len(pendingComments) > 0 {
				commentText := strings.Join(pendingComments, "\n")
				// Section names may be dotted (e.g., "database.ssl").
				// Comment goes as head comment on the last segment at the parent level.
				parts := strings.Split(sectionName, ".")
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
			currentSection = sectionName
			continue
		}

		// Key = value line
		eqIdx := strings.Index(trimmed, "=")
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eqIdx])
		if key == "" {
			continue
		}

		if len(pendingComments) > 0 {
			commentText := strings.Join(pendingComments, "\n")
			commentKey := yamlkit.CommentKey(yamlkit.CommentHead, key)
			if currentSection != "" {
				comments[currentSection+"."+commentKey] = commentText
			} else {
				comments[commentKey] = commentText
			}
			pendingComments = nil
		}

		// Check for inline comment after value
		value := strings.TrimSpace(trimmed[eqIdx+1:])
		if idx := strings.Index(value, " #"); idx >= 0 {
			inlineText := strings.TrimSpace(value[idx+2:])
			commentKey := yamlkit.CommentKey(yamlkit.CommentLine, key)
			if currentSection != "" {
				comments[currentSection+"."+commentKey] = inlineText
			} else {
				comments[commentKey] = inlineText
			}
		} else if idx := strings.Index(value, " ;"); idx >= 0 {
			inlineText := strings.TrimSpace(value[idx+2:])
			commentKey := yamlkit.CommentKey(yamlkit.CommentLine, key)
			if currentSection != "" {
				comments[currentSection+"."+commentKey] = inlineText
			} else {
				comments[commentKey] = inlineText
			}
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

// injectINIComments post-processes INI encoder output to re-insert comments.
func injectINIComments(iniOutput []byte, comments map[string]string) []byte {
	if len(comments) == 0 {
		return iniOutput
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

	lines := strings.Split(string(iniOutput), "\n")
	var result []string
	var currentSection string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionPath := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if text, ok := headComments[sectionPath]; ok {
				result = yamlkit.AppendCommentLines(result, "", text)
				delete(headComments, sectionPath)
			}
			result = append(result, line)
			currentSection = sectionPath
		} else if eqIdx := strings.Index(trimmed, "="); eqIdx > 0 && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";") {
			key := strings.TrimSpace(trimmed[:eqIdx])
			var fullPath string
			if currentSection != "" {
				fullPath = currentSection + "." + key
			} else {
				fullPath = key
			}

			if text, ok := headComments[fullPath]; ok {
				indent := yamlkit.LeadingWhitespace(line)
				result = yamlkit.AppendCommentLines(result, indent, text)
				delete(headComments, fullPath)
			}

			if text, ok := lineComments[fullPath]; ok {
				line = line + " # " + text
				delete(lineComments, fullPath)
			}

			result = append(result, line)
		} else {
			result = append(result, line)
		}
	}

	if footComment != "" {
		result = yamlkit.AppendCommentLines(result, "", footComment)
	}

	return []byte(strings.Join(result, "\n"))
}
