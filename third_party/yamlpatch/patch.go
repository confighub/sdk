package yamlpatch

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/third_party/gaby"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	ErrTestFailed          = errors.New("test failed")
	ErrMissing             = errors.New("missing value")
	ErrMissingKey          = errors.New("missing key")
	ErrUnknownType         = errors.New("unknown object type")
	ErrInvalid             = errors.New("invalid state detected")
	ErrInvalidIndex        = errors.New("invalid index referenced")
	ErrInvalidPatchFormat  = errors.New("invalid patch format")
	ErrAccumulatedCopySize = errors.New("accumulated copy size limit exceeded")
)

// -----------------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------------

// SupportNegativeIndices decides whether to support non-standard practice of
// allowing negative indices to mean indices starting at the end of an array.
// Default to true (mirroring the behavior in the provided jsonpatch example).
var SupportNegativeIndices = true

// AccumulatedCopySizeLimit limits the total size increase in bytes caused by
// "copy" operations in a patch. If 0, there's no limit.
var AccumulatedCopySizeLimit int64 = 0

// -----------------------------------------------------------------------------
// Patch Data Structures
// -----------------------------------------------------------------------------

// Operation is a single YAML Patch step, such as an "add" operation.
type Operation map[string]interface{}

// Patch is an ordered collection of Operations.
type Patch []Operation

// -----------------------------------------------------------------------------
// RFC6901 "pointer" decoding
// (Same transformation used in JSON Patch: "~1" -> "/", "~0" -> "~")
// -----------------------------------------------------------------------------

var rfc6901Decoder = strings.NewReplacer("~1", "/", "~0", "~")

func decodePatchKey(k string) string {
	return rfc6901Decoder.Replace(k)
}

// -----------------------------------------------------------------------------
// Operation Helpers
// -----------------------------------------------------------------------------

func (o Operation) Kind() string {
	if val, ok := o["op"]; ok {
		if opStr, ok2 := val.(string); ok2 {
			return opStr
		}
	}
	return "unknown"
}

func (o Operation) Path() (string, error) {
	if val, ok := o["path"]; ok {
		if pStr, ok2 := val.(string); ok2 {
			return pStr, nil
		}
	}
	return "", errors.Wrap(ErrMissing, `"path" field is missing or not a string`)
}

func (o Operation) From() (string, error) {
	if val, ok := o["from"]; ok {
		if fStr, ok2 := val.(string); ok2 {
			return fStr, nil
		}
	}
	return "", errors.Wrap(ErrMissing, `"from" field is missing or not a string`)
}

