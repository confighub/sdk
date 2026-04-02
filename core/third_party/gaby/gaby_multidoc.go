package gaby

import (
	"regexp"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type Container []*YamlDoc

func NormalizeYAML(y string) string {
	y = strings.ReplaceAll(y, "\r\n", "\n")
	// Handle comment after document separator without newline
	re := regexp.MustCompile(`(---)([ \t]*#)`)
	y = re.ReplaceAllString(y, "$1\n$2")
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
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			hasLeadingSep = line == "---"
			break
		}
	}

	chunks := strings.Split(normalized, "\n---\n")
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
	return multiDoc, nil
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

func (m Container) String() string {
	var sb strings.Builder
	docCount := 0
	for _, c := range m {
		if c == nil || c.IsEmptyDoc() {
			continue
		}
		docComment, hasDocComment := c.GetDocComment()

		if hasDocComment {
			// Emit comment lines above the --- separator
			if docComment != "" {
				for _, line := range strings.Split(docComment, "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						line = "# " + line
					}
					sb.WriteString(line)
					sb.WriteByte('\n')
				}
			}
			sb.WriteString("---\n")
			sb.WriteString(c.stringWithoutDocComment())
		} else {
			// Insert --- separator between docs that don't have doc comments
			if docCount > 0 {
				sb.WriteString("---\n")
			}
			sb.WriteString(c.String())
		}
		docCount++
	}
	return sb.String()
}

func (m Container) Bytes() []byte {
	return []byte(m.String())
}
