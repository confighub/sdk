// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"strconv"
	"strings"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// CommentKeyPrefix is the prefix for all comment keys in the data structure.
// Comment keys encode comments from native config formats (TOML, INI, HCL, etc.)
// as map entries so they survive conversion through formats like JSON that don't
// support comments natively.
const CommentKeyPrefix = "$comment$"

// CommentType represents the position of a comment relative to its anchor element.
type CommentType string

const (
	// CommentHead is a comment that appears on the line(s) above its anchor.
	CommentHead CommentType = "head"
	// CommentLine is an inline comment on the same line as its anchor.
	CommentLine CommentType = "line"
	// CommentFoot is a comment that appears on the line(s) below its anchor.
	CommentFoot CommentType = "foot"
)

// CommentKey builds a comment map key from a comment type and target key name.
// For example, CommentKey(CommentHead, "server") returns "$comment$head$server".
// An empty targetKey refers to the containing object/document itself.
func CommentKey(ct CommentType, targetKey string) string {
	return CommentKeyPrefix + string(ct) + "$" + targetKey
}

// ParseCommentKey extracts the comment type and target key from a comment map key.
// Returns ("", "", false) if the key is not a comment key.
func ParseCommentKey(key string) (CommentType, string, bool) {
	if !strings.HasPrefix(key, CommentKeyPrefix) {
		return "", "", false
	}
	rest := key[len(CommentKeyPrefix):]
	// Split into at most 2 parts: type and target (target may contain '$')
	parts := strings.SplitN(rest, "$", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	ct := CommentType(parts[0])
	switch ct {
	case CommentHead, CommentLine, CommentFoot:
		return ct, parts[1], true
	default:
		return "", "", false
	}
}

// IsCommentKey returns true if the key matches the $comment$TYPE$TARGET pattern
// where TYPE is head, line, or foot. Equivalent to gaby.IsCommentKey.
func IsCommentKey(key string) bool {
	_, _, ok := ParseCommentKey(key)
	return ok
}

// StripCommentKeys returns a deep copy of data with all comment keys removed.
// Works recursively on map[string]any and []any.
// Non-map, non-slice values are returned as-is.
func StripCommentKeys(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, val := range v {
			if IsCommentKey(key) {
				continue
			}
			result[key] = StripCommentKeys(val)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			result[i] = StripCommentKeys(val)
		}
		return result
	default:
		return data
	}
}

// ExtractCommentKeys removes comment keys from the top level of a map, returning
// them as a separate map from comment key to comment text. The input map is
// modified in place.
func ExtractCommentKeys(data map[string]any) map[string]string {
	comments := make(map[string]string)
	for key, val := range data {
		if IsCommentKey(key) {
			if text, ok := val.(string); ok {
				comments[key] = text
			}
			delete(data, key)
		}
	}
	return comments
}

// InjectCommentKeys adds comment entries into a map.
func InjectCommentKeys(data map[string]any, comments map[string]string) {
	for key, text := range comments {
		data[key] = text
	}
}

// CommentKeysForDataKey returns the $comment$ keys that could be associated with
// a given data key (head, line, and foot).
func CommentKeysForDataKey(targetKey string) []string {
	return []string{
		CommentKey(CommentHead, targetKey),
		CommentKey(CommentLine, targetKey),
		CommentKey(CommentFoot, targetKey),
	}
}

// MergeCommentsIntoData walks a parsed data tree and inserts comment keys at the
// corresponding map levels based on a flat comment map. The flat map uses
// dot-separated paths to indicate where in the nested structure the comment belongs.
// For example, "database.$comment$head$server" places the comment key
// "$comment$head$server" inside the "database" map.
func MergeCommentsIntoData(data any, comments map[string]string) any {
	if len(comments) == 0 {
		return data
	}
	return mergeCommentsRecursive(data, "", comments)
}

