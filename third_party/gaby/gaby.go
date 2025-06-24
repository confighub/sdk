package gaby

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

//------------------------------------------------------------------------------

// Error variables similar to gabs
var (
	// ErrOutOfBounds indicates an index was out of bounds.
	ErrOutOfBounds = errors.New("out of bounds")

	// ErrNotObjOrArray is returned when a target is not an object or array type
	// but needs to be for the intended operation.
	ErrNotObjOrArray = errors.New("not an object or array")

	// ErrNotObj is returned when a target is not an object but needs to be for
	// the intended operation.
	ErrNotObj = errors.New("not an object")

	// ErrInvalidQuery is returned when a search query was not valid.
	ErrInvalidQuery = errors.New("invalid search query")

	// ErrNotArray is returned when a target is not an array but needs to be for
	// the intended operation.
	ErrNotArray = errors.New("not an array")

	// ErrPathCollision is returned when creating a path failed because an
	// element collided with an existing value.
	ErrPathCollision = errors.New("encountered value collision whilst building path")

	// ErrInvalidInputObj is returned when the input value was not a
	// map[string]interface{}.
	ErrInvalidInputObj = errors.New("invalid input object")

	// ErrInvalidInputText is returned when the input data could not be parsed.
	ErrInvalidInputText = errors.New("input text could not be parsed")

	// ErrNotFound is returned when a query leaf is not found.
	ErrNotFound = errors.New("field not found")

	// ErrInvalidPath is returned when the filepath was not valid.
	ErrInvalidPath = errors.New("invalid file path")

	// ErrInvalidBuffer is returned when the input buffer contained an invalid
	// YAML string.
	ErrInvalidBuffer = errors.New("input buffer contained invalid YAML")
)

var EmptyDocument = []byte("null")

var (
	r1 *strings.Replacer
	r2 *strings.Replacer
)

func init() {
	r1 = strings.NewReplacer("~1", "/", "~0", "~")
	r2 = strings.NewReplacer("~1", ".", "~0", "~")
}

//------------------------------------------------------------------------------

// YAMLPointerToSlice parses a YAML pointer path and returns the path segments as a slice.
func YAMLPointerToSlice(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if path[0] != '/' {
		return nil, errors.New("failed to resolve YAML pointer: path must begin with '/'")
	}
	if path == "/" {
		return []string{""}, nil
	}
	hierarchy := strings.Split(path, "/")[1:]
	for i, v := range hierarchy {
		hierarchy[i] = r1.Replace(v)
	}
	return hierarchy, nil
}

// DotPathToSlice returns a slice of path segments parsed out of a dot path.
func DotPathToSlice(path string) []string {
	// Regular expression to match quoted segments or normal segments
	re := regexp.MustCompile(`"([^"]+)"|[^.]+`)
	matches := re.FindAllString(path, -1)

	// Optional: Process each match if needed (similar to r2.Replace(v))
	for i, v := range matches {
		matches[i] = r2.Replace(strings.Trim(v, `"`)) // Remove quotes around the quoted segments
	}

	return matches
}

//------------------------------------------------------------------------------

// YamlDoc references a specific element within a YAML structure.
type YamlDoc struct {
	// an empty document is a doc that contains only comments
	isEmptyDoc bool
	node       *yaml.RNode
}

// Data returns the underlying node of the target element in the YAML structure.
func (c *YamlDoc) Data() interface{} {
	if c == nil {
		return nil
	}
	var v interface{}
	err := c.node.YNode().Decode(&v)
	if err != nil {
		return nil
	}
	return v
}

// YNode returns yaml's Node to prevent the decoding process.
func (c *YamlDoc) YNode() *yaml.Node {
	if c == nil {
		return nil
	}
	return c.node.YNode()
}

//------------------------------------------------------------------------------

// Search attempts to find and return a node within the YAML structure by
// following a provided hierarchy of field names to locate the target.
//
// If the search encounters an array then the next hierarchy field name must be
// either an integer which is interpreted as the index of the target, or the
// character '*', in which case all elements are searched with the remaining
// search hierarchy and the results returned within an array.

func (c *YamlDoc) Search(hierarchy ...string) *YamlDoc {
	result, _ := c.searchStrict(hierarchy...)
	return result
}

