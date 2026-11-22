package gaby

import (
	"regexp"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type Container []*YamlDoc

// commentAfterDocumentMarker matches a document marker at the start of a line followed by
// a comment. It is anchored to the line start so that "---" inside a value is left alone.
var commentAfterDocumentMarker = regexp.MustCompile(`(?m)^(---)([ \t]*#)`)

func NormalizeYAML(y string) string {
	y = strings.ReplaceAll(y, "\r\n", "\n")
	// Handle comment after document separator without newline
	y = commentAfterDocumentMarker.ReplaceAllString(y, "$1\n$2")
	// Remove leading and trailing space
	y = strings.TrimSpace(y)
	// Remove leading doc separator
	y = strings.TrimPrefix(y, "---\n")
	// Remove trailing doc separator
	y = strings.TrimSuffix(y, "\n---")
	// Remove leading and trailing space again, if any
	y = strings.TrimSpace(y)
	return y
}

// documentMarker reports whether line is a document marker -- "---", which starts a
// document, or "...", which ends one -- and returns what follows the marker on the line.
// YAML defines a marker as the three characters at the start of a line followed by
// whitespace or the end of the line, so "--- !!map", "--- {a: 1}" and "--- |" start a
// document whose content begins on the marker's line, while "----" and "---a" are not
// markers. A comment directly after the marker, "---# text", is not YAML but is accepted,
// since Helm charts in the wild write it.
func documentMarker(line string) (rest string, ok bool) {
	if !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "...") {
		return "", false
	}
	rest = line[3:]
	if rest == "" {
		return "", true
	}
	switch rest[0] {
	case ' ', '\t', '#':
		return strings.TrimLeft(rest, " \t"), true
	}
	return "", false
}

// splitDocuments splits YAML text into the text of each document, at document markers.
// A document marker at the start of a line always ends the document before it, even
// within a block scalar, so no parsing is needed to find them. What follows "---" on its
// line is the start of the next document. What follows "..." can only be a comment, and is
// kept with the document it ends.
func splitDocuments(y string) []string {
	var chunks []string
	var current []string
	for line := range strings.SplitSeq(y, "\n") {
		rest, ok := documentMarker(line)
		if !ok {
			current = append(current, line)
			continue
		}
		startsDocument := strings.HasPrefix(line, "---")
		if !startsDocument && rest != "" {
			current = append(current, rest)
		}
		chunks = append(chunks, strings.Join(current, "\n"))
		current = nil
		if startsDocument && rest != "" {
			current = append(current, rest)
		}
	}
	return append(chunks, strings.Join(current, "\n"))
}

// Returns true if YAML doc is trivially empty, even no comments
func YamlIsEmpty(y string) bool {
	y = strings.TrimSpace(y)
	return y == "" || y == "null" || y == "{}" || y == "[]"
}

// docCommentKey is the $comment$head$ key used to store document-level header comments.
// When present in a document's top-level map, it indicates that the document was preceded
// by a "---" separator, and the value is the comment text that appeared above that separator.
// An empty value means there was a separator but no comment.
const docCommentKey = commentKeyPrefix + "head$"