// value returns the raw gaby.YamlDoc representing the "value" in the Operation,
// or nil if missing.
func (o Operation) value() (*gaby.YamlDoc, error) {
	val, ok := o["value"]
	if !ok {
		return nil, nil
	}

	// Convert "val" interface{} -> YAML -> *gaby.YamlDoc
	// This is effectively "deep" generation of a YamlDoc from any arbitrary input.
	rawBytes, err := yaml.Marshal(val)
	if err != nil {
		return nil, err
	}

	doc, err := gaby.ParseYAML(rawBytes)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// ValueInterface decodes the "value" field from the Operation back into
// a Go interface{}, similar to the jsonpatch ValueInterface method.
func (o Operation) ValueInterface() (interface{}, error) {
	val, ok := o["value"]
	if !ok {
		return nil, errors.Wrap(ErrMissing, `"value" field is missing`)
	}
	return val, nil
}

// -----------------------------------------------------------------------------
// "Container" interface for partialDoc / partialArray, similar to the JSON version
// -----------------------------------------------------------------------------

type container interface {
	get(key string) (*gaby.YamlDoc, error)
	set(key string, val *gaby.YamlDoc) error
	add(key string, val *gaby.YamlDoc) error
	remove(key string) error
}

// partialDoc wraps a *gaby.YamlDoc that is known to be a MappingNode.
type partialDoc struct {
	doc *gaby.YamlDoc
}

func (d *partialDoc) get(key string) (*gaby.YamlDoc, error) {
	if key == "" {
		return d.doc, nil
	}
	// For objects, key is just a string
	res := d.doc.S(key)
	if res == nil || res.Data() == nil {
		return nil, errors.Wrapf(ErrMissing, "missing key: %s", key)
	}
	return res, nil
}

func (d *partialDoc) set(key string, val *gaby.YamlDoc) error {
	if key == "" {
		gaby.SetRoot(d.doc, gaby.GetRoot(val))
		return nil
	}
	_, err := d.doc.Set(val.YNode(), key)
	return err
}

func (d *partialDoc) add(key string, val *gaby.YamlDoc) error {
	// add is the same as set for an object
	_, err := d.doc.Set(val.YNode(), key)
	return err
}

func (d *partialDoc) remove(key string) error {
	if d.doc.ExistsP(key) == false {
		return errors.New("path not found")
	}
	return d.doc.Delete(key)
}

// partialArray wraps a *gaby.YamlDoc that is known to be a SequenceNode.
type partialArray struct {
	arr *gaby.YamlDoc
}

// get is only meaningful by interpreting key as an integer index.
func (a *partialArray) get(key string) (*gaby.YamlDoc, error) {
	idx, err := strconv.Atoi(key)
	if err != nil {
		return nil, errors.Wrapf(err, "key is not a valid index: %s", key)
	}
	// negative index support
	size, err := a.arr.ArrayCount()
	if err != nil {
		return nil, err
	}
	if idx < 0 {
		if !SupportNegativeIndices {
			return nil, errors.Wrapf(ErrInvalidIndex, "negative index not allowed: %d", idx)
		}
		idx += size
	}
	if idx < 0 || idx >= size {
		return nil, errors.Wrapf(ErrInvalidIndex, "index out of bounds: %d", idx)
	}
	elem, err := a.arr.ArrayElement(idx)
	if err != nil {
		return nil, err
	}
	return elem, nil
}

func (a *partialArray) set(key string, val *gaby.YamlDoc) error {
	if key == "-" {
		return a.arr.ArrayAppend(val.Data())
	}

	idx, err := strconv.Atoi(key)
	if err != nil {
		return errors.Wrapf(err, "invalid array index: %s", key)
	}
	size, err := a.arr.ArrayCount()
	if err != nil {
		return err
	}
	if idx < 0 {
		if !SupportNegativeIndices {
			return errors.Wrapf(ErrInvalidIndex, "negative index not allowed: %d", idx)
		}
		idx += size
	}
	if idx < 0 || idx >= size {
		return errors.Wrapf(ErrInvalidIndex, "index out of range: %d", idx)
	}
	_, err = a.arr.SetIndex(val.YNode(), idx)
	return err
}

func (a *partialArray) add(key string, val *gaby.YamlDoc) error {
	if key == "-" {
		// append
		return a.arr.ArrayAppend(val.YNode())
	}
	idx, err := strconv.Atoi(key)
	if err != nil {
		return errors.Wrapf(err, "value was not a proper array index: '%s'", key)
	}
	size, err := a.arr.ArrayCount()
	if err != nil {
		return err
	}
	if idx < 0 {
		if !SupportNegativeIndices {
			return errors.Wrapf(ErrInvalidIndex, "negative index not allowed: %d", idx)
		}
		idx += size + 1 // +1 since we effectively do an insert
	}
	if idx < 0 || idx > size {
		return errors.Wrapf(ErrInvalidIndex, "invalid index: %d", idx)
	}

	return a.arr.ArrayInsert(val.Data(), idx)
}

func (a *partialArray) remove(key string) error {
	idx, err := strconv.Atoi(key)
	if err != nil {
		return errors.Wrapf(err, "key is not a valid index: %s", key)
	}
	size, err := a.arr.ArrayCount()
	if err != nil {
		return err
	}
	if idx < 0 {
		if !SupportNegativeIndices {
			return errors.Wrapf(ErrInvalidIndex, "negative index not allowed: %d", idx)
		}
		idx += size
	}
	if idx < 0 || idx > size {
		return errors.Wrapf(ErrInvalidIndex, "invalid index: %d", idx)
	}

	return a.arr.ArrayRemove(idx)
}

// -----------------------------------------------------------------------------
// "findContainer" traverses the document path (split by "/"), returning the
// container of the final parent and the final key. This is analogous to
// findObject in the jsonpatch code.
// -----------------------------------------------------------------------------

func findContainer(doc *gaby.YamlDoc, path string) (container, string, error) {
	// According to RFC6901, path is split by "/", ignoring the first empty part.
	// For example: /a/b/c -> ["a", "b", "c"]
	if path == "" {
		return &partialDoc{doc: doc}, "", nil
	}
	if path[0] != '/' {
		return nil, "", errors.Wrapf(ErrMissing, "path must start with '/': %s", path)
	}
	parts := strings.Split(path, "/")[1:] // skip leading slash
	if len(parts) == 0 {
		return nil, "", errors.Wrap(ErrMissing, "path has no segments")
	}

	// decode ~1 -> /, ~0 -> ~
	for i := range parts {
		parts[i] = decodePatchKey(parts[i])
	}

	// Traverse all but the last part
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		currData := doc.Data()
		if currData == nil {
			return nil, "", errors.Wrapf(ErrMissing, "non-existent segment: %s", part)
		}

		switch data := currData.(type) {
		case map[string]interface{}:
			// partialDoc
			res := doc.S(part)
			if res == nil || res.Data() == nil {
				return &partialDoc{doc: doc}, part, ErrMissingKey
			}
			doc = res
		case []interface{}:
			// partialArray
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, "", errors.Wrapf(err, "expected array index but got '%s'", part)
			}
			if idx < 0 {
				if !SupportNegativeIndices {
					return nil, "", errors.Wrapf(ErrInvalidIndex, "negative index not allowed: %d", idx)
				}
				idx += len(data)
			}
			if idx < 0 || idx >= len(data) {
				return nil, "", errors.Wrapf(ErrInvalidIndex, "index out of range: %d", idx)
			}
			elem := doc.S(strconv.Itoa(idx))
			if elem == nil {
				return nil, "", errors.Wrapf(ErrMissing, "array element is nil: index %d", idx)
			}
			doc = elem
		default:
			return nil, "", errors.Wrapf(ErrUnknownType, "unexpected node type at segment: %s", part)
		}
	}

	// Now doc is the parent container, parts[len(parts)-1] is the final key
	finalKey := parts[len(parts)-1]
	switch doc.Data().(type) {
	case map[string]interface{}:
		return &partialDoc{doc: doc}, finalKey, nil
	case []interface{}:
		return &partialArray{arr: doc}, finalKey, nil
	default:
		return nil, "", errors.Wrapf(ErrUnknownType, "cannot get container for final path segment")
	}
}