func (c *YamlDoc) searchStrict(hierarchy ...string) (*YamlDoc, error) {
	if c == nil || c.node == nil {
		return nil, ErrNotFound
	}
	node := c.node
	for target := 0; target < len(hierarchy); target++ {
		pathSeg := hierarchy[target]
		kind := node.YNode().Kind
		switch kind {
		/*case yaml.DocumentNode:
		// Handle the DocumentNode
		if pathSeg == "*" {
			// Handle wildcard to search across all documents
		} else {
			index, err := strconv.Atoi(pathSeg)
			if err != nil {
				return nil, fmt.Errorf("invalid document index '%v'", pathSeg)
			}
			if index < 0 || index >= len(node.Content()) {
				return nil, ErrOutOfBounds
			}
			node = node.
		}*/
		case yaml.MappingNode, yaml.ScalarNode:
			fieldNode, err := node.Pipe(yaml.Get(pathSeg))
			if err != nil {
				return nil, fmt.Errorf("failed to resolve path segment '%v': %v", pathSeg, err)
			}
			if fieldNode == nil {
				return nil, fmt.Errorf("failed to resolve path segment '%v': field not found", pathSeg)
			}
			node = fieldNode
		case yaml.SequenceNode:
			if pathSeg == "*" {
				var nodes []*yaml.RNode
				elements, err := node.Elements()
				if err != nil {
					return nil, err
				}
				if (target + 1) >= len(hierarchy) {
					nodes = elements
				} else {
					for _, elem := range elements {
						res, err := (&YamlDoc{node: elem}).searchStrict(hierarchy[target+1:]...)
						if err == nil && res != nil {
							nodes = append(nodes, res.node)
						}
					}
				}
				if len(nodes) == 0 {
					return nil, nil
				}
				return &YamlDoc{node: yaml.NewRNode(&yaml.Node{
					Kind:    yaml.SequenceNode,
					Content: nodesToNodes(nodes),
				})}, nil
			}
			index, err := strconv.Atoi(pathSeg)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve path segment '%v': invalid array index", pathSeg)
			}
			elements, err := node.Elements()
			if err != nil {
				return nil, err
			}
			if index < 0 || index >= len(elements) {
				return nil, ErrOutOfBounds
			}
			node = elements[index]
		default:
			return nil, fmt.Errorf("failed to resolve path segment '%v': unexpected node kind %v", pathSeg, kind)
		}
	}
	return &YamlDoc{node: node}, nil
}

func nodesToNodes(rnodes []*yaml.RNode) []*yaml.Node {
	var nodes []*yaml.Node
	for _, rnode := range rnodes {
		nodes = append(nodes, rnode.YNode())
	}
	return nodes
}

// JSONPointerToSlice parses a JSON pointer path
// (https://tools.ietf.org/html/rfc6901) and returns the path segments as a
// slice.
//
// Because the characters '~' (%x7E) and '/' (%x2F) have special meanings in
// gabs paths, '~' needs to be encoded as '~0' and '/' needs to be encoded as
// '~1' when these characters appear in a reference key.
func JSONPointerToSlice(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if path[0] != '/' {
		return nil, errors.New("failed to resolve JSON pointer: path must begin with '/'")
	}
	if path == "/" {
		return []string{""}, nil
	}
	hierarchy := strings.Split(path, "/")[1:]
	for i, v := range hierarchy {
		hierarchy[i] = r1.Replace(v)
	}
	return hierarchy, nil
}

func (c *YamlDoc) JSONPointer(path string) (*YamlDoc, error) {
	hierarchy, err := JSONPointerToSlice(path)
	if err != nil {
		return nil, err
	}
	return c.searchStrict(hierarchy...)
}

// Path searches the YAML structure following a path in dot notation,
// segments of this path are searched according to the same rules as Search.
func (c *YamlDoc) Path(path string) *YamlDoc {
	return c.Search(DotPathToSlice(path)...)
}

// S is a shorthand alias for Search.
func (c *YamlDoc) S(hierarchy ...string) *YamlDoc {
	return c.Search(hierarchy...)
}

// Exists checks whether a field exists within the hierarchy.
func (c *YamlDoc) Exists(hierarchy ...string) bool {
	res, err := c.searchStrict(hierarchy...)
	return err == nil && res != nil
}

// ExistsP checks whether a dot notation path exists.
func (c *YamlDoc) ExistsP(path string) bool {
	return c.Exists(DotPathToSlice(path)...)
}

// Index attempts to find and return an element within a YAML array by an index.
func (c *YamlDoc) Index(index int) *YamlDoc {
	result, _ := c.index0(index)
	return result
}

func (c *YamlDoc) index0(index int) (*YamlDoc, error) {
	if c == nil || c.node == nil {
		return nil, ErrNotArray
	}
	node := c.node
	if node.YNode().Kind != yaml.SequenceNode {
		return nil, ErrNotArray
	}
	elements, err := node.Elements()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(elements) {
		return nil, ErrOutOfBounds
	}
	return &YamlDoc{node: elements[index]}, nil
}

func (c *YamlDoc) IsEmptyDoc() bool {
	return c.isEmptyDoc || c.YNode().IsZero() || yaml.IsYNodeNilOrEmpty(c.YNode())
}

func (c *YamlDoc) IsArray() bool {
	return c.node.YNode().Kind == yaml.SequenceNode
}

// Children returns a slice of all children of an array element. This also works
// for objects; however, the children returned for an object will be in a random
// order, and you lose the names of the returned objects this way. If the
// underlying container value isn't an array or map, nil is returned.
func (c *YamlDoc) Children() []*YamlDoc {
	if c == nil || c.node == nil {
		return nil
	}
	node := c.node
	switch node.YNode().Kind {
	case yaml.SequenceNode:
		elements, err := node.Elements()
		if err != nil {
			return nil
		}
		children := make([]*YamlDoc, len(elements))
		for i, elem := range elements {
			children[i] = &YamlDoc{node: elem}
		}
		return children
	case yaml.MappingNode:
		content := node.YNode().Content
		children := make([]*YamlDoc, 0, len(content)/2)
		for i := 1; i < len(content); i += 2 {
			valueNode := yaml.NewRNode(content[i])
			children = append(children, &YamlDoc{node: valueNode})
		}
		return children
	default:
		return nil
	}
}