func ParseAll(y []byte) (Container, error) {
	normalized := NormalizeYAML(string(y))

	// Detect whether the input has a leading document separator before normalization
	// strips it.
	trimmedInput := strings.TrimSpace(string(y))
	hasLeadingSep := strings.HasPrefix(trimmedInput, "---")
	if !hasLeadingSep {
		// Check if there are comment lines before the first ---
		for _, line := range strings.Split(trimmedInput, "\n") {
			line = strings.TrimRight(line, " \t\r")
			if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			_, isMarker := documentMarker(line)
			hasLeadingSep = isMarker && strings.HasPrefix(line, "---")
			break
		}
	}

	chunks := splitDocuments(normalized)
	var multiDoc Container
	// pendingComments collects comment text from empty/comment-only chunks
	// to attach to the following document.
	var pendingComments []string
	// isLeadingComment is true only for comments that appeared before the first ---
	// in the original input. These are stored as $comment$head$ doc comment keys
	// so they appear above the --- separator on output.
	isLeadingComment := hasLeadingSep
	// needsSep tracks whether the next doc needs a --- separator marker.
	needsSep := hasLeadingSep
	for _, chunk := range chunks {
		if YamlIsEmpty(chunk) {
			if comments := extractCommentLines(chunk); comments != "" {
				pendingComments = append(pendingComments, comments)
			}
			needsSep = true
			continue
		}
		if !strings.HasSuffix(chunk, "\n") {
			chunk += "\n"
		}
		container, err := ParseYAML([]byte(chunk))
		if err != nil {
			return nil, err
		}
		if container.IsEmptyDoc() {
			if container.YNode() != nil && container.YNode().HeadComment != "" {
				pendingComments = append(pendingComments, container.YNode().HeadComment)
			}
			needsSep = true
			continue
		}

		if isLeadingComment && (needsSep || len(pendingComments) > 0) {
			// Comments that appeared before the first --- in the input:
			// store as a $comment$head$ doc comment key so they render above ---.
			commentText := strings.Join(pendingComments, "\n")
			container.SetDocComment(commentText)
			pendingComments = nil
		} else if len(pendingComments) > 0 {
			// Inter-document comments (between --- separators):
			// attach as HeadComment on the first key of the following document,
			// so they appear below --- on output (their original position).
			prependHeadComment(container, strings.Join(pendingComments, "\n"))
			pendingComments = nil
		} else if needsSep && len(multiDoc) == 0 {
			// Leading --- with no comments: store empty doc comment to preserve separator.
			container.SetDocComment("")
		}
		isLeadingComment = false
		needsSep = true
		multiDoc = append(multiDoc, container)
	}
	// Comments after the last document have no following document to precede, so they
	// follow the last one instead. Comments with no document at all are not representable
	// in a Container; callers that write data back keep the original text when it parses to
	// no documents.
	if len(pendingComments) > 0 && len(multiDoc) > 0 {
		appendFootComment(multiDoc[len(multiDoc)-1], strings.Join(pendingComments, "\n"))
	}
	return multiDoc, nil
}

// appendFootComment adds comment text after everything else in a document.
func appendFootComment(doc *YamlDoc, comment string) {
	target := doc.YNode()
	if target == nil {
		return
	}
	// yaml.v3 puts a document's foot comment on the document node in some shapes, such as
	// after a line comment, and the encoder writes only that one when both have one.
	if document := doc.node.Document(); document != nil && document.Kind == yaml.DocumentNode && document.FootComment != "" {
		target = document
	}
	if target.FootComment != "" {
		target.FootComment += "\n" + comment
	} else {
		target.FootComment = comment
	}
}

// prependHeadComment prepends comment text to the first key's HeadComment
// in a document. Used for inter-document comments that should appear below
// the --- separator (their original position).
func prependHeadComment(doc *YamlDoc, comment string) {
	if doc == nil || doc.node == nil {
		return
	}
	ynode := doc.node.YNode()
	var mappingNode *yaml.Node
	if ynode.Kind == yaml.DocumentNode && len(ynode.Content) > 0 {
		mappingNode = ynode.Content[0]
	} else if ynode.Kind == yaml.MappingNode {
		mappingNode = ynode
	}
	if mappingNode != nil && mappingNode.Kind == yaml.MappingNode && len(mappingNode.Content) >= 2 {
		keyNode := mappingNode.Content[0]
		if keyNode.HeadComment != "" {
			keyNode.HeadComment = comment + "\n" + keyNode.HeadComment
		} else {
			keyNode.HeadComment = comment
		}
	}
}

// SetDocComment sets the document-level header comment key ($comment$head$) on a document.
func (c *YamlDoc) SetDocComment(comment string) {
	// Insert at the beginning of the mapping
	ynode := c.node.YNode()
	var mappingNode *yaml.Node
	if ynode.Kind == yaml.DocumentNode && len(ynode.Content) > 0 {
		mappingNode = ynode.Content[0]
	} else if ynode.Kind == yaml.MappingNode {
		mappingNode = ynode
	}
	if mappingNode == nil || mappingNode.Kind != yaml.MappingNode {
		return
	}

	// Remove existing doc comment key if present
	var filtered []*yaml.Node
	for i := 0; i < len(mappingNode.Content)-1; i += 2 {
		if mappingNode.Content[i].Kind == yaml.ScalarNode && mappingNode.Content[i].Value == docCommentKey {
			continue
		}
		filtered = append(filtered, mappingNode.Content[i], mappingNode.Content[i+1])
	}

	// Prepend the doc comment key
	newContent := []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: docCommentKey, Tag: "!!str"},
		{Kind: yaml.ScalarNode, Value: comment, Tag: "!!str"},
	}
	mappingNode.Content = append(newContent, filtered...)
}