func findAndCreateContainer(doc *gaby.YamlDoc, path string) (container, string, error) {
	if path == "" {
		return &partialDoc{doc: doc}, "", nil
	}
	if path[0] != '/' {
		return nil, "", errors.Wrapf(ErrMissing, "path must start with '/': %s", path)
	}
	parts := strings.Split(path, "/")[1:] // skip leading slash
	if len(parts) == 0 {
		return nil, "", errors.Wrap(ErrMissing, "path has no segments")
	}

	// decode ~1 -> /, ~0 -> ~
	for i := range parts {
		parts[i] = decodePatchKey(parts[i])
	}

	// Traverse all but the last part
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		currData := doc.Data()
		if currData == nil {
			// Create new map if segment doesn't exist
			doc.Set(map[string]interface{}{}, "")
			currData = doc.Data()
		}

		switch data := currData.(type) {
		case map[string]interface{}:
			res := doc.S(part)
			if res == nil || res.Data() == nil {
				// Create new map for missing key
				doc.Set(map[string]interface{}{}, part)
				res = doc.S(part)
			}
			doc = res
		case []interface{}:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, "", errors.Wrapf(err, "expected array index but got '%s'", part)
			}
			if idx < 0 {
				if !SupportNegativeIndices {
					return nil, "", errors.Wrapf(ErrInvalidIndex, "negative index not allowed: %d", idx)
				}
				idx += len(data)
			}
			if idx < 0 || idx >= len(data) {
				return nil, "", errors.Wrapf(ErrInvalidIndex, "index out of range: %d", idx)
			}
			elem := doc.S(strconv.Itoa(idx))
			if elem == nil {
				return nil, "", errors.Wrapf(ErrMissing, "array element is nil: index %d", idx)
			}
			doc = elem
		default:
			return nil, "", errors.Wrapf(ErrUnknownType, "unexpected node type at segment: %s", part)
		}
	}

	// Now doc is the parent container, parts[len(parts)-1] is the final key
	finalKey := parts[len(parts)-1]
	switch doc.Data().(type) {
	case map[string]interface{}:
		return &partialDoc{doc: doc}, finalKey, nil
	case []interface{}:
		return &partialArray{arr: doc}, finalKey, nil
	default:
		return nil, "", errors.Wrapf(ErrUnknownType, "cannot get container for final path segment")
	}
}

