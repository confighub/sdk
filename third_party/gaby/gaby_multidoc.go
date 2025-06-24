package gaby

import (
	"regexp"
	"strings"
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

func ParseAll(y []byte) (Container, error) {
	chunks := strings.Split(NormalizeYAML(string(y)), "\n---\n")
	var multiDoc Container
	for _, chunk := range chunks {
		// If the chunk is empty, it will be deserialized as a document with no content, e.g. "---\n---",
		// or "null\n"
		// We should not add it to the container.
		if YamlIsEmpty(chunk) {
			continue
		}
		// See https://github.com/kubernetes-sigs/kustomize/pull/3431
		if !strings.HasSuffix(chunk, "\n") {
			chunk += "\n"
		}
		container, err := ParseYAML([]byte(chunk))
		if err != nil {
			return nil, err
		}
		if container.IsEmptyDoc() {
			// This is a document with only comments, e.g. "---\n# comment\n---"
			// We should not add it to the container.
			continue
		}
		multiDoc = append(multiDoc, container)
	}
	return multiDoc, nil
}

func (m Container) Search(path ...string) Container {
	var results Container
	for _, c := range m {
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
	if len(m) == 0 {
		return nil
	}
	return m[0].Data()
}

func (m Container) String() string {
	var result []string
	for _, c := range m {
		if c.IsEmptyDoc() {
			continue
		}
		result = append(result, c.String())
	}
	return strings.Join(result, "---\n")
}