func mergeCommentsRecursive(data any, currentPath string, comments map[string]string) any {
	switch v := data.(type) {
	case map[string]any:
		mergeCommentsIntoMap(v, currentPath, comments)
		return v
	case []any:
		for i, val := range v {
			childPath := currentPath + "." + indexSegment(i)
			if currentPath == "" {
				childPath = indexSegment(i)
			}
			v[i] = mergeCommentsRecursive(val, childPath, comments)
		}
		return v
	default:
		return data
	}
}

func mergeCommentsIntoMap(v map[string]any, currentPath string, comments map[string]string) {
	for commentPath, text := range comments {
		prefix := currentPath
		if prefix != "" {
			prefix += "."
		}
		if strings.HasPrefix(commentPath, prefix) {
			remainder := commentPath[len(prefix):]
			if !strings.Contains(remainder, ".") && IsCommentKey(remainder) {
				v[remainder] = text
			}
		} else if currentPath == "" && !strings.Contains(commentPath, ".") && IsCommentKey(commentPath) {
			v[commentPath] = text
		}
	}
	for key, val := range v {
		if IsCommentKey(key) {
			continue
		}
		childPath := key
		if currentPath != "" {
			childPath = currentPath + "." + key
		}
		v[key] = mergeCommentsRecursive(val, childPath, comments)
	}
}

// ExtractCommentsFromData recursively extracts all comment keys from a data tree,
// returning the cleaned data and a flat map of dot-path to comment text.
func ExtractCommentsFromData(data any) (any, map[string]string) {
	comments := make(map[string]string)
	cleaned := extractCommentsRecursive(data, "", comments)
	return cleaned, comments
}

func extractCommentsRecursive(data any, currentPath string, comments map[string]string) any {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, val := range v {
			if IsCommentKey(key) {
				if text, ok := val.(string); ok {
					commentPath := key
					if currentPath != "" {
						commentPath = currentPath + "." + key
					}
					comments[commentPath] = text
				}
				continue
			}
			childPath := key
			if currentPath != "" {
				childPath = currentPath + "." + key
			}
			result[key] = extractCommentsRecursive(val, childPath, comments)
		}
		return result
	case *orderedmap.OrderedMap[string, any]:
		result := orderedmap.New[string, any]()
		for pair := v.Oldest(); pair != nil; pair = pair.Next() {
			if IsCommentKey(pair.Key) {
				if text, ok := pair.Value.(string); ok {
					commentPath := pair.Key
					if currentPath != "" {
						commentPath = currentPath + "." + pair.Key
					}
					comments[commentPath] = text
				}
				continue
			}
			childPath := pair.Key
			if currentPath != "" {
				childPath = currentPath + "." + pair.Key
			}
			result.Set(pair.Key, extractCommentsRecursive(pair.Value, childPath, comments))
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			childPath := currentPath + "." + indexSegment(i)
			if currentPath == "" {
				childPath = indexSegment(i)
			}
			result[i] = extractCommentsRecursive(val, childPath, comments)
		}
		return result
	default:
		return data
	}
}

func indexSegment(i int) string {
	return strconv.Itoa(i)
}

// SplitInlineComment splits a value string into the clean value and inline comment.
// An inline comment is text after " #" (space-hash). Quoted values are not split.
// Returns the clean value and the comment text (without the # prefix).
// If there is no inline comment, comment is empty.
func SplitInlineComment(value string) (string, string) {
	// Don't split quoted values
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
		(value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value, ""
	}
	if idx := strings.Index(value, " #"); idx >= 0 {
		return strings.TrimSpace(value[:idx]), strings.TrimSpace(value[idx+2:])
	}
	return value, ""
}

// LeadingWhitespace returns the leading whitespace prefix of a line.
func LeadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// AppendCommentLines appends comment text as #-prefixed lines to the result slice,
// using the given indentation prefix to match surrounding lines.
func AppendCommentLines(result []string, indent string, text string) []string {
	for _, line := range strings.Split(text, "\n") {
		result = append(result, indent+"# "+line)
	}
	return result
}