// ChildrenMap returns a map of all the children of an object element. If the
// underlying value isn't an object then an empty map is returned.
func (c *YamlDoc) ChildrenMap() map[string]*YamlDoc {
	if c == nil || c.node == nil {
		return map[string]*YamlDoc{}
	}
	node := c.node
	if node.YNode().Kind != yaml.MappingNode {
		return map[string]*YamlDoc{}
	}
	content := node.YNode().Content
	children := make(map[string]*YamlDoc, len(content)/2)
	for i := 0; i < len(content); i += 2 {
		keyNode := content[i]
		valueNode := content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			// Skip non-scalar keys
			continue
		}
		key := keyNode.Value
		children[key] = &YamlDoc{node: yaml.NewRNode(valueNode)}
	}
	return children
}

// Set attempts to set the value of a field located by a hierarchy of field
// names.
func (c *YamlDoc) Set(value interface{}, hierarchy ...string) (*YamlDoc, error) {
	if c == nil {
		return nil, ErrInvalidInputObj
	}
	node := c.node
	for target := 0; target < len(hierarchy); target++ {
		pathSeg := hierarchy[target]
		kind := node.YNode().Kind
		switch kind {
		case yaml.MappingNode:
			// Check if field exists
			fieldNode, err := node.Pipe(yaml.Get(pathSeg))
			if err != nil {
				return nil, err
			}
			if fieldNode == nil {
				// Create new field
				newNode := &yaml.Node{}
				if target == len(hierarchy)-1 {
					// Last segment, set the value
					err := setValue(newNode, value)
					if err != nil {
						return nil, err
					}
				} else {
					// Intermediate segment, create a mapping node
					newNode.Kind = yaml.MappingNode
				}
				rNode := yaml.NewRNode(newNode)
				if newNode.Tag == yaml.NodeTagNull {
					rNode.ShouldKeep = true
				}
				err = node.PipeE(yaml.SetField(pathSeg, rNode))
				if err != nil {
					return nil, err
				}
				fieldNode, err = node.Pipe(yaml.Get(pathSeg))
				if err != nil {
					return nil, err
				}
			} else if target == len(hierarchy)-1 {
				// Overwrite existing field
				err := setValue(fieldNode.YNode(), value)
				if err != nil {
					return nil, err
				}
			}
			node = fieldNode
		case yaml.SequenceNode:
			if pathSeg == "-" {
				// Append to sequence
				newNode := &yaml.Node{}
				if target == len(hierarchy)-1 {
					err := setValue(newNode, value)
					if err != nil {
						return nil, err
					}
				} else {
					newNode.Kind = yaml.MappingNode
				}
				err := node.PipeE(yaml.Append(newNode))
				if err != nil {
					return nil, err
				}
				elements, err := node.Elements()
				if err != nil {
					return nil, err
				}
				node = elements[len(elements)-1]
			} else {
				index, err := strconv.Atoi(pathSeg)
				if err != nil {
					return nil, fmt.Errorf("invalid array index '%v': %v", pathSeg, err)
				}
				elements, err := node.Elements()
				if err != nil {
					return nil, err
				}
				if index < 0 || index >= len(elements) {
					return nil, ErrOutOfBounds
				}
				node = elements[index]
				if target == len(hierarchy)-1 {
					err := setValue(node.YNode(), value)
					if err != nil {
						return nil, err
					}
				}
			}
		default:
			return nil, fmt.Errorf("unexpected node kind %v at path segment '%v'", kind, pathSeg)
		}
	}
	return &YamlDoc{node: node}, nil
}

func setValue(node *yaml.Node, value interface{}) error {
	switch v := value.(type) {
	case string:
		node.Kind = yaml.ScalarNode
		node.Value = v
		node.Tag = yaml.NodeTagString
	case int:
		node.Kind = yaml.ScalarNode
		node.Value = strconv.Itoa(v)
		node.Tag = yaml.NodeTagInt
	case bool:
		node.Kind = yaml.ScalarNode
		node.Value = strconv.FormatBool(v)
		node.Tag = yaml.NodeTagBool
	case float64:
		node.Kind = yaml.ScalarNode
		node.Value = fmt.Sprintf("%v", v)
		node.Tag = yaml.NodeTagFloat
	case map[string]interface{}:
		node.Kind = yaml.MappingNode
		for key, val := range v {
			childNode := &yaml.Node{}
			err := setValue(childNode, val)
			if err != nil {
				return err
			}
			node.Content = append(node.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: key,
				Tag:   yaml.NodeTagString,
			}, childNode)
		}
	case *orderedmap.OrderedMap[string, interface{}]:
		node.Kind = yaml.MappingNode
		for pair := v.Oldest(); pair != nil; pair = pair.Next() {
			childNode := &yaml.Node{}
			err := setValue(childNode, pair.Value)
			if err != nil {
				return err
			}
			node.Content = append(node.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: pair.Key,
				Tag:   yaml.NodeTagString,
			}, childNode)
		}
	case []interface{}:
		node.Kind = yaml.SequenceNode
		for _, val := range v {
			childNode := &yaml.Node{}
			err := setValue(childNode, val)
			if err != nil {
				return err
			}
			node.Content = append(node.Content, childNode)
		}
	case nil:
		node.Kind = yaml.ScalarNode
		node.Value = "null"
		node.Tag = yaml.NodeTagNull
	case *yaml.Node:
		// direct copy without decoding
		// by using yaml.Node, we solved key/value reordering problem
		// caused by decoding YAML maps into Go's maps, and back again.
		*node = *v
	default:
		return fmt.Errorf("unsupported value type: %T", value)
	}
	return nil
}

