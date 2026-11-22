// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package tomlkit

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/pelletier/go-toml/v2/unstable"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// TOML is converted to YAML from its syntax tree rather than from decoded
// values, so that keys keep their order and values the text they are written
// as: 1.10 stays 1.10 and 0xFF stays 0xFF, which YAML reads as the same
// numbers. TOML writes infinity and NaN as inf and nan, which become YAML's
// .inf and .nan. Strings are double-quoted in YAML when YAML 1.1 would read
// them as another type.
//
// An offset date-time or a local date is a YAML timestamp. YAML has no local
// date-time or local time, so those, and any date-time YAML doesn't read as
// a timestamp, are tagged with one of the tags below. A tagged scalar reads
// as a string, as a timestamp does, and the tag tells YAMLToNative to write
// it back as a date-time.
const (
	TagDateTime      = "!toml/datetime"
	TagLocalDateTime = "!toml/local-datetime"
	TagLocalDate     = "!toml/local-date"
	TagLocalTime     = "!toml/local-time"
)

var dateTimeTags = map[unstable.Kind]string{
	unstable.DateTime:      TagDateTime,
	unstable.LocalDateTime: TagLocalDateTime,
	unstable.LocalDate:     TagLocalDate,
	unstable.LocalTime:     TagLocalTime,
}

const timestampTag = "!!timestamp"

// tomlToYAML converts a TOML document to a YAML document, with the source's
// comments as $comment$ keys next to the keys they belong to.
func tomlToYAML(data []byte) (*gaby.YamlDoc, error) {
	// The syntax tree doesn't check that tables and keys aren't defined
	// twice, so the document is decoded to check that it is valid TOML.
	var decoded any
	if _, err := toml.Decode(string(data), &decoded); err != nil {
		return nil, fmt.Errorf("error parsing TOML: %w", err)
	}

	comments := extractTOMLComments(data)
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: yaml.NodeTagMap}
	doc := gaby.Wrap(yaml.NewRNode(root))
	b := tomlYAMLBuilder{comments: comments}
	arrayCounts := map[string]int{}

	current := root
	// commentSection is the path extractTOMLComments files the comments of
	// the current table under, which includes the index of an array table's
	// element.
	var commentSection []string

	var p unstable.Parser
	p.Reset(data)
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table:
			keys := nodeKeys(e)
			parent, err := descend(root, keys[:len(keys)-1])
			if err != nil {
				return nil, err
			}
			b.addComment(parent, keys[:len(keys)-1], yamlkit.CommentHead, keys[len(keys)-1])
			table, err := descend(parent, keys[len(keys)-1:])
			if err != nil {
				return nil, err
			}
			current = table
			commentSection = keys
		case unstable.ArrayTable:
			keys := nodeKeys(e)
			arrayPath := strings.Join(keys, ".")
			index := arrayCounts[arrayPath]
			arrayCounts[arrayPath]++
			parent, err := descend(root, keys[:len(keys)-1])
			if err != nil {
				return nil, err
			}
			last := keys[len(keys)-1]
			if index == 0 {
				b.addComment(parent, keys[:len(keys)-1], yamlkit.CommentHead, last)
			}
			array := mappingValue(parent, last)
			if array == nil {
				array = &yaml.Node{Kind: yaml.SequenceNode, Tag: yaml.NodeTagSeq}
				parent.Content = append(parent.Content, gaby.NewStringNode(last), array)
			} else if array.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("%s is not an array of tables", arrayPath)
			}
			element := &yaml.Node{Kind: yaml.MappingNode, Tag: yaml.NodeTagMap}
			array.Content = append(array.Content, element)
			current = element
			commentSection = append(append([]string{}, keys...), strconv.Itoa(index))
			// Later elements' comments are self-referential head comments.
			b.addComment(element, commentSection, yamlkit.CommentHead, "")
		case unstable.KeyValue:
			keys := nodeKeys(e)
			commentParent := append(append([]string{}, commentSection...), keys[:len(keys)-1]...)
			last := keys[len(keys)-1]
			parent, err := descend(current, keys[:len(keys)-1])
			if err != nil {
				return nil, err
			}
			// BurntSushi accepts some documents that define a key twice.
			if mappingValue(parent, last) != nil {
				return nil, fmt.Errorf("error parsing TOML: %s is defined twice", strings.Join(append(commentParent, last), "."))
			}
			b.addComment(parent, commentParent, yamlkit.CommentHead, last)
			value, err := tomlValueToNode(e.Value())
			if err != nil {
				return nil, err
			}
			parent.Content = append(parent.Content, gaby.NewStringNode(last), value)
			b.addComment(parent, commentParent, yamlkit.CommentLine, last)
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("error parsing TOML: %w", err)
	}

	// The document's foot comment.
	b.addComment(root, nil, yamlkit.CommentFoot, "")
	return doc, nil
}

