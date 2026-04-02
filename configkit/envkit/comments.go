// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package envkit

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
)

// extractEnvComments parses raw env file source line-by-line and returns a flat
// map of comment keys to comment text. Env comments start with '#'.
// Since env files use flat KEY=value format (no nesting except configHub metadata),
// comments are associated with root-level keys.
func extractEnvComments(source []byte) map[string]string {
	comments := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(source))

	var pendingComments []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			text := strings.TrimPrefix(trimmed, "#")
			text = strings.TrimPrefix(text, " ")
			pendingComments = append(pendingComments, text)
			continue
		}

		// KEY=value line
		eqIdx := strings.Index(trimmed, "=")
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eqIdx])
		if key == "" {
			continue
		}

		// Detect inline comment
		value := strings.TrimSpace(trimmed[eqIdx+1:])
		value = stripQuotesForComments(value)
		if _, inlineComment := yamlkit.SplitInlineComment(value); inlineComment != "" {
			commentKey := yamlkit.CommentKey(yamlkit.CommentLine, key)
			comments[commentKey] = inlineComment
		}

		if len(pendingComments) > 0 {
			commentText := strings.Join(pendingComments, "\n")
			// Env keys are flat. configHub keys use dots but we treat the whole
			// key as the target since configHub metadata nests into the map.
			if strings.Contains(key, ".") {
				// Dotted key (configHub metadata): split into parent path + leaf
				parts := strings.Split(key, ".")
				targetKey := parts[len(parts)-1]
				parentPath := strings.Join(parts[:len(parts)-1], ".")
				commentKey := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
				comments[parentPath+"."+commentKey] = commentText
			} else {
				commentKey := yamlkit.CommentKey(yamlkit.CommentHead, key)
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

// stripQuotesForComments removes surrounding quotes for inline comment detection.
func stripQuotesForComments(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// injectEnvComments post-processes env encoder output to re-insert comments.
func injectEnvComments(envOutput []byte, comments map[string]string) []byte {
	if len(comments) == 0 {
		return envOutput
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

		// For env files, the key in the output is the flat env var name.
		// configHub keys are dotted in the env file (e.g., configHub.configName=...).
		var fullKey string
		if parentPath != "" {
			fullKey = parentPath + "." + target
		} else {
			fullKey = target
		}

		switch ct {
		case yamlkit.CommentHead:
			headComments[fullKey] = text
		case yamlkit.CommentLine:
			lineComments[fullKey] = text
		}
	}

	lines := strings.Split(string(envOutput), "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if eqIdx := strings.Index(trimmed, "="); eqIdx > 0 {
			key := strings.TrimSpace(trimmed[:eqIdx])
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