// SetP sets the value of a field at a path using dot notation.
func (c *YamlDoc) SetP(value interface{}, path string) (*YamlDoc, error) {
	return c.Set(value, DotPathToSlice(path)...)
}

// SetDocP sets the value of a field to a YamlDoc at a path using dot notation.
func (c *YamlDoc) SetDocP(doc *YamlDoc, path string) (*YamlDoc, error) {
	return c.Set(doc.node.YNode(), DotPathToSlice(path)...)
}

// SetIndex attempts to set a value of an array element based on an index.
func (c *YamlDoc) SetIndex(value interface{}, index int) (*YamlDoc, error) {
	if c == nil || c.node == nil {
		return nil, ErrNotArray
	}
	node := c.node
	if node.YNode().Kind != yaml.SequenceNode {
		return nil, ErrNotArray
	}
	elements := node.YNode().Content
	if index < 0 || index >= len(elements) {
		return nil, ErrOutOfBounds
	}

	// Convert the value to a *yaml.Node
	newNode := &yaml.Node{}
	err := setValue(newNode, value)
	if err != nil {
		return nil, err
	}

	// Replace the element at the specified index
	elements[index] = newNode

	return &YamlDoc{node: yaml.NewRNode(newNode)}, nil
}

// SetYAMLPointer parses a YAML pointer path and sets the leaf to a value. Returns
// an error if the pointer could not be resolved due to missing fields.
func (c *YamlDoc) SetYAMLPointer(value interface{}, path string) (*YamlDoc, error) {
	hierarchy, err := YAMLPointerToSlice(path)
	if err != nil {
		return nil, err
	}
	return c.Set(value, hierarchy...)
}

// Object creates a new YAML object at a target path. Returns an error if the
// path contains a collision with a non object type.
func (c *YamlDoc) Object(hierarchy ...string) (*YamlDoc, error) {
	return c.Set(map[string]interface{}{}, hierarchy...)
}

// ObjectP creates a new YAML object at a target path using dot notation.
// Returns an error if the path contains a collision with a non object type.
func (c *YamlDoc) ObjectP(path string) (*YamlDoc, error) {
	return c.Object(DotPathToSlice(path)...)
}

// ObjectI creates a new YAML object at an array index. Returns an error if the
// object is not an array or the index is out of bounds.
func (c *YamlDoc) ObjectI(index int) (*YamlDoc, error) {
	return c.SetIndex(map[string]interface{}{}, index)
}

// Array creates a new YAML array at a path. Returns an error if the path
// contains a collision with a non object type.
func (c *YamlDoc) Array(hierarchy ...string) (*YamlDoc, error) {
	return c.Set([]interface{}{}, hierarchy...)
}

// ArrayP creates a new YAML array at a path using dot notation. Returns an
// error if the path contains a collision with a non object type.
func (c *YamlDoc) ArrayP(path string) (*YamlDoc, error) {
	return c.Array(DotPathToSlice(path)...)
}

// ArrayI creates a new YAML array within an array at an index. Returns an error
// if the element is not an array or the index is out of bounds.
func (c *YamlDoc) ArrayI(index int) (*YamlDoc, error) {
	return c.SetIndex([]interface{}{}, index)
}

// ArrayOfSize creates a new YAML array of a particular size at a path. Returns
// an error if the path contains a collision with a non object type.
func (c *YamlDoc) ArrayOfSize(size int, hierarchy ...string) (*YamlDoc, error) {
	a := make([]interface{}, size)
	return c.Set(a, hierarchy...)
}

// ArrayOfSizeP creates a new YAML array of a particular size at a path using
// dot notation. Returns an error if the path contains a collision with a non
// object type.
func (c *YamlDoc) ArrayOfSizeP(size int, path string) (*YamlDoc, error) {
	return c.ArrayOfSize(size, DotPathToSlice(path)...)
}

// ArrayOfSizeI creates a new YAML array of a particular size within an array at
// an index. Returns an error if the element is not an array or the index is out
// of bounds.
func (c *YamlDoc) ArrayOfSizeI(size, index int) (*YamlDoc, error) {
	a := make([]interface{}, size)
	return c.SetIndex(a, index)
}

type ElementRemover struct {
	Index int
}