type tomlYAMLBuilder struct {
	comments map[string]string
}

// addComment adds the comment of type commentType on target, if
// extractTOMLComments found one, to mapping, which is the mapping it filed
// under the path commentPath.
func (b *tomlYAMLBuilder) addComment(mapping *yaml.Node, commentPath []string, commentType yamlkit.CommentType, target string) {
	commentKey := yamlkit.CommentKey(commentType, target)
	flat := flatCommentPath(strings.Join(commentPath, "."), commentKey)
	text, ok := b.comments[flat]
	if !ok {
		return
	}
	delete(b.comments, flat)
	mapping.Content = append(mapping.Content, gaby.NewStringNode(commentKey), gaby.NewStringNode(text))
}

// mappingValue returns the value of key in a mapping node, or nil.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// descend returns the table at keys under mapping, creating the tables that
// don't exist. A key naming an array of tables refers to its last element,
// as in TOML.
func descend(mapping *yaml.Node, keys []string) (*yaml.Node, error) {
	for _, key := range keys {
		next := mappingValue(mapping, key)
		switch {
		case next == nil:
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: yaml.NodeTagMap}
			mapping.Content = append(mapping.Content, gaby.NewStringNode(key), next)
		case next.Kind == yaml.SequenceNode && len(next.Content) > 0 && next.Content[len(next.Content)-1].Kind == yaml.MappingNode:
			next = next.Content[len(next.Content)-1]
		case next.Kind != yaml.MappingNode:
			return nil, fmt.Errorf("%s is not a table", key)
		}
		mapping = next
	}
	return mapping, nil
}

// tomlValueToNode returns the YAML node of a TOML value.
func tomlValueToNode(n *unstable.Node) (*yaml.Node, error) {
	text := string(n.Data)
	switch n.Kind {
	case unstable.String:
		return gaby.NewStringNode(text), nil
	case unstable.Bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: yaml.NodeTagBool, Value: text}, nil
	case unstable.Integer:
		return numberNode(text, yaml.NodeTagInt), nil
	case unstable.Float:
		switch text {
		case "inf", "+inf":
			text = ".inf"
		case "-inf":
			text = "-.inf"
		case "nan", "+nan", "-nan":
			text = ".nan"
		}
		return numberNode(text, yaml.NodeTagFloat), nil
	case unstable.DateTime, unstable.LocalDateTime, unstable.LocalDate, unstable.LocalTime:
		tag := dateTimeTags[n.Kind]
		if n.Kind == unstable.DateTime || n.Kind == unstable.LocalDate {
			probe := yaml.Node{Kind: yaml.ScalarNode, Value: text}
			if probe.ShortTag() == timestampTag {
				tag = timestampTag
			}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: text}, nil
	case unstable.Array:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: yaml.NodeTagSeq}
		for it := n.Children(); it.Next(); {
			elem, err := tomlValueToNode(it.Node())
			if err != nil {
				return nil, err
			}
			seq.Content = append(seq.Content, elem)
		}
		return seq, nil
	case unstable.InlineTable:
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: yaml.NodeTagMap}
		for it := n.Children(); it.Next(); {
			kv := it.Node()
			keys := nodeKeys(kv)
			parent, err := descend(mapping, keys[:len(keys)-1])
			if err != nil {
				return nil, err
			}
			if mappingValue(parent, keys[len(keys)-1]) != nil {
				return nil, fmt.Errorf("%s is defined twice in an inline table", strings.Join(keys, "."))
			}
			value, err := tomlValueToNode(kv.Value())
			if err != nil {
				return nil, err
			}
			parent.Content = append(parent.Content, gaby.NewStringNode(keys[len(keys)-1]), value)
		}
		return mapping, nil
	}
	return nil, fmt.Errorf("unexpected TOML value %q", text)
}