// -----------------------------------------------------------------------------
// deepCopy using gaby
// -----------------------------------------------------------------------------

func deepCopy(src *gaby.YamlDoc) (*gaby.YamlDoc, int, error) {
	if src == nil {
		return nil, 0, nil
	}
	// Marshal to YAML
	data := src.Bytes()
	// The "size" we measure is simply the length of the YAML text
	sz := len(data)
	// Parse it back into a brand new *gaby.YamlDoc
	newDoc, err := gaby.ParseYAML(data)
	if err != nil {
		return nil, 0, err
	}
	return newDoc, sz, nil
}

// -----------------------------------------------------------------------------
// patch operations
// -----------------------------------------------------------------------------

func (p Patch) applyAdd(doc *gaby.YamlDoc, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrap(err, "add operation failed to get path")
	}
	valueDoc, err := op.value()
	if err != nil {
		return errors.Wrap(err, "add operation invalid value")
	}

	con, key, err := findAndCreateContainer(doc, path)
	if err != nil {
		return errors.Wrapf(err, "add operation does not apply")
	}

	if err := con.add(key, valueDoc); err != nil {
		return errors.Wrapf(err, "failed to add value at path '%s'", path)
	}

	return nil
}

func (p Patch) applyRemove(doc *gaby.YamlDoc, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrap(err, "remove operation failed to get path")
	}
	con, key, err := findContainer(doc, path)
	if err != nil {
		return errors.Wrapf(err, "remove operation does not apply")
	}
	if err := con.remove(key); err != nil {
		return errors.Wrapf(err, "failed to remove value at path '%s'", path)
	}
	return nil
}

func (p Patch) applyReplace(doc *gaby.YamlDoc, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrap(err, "replace operation failed to get path")
	}
	valueDoc, err := op.value()
	if err != nil {
		return errors.Wrap(err, "replace operation invalid value")
	}

	con, key, err := findContainer(doc, path)
	if err != nil {
		return errors.Wrapf(err, "replace operation does not apply")
	}

	// "replace" requires that key already exists:
	_, getErr := con.get(key)
	if getErr != nil {
		return errors.Wrapf(ErrMissing, "replace operation: no existing key '%s' in path '%s'", key, path)
	}

	if err := con.set(key, valueDoc); err != nil {
		return errors.Wrapf(err, "failed to replace value at path '%s'", path)
	}
	return nil
}

func (p Patch) applyMove(doc *gaby.YamlDoc, op Operation) error {
	from, err := op.From()
	if err != nil {
		return errors.Wrap(err, "move operation failed to get from")
	}
	conSrc, keySrc, err := findContainer(doc, from)
	if err != nil {
		return errors.Wrapf(err, "move operation does not apply: missing source path: %s", from)
	}
	val, err := conSrc.get(keySrc)
	if err != nil {
		return errors.Wrapf(err, "cannot get source value for move: %s", from)
	}
	if err := conSrc.remove(keySrc); err != nil {
		return errors.Wrapf(err, "cannot remove source value for move: %s", from)
	}

	path, err := op.Path()
	if err != nil {
		return errors.Wrap(err, "move operation failed to get path")
	}
	conDest, keyDest, err := findContainer(doc, path)
	if err != nil {
		return errors.Wrapf(err, "move operation does not apply: missing destination path: %s", path)
	}
	if err := conDest.add(keyDest, val); err != nil {
		return errors.Wrapf(err, "cannot add value to destination for move: %s", path)
	}
	return nil
}

func (p Patch) applyTest(doc *gaby.YamlDoc, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrap(err, "test operation failed to get path")
	}
	valueDoc, err := op.value()
	if err != nil {
		return errors.Wrap(err, "test operation invalid value")
	}

	con, key, err := findContainer(doc, path)
	if err != nil {
		return errors.Wrapf(err, "test operation does not apply: path not found: %s", path)
	}
	currentVal, err := con.get(key)
	if err != nil {
		return errors.Wrapf(err, "test operation cannot retrieve value at path: %s", path)
	}

	// Compare the data via reflect.DeepEqual
	// We can do a = currentVal.Data(), b = valueDoc.Data()
	a := currentVal.Data()
	b := interface{}(nil)
	if valueDoc != nil {
		b = valueDoc.Data()
	}
	if !reflect.DeepEqual(a, b) {
		return errors.Wrapf(ErrTestFailed, "testing value at path '%s' failed", path)
	}
	return nil
}