// GetDocComment returns the document-level header comment and whether it exists.
func (c *YamlDoc) GetDocComment() (string, bool) {
	if c == nil || c.node == nil {
		return "", false
	}
	ynode := c.node.YNode()
	var mappingNode *yaml.Node
	if ynode.Kind == yaml.DocumentNode && len(ynode.Content) > 0 {
		mappingNode = ynode.Content[0]
	} else if ynode.Kind == yaml.MappingNode {
		mappingNode = ynode
	}
	if mappingNode == nil || mappingNode.Kind != yaml.MappingNode {
		return "", false
	}
	for i := 0; i < len(mappingNode.Content)-1; i += 2 {
		keyNode := mappingNode.Content[i]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == docCommentKey {
			return mappingNode.Content[i+1].Value, true
		}
	}
	return "", false
}

// extractCommentLines extracts comment lines (starting with #) from a chunk
// that may otherwise be empty. Returns empty string if no comments found.
func extractCommentLines(chunk string) string {
	var comments []string
	for _, line := range strings.Split(chunk, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			comments = append(comments, trimmed)
		}
	}
	return strings.Join(comments, "\n")
}

// stringWithoutDocComment serializes a doc, stripping only the doc-level
// $comment$head$ key while preserving all other $comment$ keys.
func (c *YamlDoc) stringWithoutDocComment() string {
	if c == nil || c.node == nil {
		return ""
	}
	// Deep copy via round-trip to avoid mutating the original
	data := c.Bytes()
	copy, err := ParseYAML(data)
	if err != nil {
		return c.String()
	}
	// Remove the $comment$head$ key from the copy
	_ = copy.DeleteP(docCommentKey)
	return copy.String()
}

func (m Container) Search(path ...string) Container {
	var results Container
	for _, c := range m {
		if c == nil {
			continue
		}
		if result := c.Search(path...); result != nil {
			results = append(results, result)
		}
	}
	if len(results) > 0 {
		return results
	}
	return nil
}

func (m Container) Data() interface{} {
	if len(m) == 0 || m[0] == nil {
		return nil
	}
	return m[0].Data()
}

// DocumentRendering is one document of a Container as String renders it: Text is the document
// itself, and Prefix what String writes before it -- nothing for the first document, "---\n"
// between documents, or a document comment's lines followed by "---\n". A document's rendering
// depends on nothing but the document and whether it is the first, so the documents of a Container
// can be stored apart and joined back by concatenation.
type DocumentRendering struct {
	Prefix string
	Text   string
}

// Renderings returns the documents of m as String renders them, in order, skipping the empty
// documents String skips. Concatenating each Prefix and Text gives String's result.
func (m Container) Renderings() []DocumentRendering {
	var renderings []DocumentRendering
	for _, c := range m {
		if c == nil || c.IsEmptyDoc() {
			continue
		}
		docComment, hasDocComment := c.GetDocComment()

		if hasDocComment {
			// Emit comment lines above the --- separator
			var prefix strings.Builder
			if docComment != "" {
				for _, line := range strings.Split(docComment, "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						line = "# " + line
					}
					prefix.WriteString(line)
					prefix.WriteByte('\n')
				}
			}
			prefix.WriteString("---\n")
			renderings = append(renderings, DocumentRendering{Prefix: prefix.String(), Text: c.stringWithoutDocComment()})
		} else {
			// Insert --- separator between docs that don't have doc comments
			prefix := ""
			if len(renderings) > 0 {
				prefix = "---\n"
			}
			renderings = append(renderings, DocumentRendering{Prefix: prefix, Text: c.String()})
		}
	}
	return renderings
}

func (m Container) String() string {
	var sb strings.Builder
	for _, rendering := range m.Renderings() {
		sb.WriteString(rendering.Prefix)
		sb.WriteString(rendering.Text)
	}
	return sb.String()
}

func (m Container) Bytes() []byte {
	return []byte(m.String())
}
