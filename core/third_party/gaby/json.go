package gaby

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// JSON is YAML flow syntax, but a YAML parser is not a faithful JSON parser:
// it rejects surrogate-pair escapes, which JSON encoders write for characters
// outside the Basic Multilingual Plane, and accepts much that JSON doesn't. So
// JSON is read with encoding/json's tokenizer, and the YAML nodes are built
// from its tokens. A number token is kept as the text it was written as, so
// that neither its precision nor its spelling is lost.

// ParseJSON reads a JSON value, which may be an object, an array or a scalar,
// and returns a *YamlDoc. Object members keep their order, and numbers keep
// their text: 1.10 stays 1.10, and 12345678901234567890 is not rounded.
func ParseJSON(data []byte) (*YamlDoc, error) {
	node, err := JSONToNode(data)
	if err != nil {
		return nil, err
	}
	return &YamlDoc{node: yaml.NewRNode(node)}, nil
}

// JSONToNode reads a JSON value and returns it as a YAML node. Strings become
// !!str scalars with no style, so the YAML encoder quotes only those that
// need it, and objects and arrays are block style.
func JSONToNode(data []byte) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err == io.EOF {
		return nil, errors.New("no JSON value")
	}
	if err != nil {
		return nil, err
	}
	node, err := jsonTokenToNode(dec, tok)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected data after the JSON value at offset %d", dec.InputOffset())
	}
	return node, nil
}

func jsonTokenToNode(dec *json.Decoder, tok json.Token) (*yaml.Node, error) {
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			node := &yaml.Node{Kind: yaml.MappingNode, Tag: yaml.NodeTagMap}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("object key %v is not a string", keyTok)
				}
				valueTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				value, err := jsonTokenToNode(dec, valueTok)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, NewStringNode(key), value)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return node, nil
		case '[':
			node := &yaml.Node{Kind: yaml.SequenceNode, Tag: yaml.NodeTagSeq}
			for dec.More() {
				elemTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				elem, err := jsonTokenToNode(dec, elemTok)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, elem)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return node, nil
		}
		return nil, fmt.Errorf("unexpected %v", v)
	case string:
		return NewStringNode(v), nil
	case json.Number:
		text := string(v)
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: jsonNumberTag(text), Value: text}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: yaml.NodeTagBool, Value: strconv.FormatBool(v)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: yaml.NodeTagNull, Value: "null"}, nil
	}
	return nil, fmt.Errorf("unexpected JSON token %v", tok)
}

// NewStringNode returns a !!str scalar node for s. The YAML encoder quotes a
// string that YAML 1.2 would read as another type. A string that only YAML
// 1.1 would, such as yes, off or 1:20, is double-quoted here, so that readers
// of either version see a string, and so is <<, which as a key would be a
// merge key.
func NewStringNode(s string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: yaml.NodeTagString, Value: s}
	if s == "<<" || isYAML11NonString(s) {
		node.Style = yaml.DoubleQuotedStyle
	}
	node.Style |= stringStyle(s)
	return node
}

// stringStyle returns the style a string scalar needs for the YAML encoder
// to write it readably. The encoder writes a string with a newline as a
// literal block, which the parser rejects if its first line starts with a
// tab, so such a string is double-quoted.
func stringStyle(s string) yaml.Style {
	if strings.HasPrefix(s, "\t") && strings.Contains(s, "\n") {
		return yaml.DoubleQuotedStyle
	}
	return 0
}

var yaml11Base60 = regexp.MustCompile(`^[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+(?:\.[0-9_]*)?$`)

func isYAML11NonString(s string) bool {
	switch s {
	case "y", "Y", "yes", "Yes", "YES", "n", "N", "no", "No", "NO",
		"on", "On", "ON", "off", "Off", "OFF":
		return true
	}
	return strings.Contains(s, ":") && yaml11Base60.MatchString(s)
}