func (p Patch) applyCopy(doc *gaby.YamlDoc, op Operation, accumulatedSize *int64) error {
	from, err := op.From()
	if err != nil {
		return errors.Wrap(err, "copy operation failed to get from")
	}
	conSrc, keySrc, err := findContainer(doc, from)
	if err != nil {
		return errors.Wrapf(err, "copy operation does not apply: missing from path: %s", from)
	}
	val, err := conSrc.get(keySrc)
	if err != nil {
		return errors.Wrapf(err, "cannot get source value for copy: %s", from)
	}

	path, err := op.Path()
	if err != nil {
		return errors.Wrap(err, "copy operation failed to get path")
	}
	conDest, keyDest, err := findContainer(doc, path)
	if err != nil {
		return errors.Wrapf(err, "copy operation does not apply: missing destination path: %s", path)
	}

	// deepCopy
	valCopy, size, err := deepCopy(val)
	if err != nil {
		return errors.Wrap(err, "error in deep copy for copy operation")
	}

	(*accumulatedSize) += int64(size)
	if AccumulatedCopySizeLimit > 0 && *accumulatedSize > AccumulatedCopySizeLimit {
		return errors.Wrapf(ErrAccumulatedCopySize,
			"limit: %d, used: %d",
			AccumulatedCopySizeLimit,
			*accumulatedSize,
		)
	}

	if err := conDest.set(keyDest, valCopy); err != nil {
		return errors.Wrapf(err, "cannot add copied value to path: %s", path)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Main patch application
// -----------------------------------------------------------------------------

// Apply applies a yamlpatch Patch to a YAML document. Returns the new document.
func (p Patch) Apply(doc []byte) ([]byte, error) {
	// Parse the YAML doc into a YamlDoc
	root, err := gaby.ParseYAML(doc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse YAML document")
	}

	var accumulatedCopySize int64 = 0
	for _, op := range p {
		switch op.Kind() {
		case "add":
			err = p.applyAdd(root, op)
		case "remove":
			err = p.applyRemove(root, op)
		case "replace":
			err = p.applyReplace(root, op)
		case "move":
			err = p.applyMove(root, op)
		case "test":
			err = p.applyTest(root, op)
		case "copy":
			err = p.applyCopy(root, op, &accumulatedCopySize)
		default:
			err = fmt.Errorf("unexpected operation kind: %s", op.Kind())
		}
		if err != nil {
			return nil, err
		}
	}

	// Return the updated YAML
	return root.Bytes(), nil
}

// ApplyIndent is like Apply but returns an indented YAML string using
// the provided indentation.
func (p Patch) ApplyIndent(doc []byte, indent int) ([]byte, error) {
	patched, err := p.Apply(doc)
	if err != nil {
		return nil, err
	}
	// Re-parse and re-render with indentation for a stable format
	parsed, err := gaby.ParseYAML(patched)
	if err != nil {
		return nil, err
	}
	return parsed.BytesIndent(indent), nil
}

// -----------------------------------------------------------------------------
// Utility: DecodePatch from YAML
// -----------------------------------------------------------------------------

// DecodePatch decodes a YAML patch document into a Patch.
// The patch document itself is expected to be a sequence of operations.
//
// Example of a YAML patch document:
//
// ```yaml
//
//   - op: add
//     path: /items/-
//     value:
//     name: "New item"
//
//   - op: remove
//     path: /items/1
//
// ```
func DecodePatch(y []byte) (Patch, error) {
	var rawOps []map[string]interface{}
	if err := yaml.Unmarshal(y, &rawOps); err != nil {
		return nil, errors.Wrap(err, "failed to decode patch")
	}
	patch := make(Patch, len(rawOps))
	for i, op := range rawOps {
		patch[i] = op
	}
	return patch, nil
}

// -----------------------------------------------------------------------------
// Utility: Equal
// -----------------------------------------------------------------------------

// Equal indicates if 2 YAML documents have the same structural equality
// by parsing them into *gaby.YamlDoc and comparing their .Data() with
// reflect.DeepEqual.
func Equal(a, b []byte) bool {
	docA, errA := gaby.ParseYAML(a)
	docB, errB := gaby.ParseYAML(b)
	if errA != nil || errB != nil {
		// If either fails to parse, treat them as not equal
		return false
	}
	return reflect.DeepEqual(docA.Data(), docB.Data())
}