func (er ElementRemover) Filter(node *yaml.RNode) (*yaml.RNode, error) {
	seqNode := node.YNode()
	if seqNode.Kind != yaml.SequenceNode {
		return node, fmt.Errorf("node is not a sequence")
	}
	if len(seqNode.Content) <= er.Index {
		return node, fmt.Errorf("index out of range")
	}
	seqNode.Content = append(seqNode.Content[:er.Index], seqNode.Content[er.Index+1:]...)
	return node, nil
}

// Delete an element at a path.
func (c *YamlDoc) Delete(hierarchy ...string) error {
	if c == nil || c.node == nil {
		return ErrNotObj
	}
	if len(hierarchy) == 0 {
		return ErrInvalidQuery
	}
	node := c.node
	for target := 0; target < len(hierarchy)-1; target++ {
		pathSeg := hierarchy[target]
		fieldNode, err := node.Pipe(yaml.Get(pathSeg))
		if err != nil {
			return err
		}
		if fieldNode == nil {
			return ErrNotFound
		}
		node = fieldNode
	}
	lastSeg := hierarchy[len(hierarchy)-1]
	if index, err := strconv.Atoi(lastSeg); err == nil {
		// lastSeg is an integer index
		if err := node.PipeE(ElementRemover{Index: index}); err != nil {
			return err
		}
	} else {
		// lastSeg is a string key
		if err := node.PipeE(yaml.Clear(lastSeg)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteP deletes an element at a path using dot notation.
func (c *YamlDoc) DeleteP(path string) error {
	return c.Delete(DotPathToSlice(path)...)
}

func (c *YamlDoc) GetComments() string {
	ynode := c.YNode()
	// Combine all the associated comments into one string.
	comments := []string{}
	if ynode.HeadComment != "" {
		comments = append(comments, ynode.HeadComment)
	}
	if ynode.LineComment != "" {
		comments = append(comments, ynode.LineComment)
	}
	if ynode.FootComment != "" {
		comments = append(comments, ynode.FootComment)
	}
	return strings.Join(comments, "\n")
}

func (c *YamlDoc) SetComment(comment string) {
	ynode := c.YNode()
	// Note: currently setting HeadComment puts the comment AFTER the line.
	// FootComment does also, but with an extra newline after.
	// kyaml also doesn't associate those comment positions with the field they appear adjacent to
	// when parsing.
	// So just set the LineComment, which DTRT.
	ynode.LineComment = comment
}

// MergeFn merges two objects using a provided function to resolve collisions.
//
// The collision function receives two interface{} arguments, destination (the
// original object) and source (the object being merged into the destination).
// Whichever value is returned becomes the new value in the destination object
// at the location of the collision.
func (c *YamlDoc) MergeFn(source *YamlDoc, collisionFn func(destination, source interface{}) interface{}) error {
	if source.node.YNode().Kind != yaml.MappingNode {
		return nil
	}

	var recursiveFn func(destNode, sourceNode *yaml.RNode, path []string) error

	recursiveFn = func(destNode, sourceNode *yaml.RNode, path []string) error {
		if destNode.YNode().Kind == yaml.MappingNode && sourceNode.YNode().Kind == yaml.MappingNode {
			sourceFields, err := sourceNode.Fields()
			if err != nil {
				return err
			}
			for _, key := range sourceFields {
				newPath := append(path, key)
				sourceValueNode, err := sourceNode.Pipe(yaml.Get(key))
				if err != nil {
					return err
				}
				destValueNode, err := destNode.Pipe(yaml.Get(key))
				if err != nil {
					return err
				}
				if destValueNode == nil {
					// Key does not exist in dest, set it
					err := destNode.PipeE(yaml.SetField(key, sourceValueNode))
					if err != nil {
						return err
					}
				} else {
					// Key exists in dest, need to resolve collision
					if destValueNode.YNode().Kind == yaml.MappingNode && sourceValueNode.YNode().Kind == yaml.MappingNode {
						// Both are mappings, recurse
						err := recursiveFn(destValueNode, sourceValueNode, newPath)
						if err != nil {
							return err
						}
					} else {
						// Not both mappings, resolve collision
						destValue := interface{}(destValueNode)
						sourceValue := interface{}(sourceValueNode)
						if err != nil {
							return err
						}
						resolvedValue := collisionFn(destValue, sourceValue)
						// Set the resolved value in dest
						newValueNode := &yaml.Node{}
						err = setValue(newValueNode, resolvedValue)
						if err != nil {
							return err
						}
						err = destNode.PipeE(yaml.SetField(key, yaml.NewRNode(newValueNode)))
						if err != nil {
							return err
						}
					}
				}
			}
		} else {
			// Nodes are not both mappings, resolve collision at this node
			destValue := interface{}(destNode)
			sourceValue := interface{}(sourceNode)
			resolvedValue := collisionFn(destValue, sourceValue)
			// Set the resolved value in destNode
			err := setValue(destNode.YNode(), resolvedValue)
			if err != nil {
				return err
			}
		}
		return nil
	}

	return recursiveFn(c.node, source.node, []string{})
}

// Merge merges a source object into an existing destination object. When a collision
// is found within the merged structures (both a source and destination object
// contain the same non-object keys), the result will be a sequence containing both
// values, where values that are already sequences will be expanded into the
// resulting sequence.
//
// It is possible to merge structures with different collision behaviours with
// MergeFn.
func (c *YamlDoc) Merge(source *YamlDoc) error {
	return c.MergeFn(source, func(dest, src interface{}) interface{} {
		destSlice, destIsSlice := dest.([]interface{})
		srcSlice, srcIsSlice := src.([]interface{})

		if destIsSlice {
			if srcIsSlice {
				return append(destSlice, srcSlice...)
			}
			return append(destSlice, src)
		}
		if srcIsSlice {
			return append([]interface{}{dest}, srcSlice...)
		}
		return []interface{}{dest, src}
	})
}

//------------------------------------------------------------------------------

/*
Array modification/search - Keeping these options simple right now, no need for
anything more complicated since you can just cast to []interface{}, modify and
then reassign with Set.
*/

// ArrayAppend attempts to append a value onto a YAML array at a path. If the
// target is not a YAML array then it will be converted into one, with its
// original contents set to the first element of the array.
func (c *YamlDoc) ArrayAppend(value interface{}, hierarchy ...string) error {
	// Get the node at the specified hierarchy
	targetContainer, err := c.searchStrict(hierarchy...)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	var node *yaml.RNode
	if targetContainer != nil {
		node = targetContainer.node
	}

	if node != nil && node.YNode().Kind == yaml.SequenceNode {
		// Node is a sequence, append the value
		valueNode := &yaml.Node{}
		err := setValue(valueNode, value)
		if err != nil {
			return err
		}
		return node.PipeE(yaml.Append(valueNode))
	}

	// Create a new sequence node
	newSequenceNode := &yaml.Node{
		Kind: yaml.SequenceNode,
	}

	// If the node exists and has data, add it as the first element
	if node != nil {
		// Add the existing node as the first element
		newSequenceNode.Content = append(newSequenceNode.Content, node.YNode())
	}

	// Create a node for the new value
	valueNode := &yaml.Node{}
	err = setValue(valueNode, value)
	if err != nil {
		return err
	}
	newSequenceNode.Content = append(newSequenceNode.Content, valueNode)

	// Set the new sequence node at the path
	_, err = c.Set(yaml.NewRNode(newSequenceNode), hierarchy...)
	return err
}

// ArrayAppendP attempts to append a value onto a YAML array at a path using dot
// notation. If the target is not a YAML array then it will be converted into
// one, with its original contents set to the first element of the array.
func (c *YamlDoc) ArrayAppendP(value interface{}, path string) error {
	return c.ArrayAppend(value, DotPathToSlice(path)...)
}

// ArrayConcat attempts to append a value onto a YAML array at a path. If the
// target is not a YAML array then it will be converted into one, with its
// original contents set to the first element of the array.
//
// ArrayConcat differs from ArrayAppend in that it will expand a value of type
// []interface{} during the append operation, resulting in concatenation of each
// element, rather than appending as a single element.
func (c *YamlDoc) ArrayConcat(value interface{}, hierarchy ...string) error {
	// Get the node at the specified hierarchy
	targetContainer, err := c.searchStrict(hierarchy...)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	var node *yaml.RNode
	if targetContainer != nil {
		node = targetContainer.node
	}

	var arrayNode *yaml.Node
	if node != nil && node.YNode().Kind == yaml.SequenceNode {
		// Node is already a sequence
		arrayNode = node.YNode()
	} else {
		// Create a new sequence node
		arrayNode = &yaml.Node{
			Kind: yaml.SequenceNode,
		}

		// If node exists, add it as the first element
		if node != nil {
			arrayNode.Content = append(arrayNode.Content, node.YNode())
		}
	}

	// Now, handle the value
	switch v := value.(type) {
	case []interface{}:
		// Expand the slice and append each element
		for _, elem := range v {
			elemNode := &yaml.Node{}
			err := setValue(elemNode, elem)
			if err != nil {
				return err
			}
			arrayNode.Content = append(arrayNode.Content, elemNode)
		}
	default:
		// Append the value
		valueNode := &yaml.Node{}
		err := setValue(valueNode, value)
		if err != nil {
			return err
		}
		arrayNode.Content = append(arrayNode.Content, valueNode)
	}

	// Set the array node at the path
	_, err = c.Set(yaml.NewRNode(arrayNode), hierarchy...)
	return err
}

// ArrayConcatP attempts to append a value onto a YAML array at a path using dot
// notation. If the target is not a YAML array then it will be converted into one,
// with its original contents set to the first element of the array.
//
// ArrayConcatP differs from ArrayAppendP in that it will expand a value of type
// []interface{} during the append operation, resulting in concatenation of each
// element, rather than appending as a single element.
func (c *YamlDoc) ArrayConcatP(value interface{}, path string) error {
	return c.ArrayConcat(value, DotPathToSlice(path)...)
}

// ArrayRemove attempts to remove an element identified by an index from a YAML
// array at a path.
func (c *YamlDoc) ArrayRemove(index int, hierarchy ...string) error {
	if index < 0 {
		return ErrOutOfBounds
	}
	targetContainer, err := c.searchStrict(hierarchy...)
	if err != nil {
		return err
	}
	node := targetContainer.node
	if node.YNode().Kind != yaml.SequenceNode {
		return ErrNotArray
	}
	elements := node.YNode().Content
	if index >= len(elements) {
		return ErrOutOfBounds
	}
	// Remove the element
	elements = append(elements[:index], elements[index+1:]...)
	// Update the sequence node
	node.YNode().Content = elements
	return nil
}

// ArrayRemoveP attempts to remove an element identified by an index from a YAML
// array at a path using dot notation.
func (c *YamlDoc) ArrayRemoveP(index int, path string) error {
	return c.ArrayRemove(index, DotPathToSlice(path)...)
}

// ArrayElement attempts to access an element by an index from a YAML array at a
// path.
func (c *YamlDoc) ArrayElement(index int, hierarchy ...string) (*YamlDoc, error) {
	if index < 0 {
		return nil, ErrOutOfBounds
	}
	targetContainer, err := c.searchStrict(hierarchy...)
	if err != nil {
		return nil, err
	}
	node := targetContainer.node
	if node.YNode().Kind != yaml.SequenceNode {
		return nil, ErrNotArray
	}
	elements := node.YNode().Content
	if index >= len(elements) {
		return nil, ErrOutOfBounds
	}
	return &YamlDoc{node: yaml.NewRNode(elements[index])}, nil
}

// ArrayElementP attempts to access an element by an index from a YAML array at
// a path using dot notation.
func (c *YamlDoc) ArrayElementP(index int, path string) (*YamlDoc, error) {
	return c.ArrayElement(index, DotPathToSlice(path)...)
}

// ArrayCount counts the number of elements in a YAML array at a path.
func (c *YamlDoc) ArrayCount(hierarchy ...string) (int, error) {
	targetContainer, err := c.searchStrict(hierarchy...)
	if err != nil {
		return 0, err
	}
	node := targetContainer.node
	if node.YNode().Kind != yaml.SequenceNode {
		return 0, ErrNotArray
	}
	count := len(node.YNode().Content)
	return count, nil
}

// ArrayCountP counts the number of elements in a YAML array at a path using dot
// notation.
func (c *YamlDoc) ArrayCountP(path string) (int, error) {
	return c.ArrayCount(DotPathToSlice(path)...)
}

// ArrayInsert attempts to insert an element at a specified index into a YAML array at a path.
func (c *YamlDoc) ArrayInsert(value interface{}, index int, hierarchy ...string) error {
	if index < 0 {
		return ErrOutOfBounds
	}
	targetContainer, err := c.searchStrict(hierarchy...)
	if err != nil {
		return err
	}
	node := targetContainer.node
	if node.YNode().Kind != yaml.SequenceNode {
		return ErrNotArray
	}
	elements := node.YNode().Content
	if index > len(elements) {
		return ErrOutOfBounds
	}

	// Convert value to yaml.Node
	newNode := &yaml.Node{}
	err = setValue(newNode, value)
	if err != nil {
		return err
	}

	// Insert the element at specified index
	elements = append(elements[:index], append([]*yaml.Node{newNode}, elements[index:]...)...)
	node.YNode().Content = elements
	return nil
}

// ArrayInsertP attempts to insert an element at a specified index into a YAML array at a path
// using dot notation.
func (c *YamlDoc) ArrayInsertP(value interface{}, index int, path string) error {
	return c.ArrayInsert(value, index, DotPathToSlice(path)...)
}

func walkMappingNode(path string, node *yaml.RNode, flat map[string]interface{}, includeEmpty bool) error {
	content := node.YNode().Content
	for i := 0; i < len(content); i += 2 {
		keyNode := content[i]
		valueNode := content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue // Skip non-scalar keys
		}
		key := keyNode.Value
		var newPath string
		if path == "" {
			newPath = key
		} else {
			newPath = path + "." + key
		}
		err := walkNode(newPath, yaml.NewRNode(valueNode), flat, includeEmpty)
		if err != nil {
			return err
		}
	}
	return nil
}

func walkSequenceNode(path string, node *yaml.RNode, flat map[string]interface{}, includeEmpty bool) error {
	elements := node.YNode().Content
	for i, elem := range elements {
		var newPath string
		indexStr := strconv.Itoa(i)
		if path == "" {
			newPath = indexStr
		} else {
			newPath = path + "." + indexStr
		}
		err := walkNode(newPath, yaml.NewRNode(elem), flat, includeEmpty)
		if err != nil {
			return err
		}
	}
	return nil
}

func walkNode(path string, node *yaml.RNode, flat map[string]interface{}, includeEmpty bool) error {
	if node == nil {
		return nil
	}
	switch node.YNode().Kind {
	case yaml.MappingNode:
		if includeEmpty && len(node.YNode().Content) == 0 {
			flat[path] = struct{}{}
		}
		return walkMappingNode(path, node, flat, includeEmpty)
	case yaml.SequenceNode:
		if includeEmpty && len(node.YNode().Content) == 0 {
			flat[path] = []struct{}{}
		}
		return walkSequenceNode(path, node, flat, includeEmpty)
	case yaml.ScalarNode:
		if path != "" {
			flat[path] = node.YNode().Value
		}
		return nil
	default:
		return nil
	}
}

// Flatten a YAML array or object into an object of key/value pairs for each
// field, where the key is the full path of the structured field in dot path
// notation matching the spec for the method Path.
//
// Returns an error if the target is not a YAML object or array.
func (c *YamlDoc) Flatten() (map[string]interface{}, error) {
	return c.flatten(false)
}

// FlattenIncludeEmpty a YAML array or object into an object of key/value pairs
// for each field, just as Flatten, but includes empty arrays and objects, where
// the key is the full path of the structured field in dot path notation matching
// the spec for the method Path.
//
// Returns an error if the target is not a YAML object or array.
func (c *YamlDoc) FlattenIncludeEmpty() (map[string]interface{}, error) {
	return c.flatten(true)
}

func (c *YamlDoc) flatten(includeEmpty bool) (map[string]interface{}, error) {
	flattened := make(map[string]interface{})
	err := walkNode("", c.node, flattened, includeEmpty)
	if err != nil {
		return nil, err
	}
	return flattened, nil
}

// Bytes marshals an element to a YAML []byte blob.
func (c *YamlDoc) Bytes() []byte {
	if c == nil || c.node == nil {
		return EmptyDocument
	}
	var (
		data string
		err  error
	)
	ynode := c.node.YNode()
	// Handle empty YAML document case
	if ynode.Kind == yaml.ScalarNode && ynode.Tag == "" && ynode.Value == "" {
		data, err = yaml.String(c.node.YNode(), yaml.Trim)
		// Make sure to add a newline at the end and there must be only one newline
		data = data + "\n"
	} else {
		data, err = c.node.String()
	}
	if err != nil {
		return EmptyDocument
	}
	return []byte(data)
}

// BytesIndent marshals an element to a YAML []byte blob formatted with a specified indent.
// Since YAML inherently supports indentation, this function allows you to set the indentation level.
func (c *YamlDoc) BytesIndent(indent int) []byte {
	if c == nil || c.node == nil {
		return []byte{}
	}
	var b bytes.Buffer
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(indent)
	err := encoder.Encode(c.node.YNode())
	if err != nil {
		return []byte{}
	}
	encoder.Close()
	return b.Bytes()
}

// String marshals an element to a YAML formatted string.
func (c *YamlDoc) String() string {
	return string(c.Bytes())
}

// StringIndent marshals an element to a YAML string formatted with indents.
func (c *YamlDoc) StringIndent(indent int) string {
	return string(c.BytesIndent(indent))
}

// MarshalYAML returns the YAML encoding of this container.
func (c *YamlDoc) MarshalYAML() ([]byte, error) {
	str := c.String()
	return []byte(str), nil
}

// MarshalJSON returns the JSON encoding of this container.
func (c *YamlDoc) MarshalJSON() ([]byte, error) {
	if c == nil || c.node == nil {
		return EmptyDocument, nil
	}
	data, err := c.node.MarshalJSON()
	if err != nil {
		return EmptyDocument, err
	}
	return data, nil
}

// ParseJSON reads a JSON byte slice and returns a *YamlDoc.
func ParseJSON(y []byte) (*YamlDoc, error) {
	node, err := yaml.ConvertJSONToYamlNode(string(y))
	if err != nil {
		return nil, err
	}
	return &YamlDoc{node: node}, nil
}

// ParseYAML reads a YAML byte slice and returns a *YamlDoc.
func ParseYAML(y []byte) (*YamlDoc, error) {
	node, err := yaml.Parse(string(y))
	if errors.Is(err, io.EOF) {
		// TODO: Technically such comments should be attached to the following
		// yaml document.
		// Handle empty YAML document
		childNode := &yaml.Node{
			Kind:        yaml.ScalarNode,
			HeadComment: strings.TrimSpace(string(y)),
		}
		documentNode := &yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{childNode},
		}
		node := yaml.NewRNode(documentNode) // Wrap in RNode
		return &YamlDoc{isEmptyDoc: true, node: node}, nil
	} else if err != nil {
		return nil, err
	}

	return &YamlDoc{node: node}, nil
}

// ParseYAMLFile reads a file and unmarshals the contents into a *YamlDoc.
func ParseYAMLFile(path string) (*YamlDoc, error) {
	if len(path) == 0 {
		return nil, ErrInvalidPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseYAML(data)
}

// ParseYAMLBuffer reads a buffer and unmarshals the contents into a *YamlDoc.
func ParseYAMLBuffer(buffer io.Reader) (*YamlDoc, error) {
	content, err := io.ReadAll(buffer)
	if err != nil {
		return nil, err
	}
	return ParseYAML(content)
}

// New creates a new gyabs YAML object.
func New() *YamlDoc {
	node := &yaml.Node{Kind: yaml.MappingNode}
	return &YamlDoc{node: yaml.NewRNode(node)}
}

// Wrap wraps an existing *yaml.RNode into a *YamlDoc.
func Wrap(node *yaml.RNode) *YamlDoc {
	return &YamlDoc{node: node}
}

func SetRoot(doc *YamlDoc, node *yaml.RNode) {
	doc.node = node
}

func GetRoot(doc *YamlDoc) *yaml.RNode {
	return doc.node
}