// jsonNumberTag returns the YAML tag of a JSON number: !!int for an integer
// that YAML reads as one, which is any that fits in 64 bits, and !!float for
// anything else, so that the YAML encoder writes the number untagged.
func jsonNumberTag(text string) string {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '.', 'e', 'E':
			return yaml.NodeTagFloat
		}
	}
	probe := yaml.Node{Kind: yaml.ScalarNode, Value: text}
	if probe.ShortTag() == yaml.NodeTagInt {
		return yaml.NodeTagInt
	}
	return yaml.NodeTagFloat
}

// NodeToJSON writes a YAML node as JSON, indenting each level by indent, or
// compactly if indent is empty. Mapping keys keep their order. A number
// whose YAML text is a valid JSON number is written as that text; any other
// number, such as 0x1F or 1_000, is written as the value YAML reads it as.
// Strings are written with only the escapes JSON requires, so < > and & are
// not escaped as they are by encoding/json. Aliases are written as the node
// they refer to, and merge keys (<<) are expanded. YAML values JSON can't
// represent, such as .inf, are an error.
func NodeToJSON(node *yaml.Node, indent string) ([]byte, error) {
	w := jsonWriter{indent: indent}
	if err := w.value(node, 0); err != nil {
		return nil, err
	}
	return w.buf.Bytes(), nil
}

type jsonWriter struct {
	buf    bytes.Buffer
	indent string
}

func (w *jsonWriter) newline(depth int) {
	if w.indent == "" {
		return
	}
	w.buf.WriteByte('\n')
	for i := 0; i < depth; i++ {
		w.buf.WriteString(w.indent)
	}
}

func (w *jsonWriter) value(node *yaml.Node, depth int) error {
	node = ResolveAlias(node)
	if node == nil {
		w.buf.WriteString("null")
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			w.buf.WriteString("null")
			return nil
		}
		return w.value(node.Content[0], depth)
	case yaml.MappingNode:
		entries, err := MappingEntries(node)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			w.buf.WriteString("{}")
			return nil
		}
		w.buf.WriteByte('{')
		for i := 0; i < len(entries); i += 2 {
			if i > 0 {
				w.buf.WriteByte(',')
			}
			w.newline(depth + 1)
			key := ResolveAlias(entries[i])
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: a mapping key that is not a scalar can't be written as JSON", key.Line)
			}
			writeJSONString(&w.buf, key.Value)
			w.buf.WriteByte(':')
			if w.indent != "" {
				w.buf.WriteByte(' ')
			}
			if err := w.value(entries[i+1], depth+1); err != nil {
				return err
			}
		}
		w.newline(depth)
		w.buf.WriteByte('}')
		return nil
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			w.buf.WriteString("[]")
			return nil
		}
		w.buf.WriteByte('[')
		for i, elem := range node.Content {
			if i > 0 {
				w.buf.WriteByte(',')
			}
			w.newline(depth + 1)
			if err := w.value(elem, depth+1); err != nil {
				return err
			}
		}
		w.newline(depth)
		w.buf.WriteByte(']')
		return nil
	case yaml.ScalarNode:
		return w.scalar(node)
	}
	return fmt.Errorf("line %d: unexpected YAML node kind %v", node.Line, node.Kind)
}

func (w *jsonWriter) scalar(node *yaml.Node) error {
	switch node.ShortTag() {
	case yaml.NodeTagNull:
		w.buf.WriteString("null")
	case yaml.NodeTagBool:
		var b bool
		if err := node.Decode(&b); err != nil {
			return fmt.Errorf("line %d: %w", node.Line, err)
		}
		w.buf.WriteString(strconv.FormatBool(b))
	case yaml.NodeTagInt:
		if IsJSONNumber(node.Value) {
			w.buf.WriteString(node.Value)
			return nil
		}
		var v any
		if err := node.Decode(&v); err != nil {
			return fmt.Errorf("line %d: %w", node.Line, err)
		}
		fmt.Fprint(&w.buf, v)
	case yaml.NodeTagFloat:
		if IsJSONNumber(node.Value) {
			w.buf.WriteString(node.Value)
			return nil
		}
		var f float64
		if err := node.Decode(&f); err != nil {
			return fmt.Errorf("line %d: %w", node.Line, err)
		}
		out, err := json.Marshal(f)
		if err != nil {
			return fmt.Errorf("line %d: %s can't be written as JSON", node.Line, node.Value)
		}
		w.buf.Write(out)
	default:
		// Strings, timestamps, binary data and scalars with tags of their own
		// are written as their text.
		writeJSONString(&w.buf, node.Value)
	}
	return nil
}