// numberNode returns a number scalar with the given tag. YAML reads TOML's
// integers, including 0x, 0o and 0b ones, and floats as numbers, ignoring
// underscores; the underscores are removed only if YAML wouldn't otherwise
// read the text as a number of the right type.
func numberNode(text, tag string) *yaml.Node {
	probe := yaml.Node{Kind: yaml.ScalarNode, Value: text}
	if probe.ShortTag() != tag {
		text = strings.ReplaceAll(text, "_", "")
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: text}
}

// yamlToTOML writes a YAML document as TOML. Keys keep their order, except
// that within each table the keys with plain values come before the
// sub-tables, as TOML requires. A sequence of mappings is an array of
// tables. Numbers and date-times are written as their YAML text when that is
// valid TOML of the same type, and otherwise as the value YAML reads them
// as. TOML has no null, so a null value is an error. $comment$ keys are
// written as comments.
func yamlToTOML(doc *gaby.YamlDoc) ([]byte, error) {
	root := gaby.ResolveAlias(doc.YNode())
	if root == nil || (root.Kind == yaml.ScalarNode && root.ShortTag() == yaml.NodeTagNull) {
		return []byte{}, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("a TOML document has to be a mapping of keys and tables")
	}
	var w tomlWriter
	if err := w.table(nil, root, "", ""); err != nil {
		return nil, err
	}
	out := w.buf.Bytes()
	// Check the result, so that a value whose text isn't valid TOML, such as
	// an invalid date-time with a tag of ours, is an error rather than an
	// unreadable document.
	var decoded any
	if _, err := toml.Decode(string(out), &decoded); err != nil {
		return nil, fmt.Errorf("the YAML can't be written as TOML: %w", err)
	}
	return out, nil
}

type tomlWriter struct {
	buf bytes.Buffer
}

type tomlEntry struct {
	key   string
	value *yaml.Node
}

// table writes the table at path: its head comment and header, if header
// is [ or [[, its keys with plain values, and then its sub-tables and arrays
// of tables. lead is a comment to write before the header. The mapping's
// $comment$ keys are written as the comments they stand for.
func (w *tomlWriter) table(path []string, node *yaml.Node, header, lead string) error {
	content, err := gaby.MappingEntries(node)
	if err != nil {
		return err
	}
	comments := map[string]string{}
	var plain, tables []tomlEntry
	for i := 0; i+1 < len(content); i += 2 {
		key := gaby.ResolveAlias(content[i]).Value
		value := gaby.ResolveAlias(content[i+1])
		if yamlkit.IsCommentKey(key) {
			if value.Kind == yaml.ScalarNode {
				comments[key] = value.Value
			}
			continue
		}
		if value.Kind == yaml.MappingNode || isArrayOfTables(value) {
			tables = append(tables, tomlEntry{key, value})
		} else {
			plain = append(plain, tomlEntry{key, value})
		}
	}
	// An element of an array of tables after the first has its comment in
	// a self-referential head comment.
	lead = joinComments(lead, comments[yamlkit.CommentKey(yamlkit.CommentHead, "")])

	// A table with only sub-tables needs no header of its own, unless it
	// has a comment to go with one.
	if header == "[[" || (header == "[" && (len(plain) > 0 || len(tables) == 0 || lead != "")) {
		if w.buf.Len() > 0 {
			w.buf.WriteByte('\n')
		}
		w.comment(lead)
		w.buf.WriteString(header)
		w.buf.WriteString(formatKeyPath(path))
		if header == "[[" {
			w.buf.WriteString("]]\n")
		} else {
			w.buf.WriteString("]\n")
		}
	} else {
		w.comment(lead)
	}
	for _, entry := range plain {
		w.comment(comments[yamlkit.CommentKey(yamlkit.CommentHead, entry.key)])
		w.buf.WriteString(formatKey(entry.key))
		w.buf.WriteString(" = ")
		if err := w.value(appendPath(path, entry.key), entry.value); err != nil {
			return err
		}
		if text := comments[yamlkit.CommentKey(yamlkit.CommentLine, entry.key)]; text != "" {
			w.buf.WriteString(" # ")
			w.buf.WriteString(strings.ReplaceAll(text, "\n", " "))
		}
		w.buf.WriteByte('\n')
	}
	for _, entry := range tables {
		entryPath := appendPath(path, entry.key)
		headComment := comments[yamlkit.CommentKey(yamlkit.CommentHead, entry.key)]
		if entry.value.Kind == yaml.MappingNode {
			if err := w.table(entryPath, entry.value, "[", headComment); err != nil {
				return err
			}
			continue
		}
		// The first element's comment is the array's head comment.
		for i, element := range entry.value.Content {
			elementLead := ""
			if i == 0 {
				elementLead = headComment
			}
			if err := w.table(entryPath, gaby.ResolveAlias(element), "[[", elementLead); err != nil {
				return err
			}
		}
	}
	if path == nil {
		w.comment(comments[yamlkit.CommentKey(yamlkit.CommentFoot, "")])
	}
	return nil
}

