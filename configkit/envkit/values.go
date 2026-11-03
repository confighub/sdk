// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package envkit

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
)

// FormatValue returns value as it is written after KEY= in an env file.
// A value with whitespace or a character an env file reader treats specially
// is double-quoted, with backslashes, double quotes, dollar signs, backticks,
// newlines and carriage returns escaped; any other value is written as is.
// Escaping $ and ` keeps readers that expand variables or run commands, such
// as dotenv libraries and shells, from doing so with the value. ParseValue
// undoes it.
func FormatValue(value string) string {
	if !needsQuoting(value) {
		return value
	}
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '$':
			b.WriteString(`\$`)
		case '`':
			b.WriteString("\\`")
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ParseValue returns the value of the text after KEY= on an env file line,
// with the line's inline comment, if any, removed. A double-quoted value has
// its quotes removed and \\, \", \', \$, \`, \!, \n and \r unescaped; a
// backslash before any other character is kept, as a shell keeps it. A
// single-quoted value is taken literally, except that \' doesn't end it. A
// value that starts with a quote has to be one closed quoted string, followed
// by nothing but an optional comment; anything else is an error. In an
// unquoted value, " #" starts a comment. Variables are never expanded: a
// value is literal configuration.
func ParseValue(raw string) (string, error) {
	value, _, err := parseValue(raw)
	return value, err
}

// parseValue is ParseValue that also returns the inline comment's text.
func parseValue(raw string) (value, comment string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || (raw[0] != '"' && raw[0] != '\'') {
		value, comment = yamlkit.SplitInlineComment(raw)
		return value, comment, nil
	}
	value, rest, ok := splitQuoted(raw)
	if !ok {
		return "", "", fmt.Errorf("unterminated quoted value %s", raw)
	}
	rest = strings.TrimSpace(rest)
	switch {
	case rest == "":
		return value, "", nil
	case strings.HasPrefix(rest, "#"):
		return value, strings.TrimSpace(rest[1:]), nil
	default:
		return "", "", fmt.Errorf("unexpected text %q after the closing quote of %s", rest, raw[:len(raw)-len(rest)])
	}
}

// splitQuoted reads the quoted string at the start of s, returning its value
// and the text after its closing quote, or false if it isn't closed.
func splitQuoted(s string) (value, rest string, ok bool) {
	if s[0] == '\'' {
		for i := 1; i < len(s); i++ {
			if s[i] == '\'' && s[i-1] != '\\' {
				return s[1:i], s[i+1:], true
			}
		}
		return "", "", false
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			return b.String(), s[i+1:], true
		case c == '\\' && i+1 < len(s):
			i++
			switch next := s[i]; next {
			case '\\', '"', '\'', '$', '`', '!':
				b.WriteByte(next)
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte('\\')
				b.WriteByte(next)
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", "", false
}

// needsQuoting reports whether a value has to be quoted to be read back as is.
func needsQuoting(value string) bool {
	return strings.ContainsAny(value, " \t\r\n\"'\\#$`")
}