// IsJSONNumber reports whether s is a number as JSON writes one.
func IsJSONNumber(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(s)
}

const hexDigits = "0123456789abcdef"

// writeJSONString writes s as a JSON string. Invalid UTF-8 is replaced with
// U+FFFD, as encoding/json does, and U+2028 and U+2029 are escaped, since
// JavaScript doesn't allow them in string literals.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\':
				buf.WriteByte('\\')
				buf.WriteByte(c)
			case c == '\n':
				buf.WriteString(`\n`)
			case c == '\r':
				buf.WriteString(`\r`)
			case c == '\t':
				buf.WriteString(`\t`)
			case c == '\b':
				buf.WriteString(`\b`)
			case c == '\f':
				buf.WriteString(`\f`)
			case c < 0x20:
				buf.WriteString(`\u00`)
				buf.WriteByte(hexDigits[c>>4])
				buf.WriteByte(hexDigits[c&0xF])
			default:
				buf.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			buf.WriteString(`�`)
		case r == ' ' || r == ' ':
			buf.WriteString(`\u202`)
			buf.WriteByte(hexDigits[r&0xF])
		default:
			buf.WriteString(s[i : i+size])
		}
		i += size
	}
	buf.WriteByte('"')
}

// ResolveAlias returns the node an alias node refers to, following chains of
// aliases, or node itself if it isn't an alias.
func ResolveAlias(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

// MappingEntries returns the keys and values of a mapping node, alternating
// as in its Content, with merge keys (<<) expanded. A merged mapping's
// entries take the place of the merge key, except for keys the mapping sets
// itself; when a merge key lists several mappings, the first to set a key
// wins, as YAML specifies. Aliases among the values are left as they are.
func MappingEntries(node *yaml.Node) ([]*yaml.Node, error) {
	node = ResolveAlias(node)
	hasMerge := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		if isMergeKey(node.Content[i]) {
			hasMerge = true
			break
		}
	}
	if !hasMerge {
		return node.Content, nil
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if key := ResolveAlias(node.Content[i]); !isMergeKey(key) {
			seen[key.Value] = true
		}
	}
	var entries []*yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if !isMergeKey(key) {
			entries = append(entries, key, value)
			continue
		}
		value = ResolveAlias(value)
		var sources []*yaml.Node
		switch value.Kind {
		case yaml.MappingNode:
			sources = []*yaml.Node{value}
		case yaml.SequenceNode:
			sources = value.Content
		default:
			return nil, fmt.Errorf("line %d: a merge key's value has to be a mapping or a sequence of mappings", value.Line)
		}
		for _, source := range sources {
			source = ResolveAlias(source)
			if source.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("line %d: a merge key's value has to be a mapping or a sequence of mappings", source.Line)
			}
			merged, err := MappingEntries(source)
			if err != nil {
				return nil, err
			}
			for j := 0; j+1 < len(merged); j += 2 {
				mergedKey := ResolveAlias(merged[j])
				if seen[mergedKey.Value] {
					continue
				}
				seen[mergedKey.Value] = true
				entries = append(entries, merged[j], merged[j+1])
			}
		}
	}
	return entries, nil
}

const mergeTag = "!!merge"

func isMergeKey(key *yaml.Node) bool {
	key = ResolveAlias(key)
	return key.Kind == yaml.ScalarNode && key.Value == "<<" && key.ShortTag() == mergeTag
}