// comment writes text as # comment lines.
func (w *tomlWriter) comment(text string) {
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		w.buf.WriteString("# ")
		w.buf.WriteString(line)
		w.buf.WriteByte('\n')
	}
}

func joinComments(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n" + b
}

// isArrayOfTables reports whether node is a non-empty sequence of mappings.
func isArrayOfTables(node *yaml.Node) bool {
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return false
	}
	for _, element := range node.Content {
		if gaby.ResolveAlias(element).Kind != yaml.MappingNode {
			return false
		}
	}
	return true
}

func appendPath(path []string, key string) []string {
	return append(append(make([]string, 0, len(path)+1), path...), key)
}

// value writes a value inline: a scalar, an array, or an inline table.
func (w *tomlWriter) value(path []string, node *yaml.Node) error {
	node = gaby.ResolveAlias(node)
	switch node.Kind {
	case yaml.SequenceNode:
		w.buf.WriteByte('[')
		for i, element := range node.Content {
			if i > 0 {
				w.buf.WriteString(", ")
			}
			if err := w.value(appendPath(path, strconv.Itoa(i)), element); err != nil {
				return err
			}
		}
		w.buf.WriteByte(']')
		return nil
	case yaml.MappingNode:
		content, err := gaby.MappingEntries(node)
		if err != nil {
			return err
		}
		w.buf.WriteByte('{')
		first := true
		for i := 0; i+1 < len(content); i += 2 {
			key := gaby.ResolveAlias(content[i]).Value
			if yamlkit.IsCommentKey(key) {
				continue
			}
			if !first {
				w.buf.WriteByte(',')
			}
			first = false
			w.buf.WriteByte(' ')
			w.buf.WriteString(formatKey(key))
			w.buf.WriteString(" = ")
			if err := w.value(appendPath(path, key), content[i+1]); err != nil {
				return err
			}
		}
		if !first {
			w.buf.WriteByte(' ')
		}
		w.buf.WriteByte('}')
		return nil
	case yaml.ScalarNode:
		text, err := formatScalar(node)
		if err != nil {
			return fmt.Errorf("%s: %w", strings.Join(path, "."), err)
		}
		w.buf.WriteString(text)
		return nil
	}
	return fmt.Errorf("%s: unexpected YAML node kind %v", strings.Join(path, "."), node.Kind)
}

// formatScalar returns the TOML text of a YAML scalar.
func formatScalar(node *yaml.Node) (string, error) {
	text := node.Value
	switch node.ShortTag() {
	case yaml.NodeTagNull:
		return "", fmt.Errorf("the value is null, which TOML can't represent")
	case yaml.NodeTagBool:
		var b bool
		if err := node.Decode(&b); err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case yaml.NodeTagInt:
		if tomlInteger.MatchString(text) {
			return text, nil
		}
		var v any
		if err := node.Decode(&v); err != nil {
			return "", err
		}
		if u, ok := v.(uint64); ok && u > math.MaxInt64 {
			return "", fmt.Errorf("%s is too large for a TOML integer", text)
		}
		return fmt.Sprint(v), nil
	case yaml.NodeTagFloat:
		switch strings.ToLower(text) {
		case ".inf", "+.inf":
			return "inf", nil
		case "-.inf":
			return "-inf", nil
		case ".nan":
			return "nan", nil
		}
		if tomlFloat.MatchString(text) {
			return text, nil
		}
		var f float64
		if err := node.Decode(&f); err != nil {
			return "", err
		}
		s := strconv.FormatFloat(f, 'g', -1, 64)
		if !strings.ContainsAny(s, ".e") {
			s += ".0"
		}
		return s, nil
	case timestampTag:
		if tomlDateTime.MatchString(text) {
			return text, nil
		}
		var t time.Time
		if err := node.Decode(&t); err != nil {
			return "", err
		}
		if !strings.Contains(text, ":") {
			return t.Format(time.DateOnly), nil
		}
		return t.Format(time.RFC3339Nano), nil
	case TagDateTime, TagLocalDateTime, TagLocalDate, TagLocalTime:
		if !tomlDateTime.MatchString(text) {
			return "", fmt.Errorf("%q is not a TOML date or time", text)
		}
		return text, nil
	}
	// Strings, binary data and scalars with other tags are written as their
	// text.
	return formatString(text), nil
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// The syntax of TOML's integers, its floats other than inf and nan, and its
// dates and times, of which the syntax tree accepts more.
var (
	tomlDateTime = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}(?:[Tt ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:[Zz]|[+-][0-9]{2}:[0-9]{2})?)?|[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?)$`)
	tomlInteger  = regexp.MustCompile(`^(?:[+-]?(?:0|[1-9](?:_?[0-9])*)|0x[0-9A-Fa-f](?:_?[0-9A-Fa-f])*|0o[0-7](?:_?[0-7])*|0b[01](?:_?[01])*)$`)
	tomlFloat    = regexp.MustCompile(`^[+-]?(?:0|[1-9](?:_?[0-9])*)(?:\.[0-9](?:_?[0-9])*(?:[eE][+-]?[0-9](?:_?[0-9])*)?|[eE][+-]?[0-9](?:_?[0-9])*)$`)
)

func formatKey(key string) string {
	if bareKey.MatchString(key) {
		return key
	}
	return formatBasicString(key)
}

func formatKeyPath(path []string) string {
	parts := make([]string, len(path))
	for i, key := range path {
		parts[i] = formatKey(key)
	}
	return strings.Join(parts, ".")
}

// formatString returns s as a TOML string: a multi-line basic string if it
// has a newline, and otherwise a basic string.
func formatString(s string) string {
	if !strings.Contains(s, "\n") {
		return formatBasicString(s)
	}
	var b strings.Builder
	// A newline right after the opening delimiter isn't part of the string.
	b.WriteString("\"\"\"\n")
	quotes := 0
	for i, r := range s {
		if r == '"' {
			quotes++
			// Three quotes in a row would close the string, and so would a
			// quote at its end, next to the closing delimiter.
			if quotes == 3 || i == len(s)-1 {
				b.WriteString(`\"`)
				quotes = 0
				continue
			}
			b.WriteByte('"')
			continue
		}
		quotes = 0
		switch r {
		case '\n', '\t':
			b.WriteRune(r)
		default:
			writeStringRune(&b, r)
		}
	}
	b.WriteString(`"""`)
	return b.String()
}

func formatBasicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		default:
			writeStringRune(&b, r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// writeStringRune writes r as it appears in a TOML basic string, escaping
// backslashes and control characters.
func writeStringRune(b *strings.Builder, r rune) {
	switch r {
	case '\\':
		b.WriteString(`\\`)
	case '\b':
		b.WriteString(`\b`)
	case '\t':
		b.WriteString(`\t`)
	case '\n':
		b.WriteString(`\n`)
	case '\f':
		b.WriteString(`\f`)
	case '\r':
		b.WriteString(`\r`)
	default:
		if r < 0x20 || r == 0x7f {
			fmt.Fprintf(b, `\u%04X`, r)
		} else {
			b.WriteRune(r)
		}
	}
}
