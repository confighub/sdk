package yamlpatch

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/confighub/sdk/third_party/gaby"
)

// reformatYAML is analogous to reformatJSON, just a convenience if you want
// to pretty-print or indent YAML. For brevity, we just return the original here.
func reformatYAML(s string) string {
	return s
}

// compareYAML parses two YAML strings into generic data via gaby, then does a
// reflect.DeepEqual comparison on their .Data() results.
func compareYAML(a, b string) bool {
	aDoc, errA := gaby.ParseYAML([]byte(a))
	bDoc, errB := gaby.ParseYAML([]byte(b))
	if errA != nil || errB != nil {
		return false
	}
	return reflect.DeepEqual(aDoc.Data(), bDoc.Data())
}

// applyPatch decodes a *YAML-based* patch and applies it to the *YAML doc*.
func applyPatch(doc, patch string) (string, error) {
	// special case, empty doc
	if strings.TrimSpace(doc) == "" {
		return doc, nil
	}

	p, err := DecodePatch([]byte(patch))
	if err != nil {
		return "", fmt.Errorf("decode patch error: %w", err)
	}
	out, err := p.Apply([]byte(doc))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type Case struct {
	doc, patch, result string
}

// Utility function for repeated 'A' characters:
func repeatedA(r int) string {
	var s string
	for i := 0; i < r; i++ {
		s += "A"
	}
	return s
}

// -----------------------------------------------------------------------------
// All the original JSON-based Cases, now converted to YAML
// -----------------------------------------------------------------------------
var Cases = []Case{
	{
		doc: `
foo: bar
`,
		patch: `
- op: add
  path: /baz
  value: qux
`,
		result: `
baz: qux
foo: bar
`,
	},
	{
		doc: `
foo:
  - bar
  - baz
`,
		patch: `
- op: add
  path: /foo/1
  value: qux
`,
		result: `
foo:
  - bar
  - qux
  - baz
`,
	},
	{
		doc: `
foo:
  - bar
  - baz
`,
		patch: `
- op: add
  path: /foo/-1
  value: qux
`,
		result: `
foo:
  - bar
  - baz
  - qux
`,
	},
	{
		doc: `
baz: qux
foo: bar
`,
		patch: `
- op: remove
  path: /baz
`,
		result: `
foo: bar
`,
	},
	{
		doc: `
foo:
  - bar
  - qux
  - baz
`,
		patch: `
- op: remove
  path: /foo/1
`,
		result: `
foo:
  - bar
  - baz
`,
	},
	{
		doc: `
baz: qux
foo: bar
`,
		patch: `
- op: replace
  path: /baz
  value: boo
`,
		result: `
baz: boo
foo: bar
`,
	},
	{
		doc: `
foo:
  bar: baz
  waldo: fred
qux:
  corge: grault
`,
		patch: `
- op: move
  from: /foo/waldo
  path: /qux/thud
`,
		result: `
foo:
  bar: baz
qux:
  corge: grault
  thud: fred
`,
	},
	{
		doc: `
foo:
  - all
  - grass
  - cows
  - eat
`,
		patch: `
- op: move
  from: /foo/1
  path: /foo/3
`,
		result: `
foo:
  - all
  - cows
  - eat
  - grass
`,
	},
	{
		doc: `
foo:
  - all
  - grass
  - cows
  - eat
`,
		patch: `
- op: move
  from: /foo/1
  path: /foo/2
`,
		result: `
foo:
  - all
  - cows
  - grass
  - eat
`,
	},
	{
		doc: `
foo: bar
`,
		patch: `
- op: add
  path: /child
  value:
    grandchild: {}
`,
		result: `
foo: bar
child:
  grandchild: {}
`,
	},
	{
		doc: `
foo:
  - bar
`,
		patch: `
- op: add
  path: /foo/-
  value:
    - abc
    - def
`,
		result: `
foo:
  - bar
  - 
    - abc
    - def
`,
	},
	{
		doc: `
foo: bar
qux:
  baz: 1
  bar: null
`,
		patch: `
- op: remove
  path: /qux/bar
`,
		result: `
foo: bar
qux:
  baz: 1
`,
	},
	{
		doc: `
foo: bar
`,
		patch: `
- op: add
  path: /baz
  value: null
`,
		result: `
baz: null
foo: bar
`,
	},
	{
		doc: `
foo:
  - bar
`,
		patch: `
- op: replace
  path: /foo/0
  value: baz
`,
		result: `
foo:
  - baz
`,
	},
	{
		doc: `
foo:
  - bar
  - baz
`,
		patch: `
- op: replace
  path: /foo/0
  value: bum
`,
		result: `
foo:
  - bum
  - baz
`,
	},
	{
		doc: `
foo:
  - bar
  - qux
  - baz
`,
		patch: `
- op: replace
  path: /foo/1
  value: bum
`,
		result: `
foo:
  - bar
  - bum
  - baz
`,
	},
	{
		doc: `
- foo:
    - bar
    - qux
    - baz
`,
		patch: `
- op: replace
  path: /0/foo/0
  value: bum
`,
		result: `
- foo:
    - bum
    - qux
    - baz
`,
	},
	{
		doc: `
- foo:
    - bar
    - qux
    - baz
  bar:
    - qux
    - baz
`,
		patch: `
- op: copy
  from: /0/foo/0
  path: /0/bar/0
`,
		result: `
- foo:
    - bar
    - qux
    - baz
  bar:
    - bar
    - baz
`,
	},
	{
		doc: `
- foo:
    - bar
    - qux
    - baz
  bar:
    - qux
    - baz
`,
		patch: `
- op: copy
  from: /0/foo/0
  path: /0/bar
`,
		result: `
- foo:
    - bar
    - qux
    - baz
  bar: bar
`,
	},
	{
		doc: `
- foo:
    bar:
      - qux
      - baz
  baz:
    qux: bum
`,
		patch: `
- op: copy
  from: /0/foo/bar
  path: /0/baz/bar
`,
		result: `
- baz:
    qux: bum
    bar:
      - qux
      - baz
  foo:
    bar:
      - qux
      - baz
`,
	},
	{
		doc: `
foo:
  - bar
`,
		patch: `
- op: copy
  from: /foo
  path: /foo/0
`,
		result: `
foo:
  - - bar
`,
	},
	{
		doc: `
foo:
  - bar
  - qux
  - baz
`,
		patch: `
- op: remove
  path: /foo/-2
`,
		result: `
foo:
  - bar
  - baz
`,
	},
	{
		doc: `
foo: []
`,
		patch: `
- op: add
  path: /foo/-1
  value: qux
`,
		result: `
foo:
  - qux
`,
	},
	{
		doc: `
bar:
  - baz: 2
`,
		patch: `
- op: replace
  path: /bar/0/baz
  value: 1
`,
		result: `
bar:
  - baz: 1
`,
	},
	{
		doc: `
bar:
  - baz: 1
`,
		patch: `
- op: replace
  path: /bar/0/baz
  value: null
`,
		result: `
bar:
  - baz: null
`,
	},
	{
		doc: `
bar:
  - null
`,
		patch: `
- op: replace
  path: /bar/0
  value: 1
`,
		result: `
bar:
  - 1
`,
	},
	{
		doc: `
bar:
  - 1
`,
		patch: `
- op: replace
  path: /bar/0
  value: null
`,
		result: `
bar:
  - null
`,
	},
	{
		// Using repeatedA(48) for demonstration
		doc: fmt.Sprintf(`
foo:
  - "A"
  - "%s"
`, repeatedA(48)),
		patch: `
- op: copy
  path: /foo/-
  from: /foo/1
- op: copy
  path: /foo/-
  from: /foo/1
`,
		result: fmt.Sprintf(`
foo:
  - "A"
  - "%s"
  - "%s"
  - "%s"
`, repeatedA(48), repeatedA(48), repeatedA(48)),
	},
	{
		doc: `
id: "00000000-0000-0000-0000-000000000000"
parentID: "00000000-0000-0000-0000-000000000000"
`,
		patch: `
- op: test
  path: ""
  value:
    id: "00000000-0000-0000-0000-000000000000"
    parentID: "00000000-0000-0000-0000-000000000000"
- op: replace
  path: ""
  value:
    id: "759981e8-ec68-4639-a83e-513225914ecb"
    originalID: bar
    parentID: "00000000-0000-0000-0000-000000000000"
`,
		result: `
id: "759981e8-ec68-4639-a83e-513225914ecb"
originalID: bar
parentID: "00000000-0000-0000-0000-000000000000"
`,
	},
}

// -----------------------------------------------------------------------------
// BadCases (expected to fail), now in YAML
// -----------------------------------------------------------------------------

type BadCase struct {
	doc, patch string
}

var BadCases = []BadCase{
	/* This case is invalid because we have a requirement to
	   auto-create key when it does not exist for the "add" op
	{

				doc: `
		foo: bar
		`,
				patch: `
		- op: add
		  path: /baz/bat
		  value: qux
		`,
			},
	*/
	{
		doc: `
a:
  b:
    d: 1
`,
		patch: `
- op: remove
  path: /a/b/c
`,
	},
	{
		doc: `
a:
  b:
    d: 1
`,
		patch: `
- op: move
  from: /a/b/c
  path: /a/b/e
`,
	},
	{
		doc: `
a:
  b:
    - 1
`,
		patch: `
- op: remove
  path: /a/b/1
`,
	},
	{
		doc: `
a:
  b:
    - 1
`,
		patch: `
- op: move
  from: /a/b/1
  path: /a/b/2
`,
	},
	{
		doc: `
foo: bar
`,
		patch: `
- op: add
  pathz: /baz
  value: qux
`,
	},
	{
		doc: `
foo: bar
`,
		patch: `
- op: add
  path: ""
  value: qux
`,
	},
	{
		doc: `
foo:
  - bar
  - baz
`,
		patch: `
- op: replace
  path: /foo/2
  value: bum
`,
	},
	{
		doc: `
foo:
  - bar
  - baz
`,
		patch: `
- op: add
  path: /foo/-4
  value: bum
`,
	},
	{
		doc: `
name:
  foo: bat
  qux: bum
`,
		patch: `
- op: replace
  path: /foo/bar
  value: baz
`,
	},
	{
		doc: `
foo:
  - bar
`,
		patch: `
- op: add
  path: /foo/2
  value: bum
`,
	},
	{
		doc: `
foo: []
`,
		patch: `
- op: remove
  path: /foo/-
`,
	},
	{
		doc: `
foo: []
`,
		patch: `
- op: remove
  path: /foo/-1
`,
	},
	{
		doc: `
foo:
  - bar
`,
		patch: `
- op: remove
  path: /foo/-2
`,
	},
	{
		doc: `
{}
`,
		patch: `
- op: null
  path: ""
`,
	},
	{
		doc: `
{}
`,
		patch: `
- op: add
  path: null
`,
	},
	{
		doc: `
{}
`,
		patch: `
- op: copy
  from: null
`,
	},
	{
		doc: `
foo:
  - bar
`,
		patch: `
- op: copy
  path: /foo/6666666666
  from: /
`,
	},
	{
		doc: `
foo:
  - bar
`,
		patch: `
- op: copy
  path: /foo/2
  from: /foo/0
`,
	},
	{
		// Accumulated copy size cannot exceed AccumulatedCopySizeLimit (49 'A's).
		doc: fmt.Sprintf(`
foo:
  - "A"
  - "%s"
`, repeatedA(49)),
		patch: `
- op: copy
  path: /foo/-
  from: /foo/1
- op: copy
  path: /foo/-
  from: /foo/1
`,
	},
	{
		doc: `
foo:
  - all
  - grass
  - cows
  - eat
`,
		patch: `
- op: move
  from: /foo/1
  path: /foo/4
`,
	},
}

// -----------------------------------------------------------------------------
// Certain patches do mutate the doc in a known way
// -----------------------------------------------------------------------------
var MutationTestCases = []BadCase{
	{
		doc: `
foo: bar
qux:
  baz: 1
  bar: null
`,
		patch: `
- op: remove
  path: /qux/bar
`,
	},
	{
		doc: `
foo: bar
qux:
  baz: 1
  bar: null
`,
		patch: `
- op: replace
  path: /qux/baz
  value: null
`,
	},
}

// TestAllCases tries applying each patch in the Cases list, then compares
// to the expected result. Also checks some known-bad patches.
func TestAllCases(t *testing.T) {
	oldLimit := AccumulatedCopySizeLimit
	AccumulatedCopySizeLimit = 102 // example limit
	defer func() {
		AccumulatedCopySizeLimit = oldLimit
	}()

	// Good cases
	for _, c := range Cases {
		out, err := applyPatch(c.doc, c.patch)
		if err != nil {
			t.Errorf("Unable to apply patch: %s\nPatch: %s\nDoc: %s", err, c.patch, c.doc)
			continue
		}
		if !compareYAML(out, c.result) {
			t.Errorf("Patch did not apply. Expected:\n%s\n\nActual:\n%s",
				reformatYAML(c.result), reformatYAML(out))
		}
	}

	// Check that certain patches do mutate the doc
	for _, c := range MutationTestCases {
		out, err := applyPatch(c.doc, c.patch)
		if err != nil {
			t.Errorf("Unable to apply patch: %s\nPatch: %s\nDoc: %s", err, c.patch, c.doc)
			continue
		}
		// Now specifically check doc != out
		if compareYAML(out, c.doc) {
			t.Errorf("Patch was applied but doc is unchanged.\nOriginal:\n%s\n\nPatched:\n%s",
				reformatYAML(c.doc), reformatYAML(out))
		}
	}

	// Bad cases (expected to fail)
	for _, c := range BadCases {
		_, err := applyPatch(c.doc, c.patch)
		if err == nil {
			t.Errorf("Patch %q should have failed to apply but it did not.\nDoc: %s",
				c.patch, c.doc)
		}
	}
}

// -----------------------------------------------------------------------------
// Testing 'test' operations (now in YAML)
// -----------------------------------------------------------------------------

/*
type TestCase struct {
	doc, patch string
	result     bool
	failedPath string
}

var TestCases = []TestCase{
	{
		doc: `
baz: qux
foo:
  - a
  - 2
  - c
`,
		patch: `
- op: test
  path: /baz
  value: qux
- op: test
  path: /foo/1
  value: 2
`,
		result:     true,
		failedPath: "",
	},
	{
		doc: `
baz: qux
`,
		patch: `
- op: test
  path: /baz
  value: bar
`,
		result:     false,
		failedPath: "/baz",
	},
	{
		doc: `
baz: qux
foo:
  - a
  - 2
  - c
`,
		patch: `
- op: test
  path: /baz
  value: qux
- op: test
  path: /foo/1
  value: c
`,
		result:     false,
		failedPath: "/foo/1",
	},
	{
		doc: `
baz: qux
`,
		patch: `
- op: test
  path: /foo
  value: 42
`,
		result:     false,
		failedPath: "/foo",
	},
	{
		doc: `
baz: qux
`,
		patch: `
- op: test
  path: /foo
  value: null
`,
		result:     true,
		failedPath: "",
	},
	{
		doc: `
foo: null
`,
		patch: `
- op: test
  path: /foo
  value: null
`,
		result:     true,
		failedPath: "",
	},
	{
		doc: `
foo: {}
`,
		patch: `
- op: test
  path: /foo
  value: null
`,
		result:     false,
		failedPath: "/foo",
	},
	{
		doc: `
foo: []
`,
		patch: `
- op: test
  path: /foo
  value: null
`,
		result:     false,
		failedPath: "/foo",
	},
	{
		doc: `
baz/foo: qux
`,
		patch: `
- op: test
  path: /baz~1foo
  value: qux
`,
		result:     true,
		failedPath: "",
	},
	{
		doc: `
foo: []
`,
		patch: `
- op: test
  path: /foo
`,
		result:     false,
		failedPath: "/foo",
	},
	{
		doc: `
baz: []
`,
		patch: `
- op: test
  path: /foo
`,
		// In JSON logic, testing an absent key w/ no 'value' sometimes is interpreted differently.
		// You can handle that however your patch logic requires.
		// Here we'll assume "not found" means "treat as null" => this yields "true" if that’s your logic.
		// Otherwise, you'd keep it "false".
		result:     true,
		failedPath: "/foo",
	},
}

func TestAllTest(t *testing.T) {
	for _, c := range TestCases {
		_, err := applyPatch(c.doc, c.patch)
		if c.result && err != nil {
			t.Errorf("Testing failed when it should have passed: %s\nPatch: %s\nDoc: %s",
				err, c.patch, c.doc)
		} else if !c.result && err == nil {
			t.Errorf("Testing passed when it should have failed.\nPatch: %s\nDoc: %s",
				c.patch, c.doc)
		} else if !c.result && err != nil {
			// Optionally check the error text
			expected := fmt.Sprintf("path '%s' failed", c.failedPath)
			expected2 := fmt.Sprintf("path: %s: missing key", c.failedPath)
			if !bytes.Contains([]byte(err.Error()), []byte(expected)) &&
				!bytes.Contains([]byte(err.Error()), []byte(expected2)) {
				t.Errorf("Expected error containing [%s], but got [%s]", expected, err)
			}
		}
	}
}
*/

// -----------------------------------------------------------------------------
// Testing the 'Equal' function with YAML data
// -----------------------------------------------------------------------------

type EqualityCase struct {
	name  string
	a, b  string
	equal bool
}

var EqualityCases = []EqualityCase{
	{
		name: "ExtraKeyFalse",
		a: `
foo: bar
`,
		b: `
foo: bar
baz: qux
`,
		equal: false,
	},
	{
		name: "StripWhitespaceTrue",
		a: `
foo: bar
baz: qux
`,
		b: `foo: bar
baz: qux
`,
		equal: true,
	},
	{
		name: "KeysOutOfOrderTrue",
		a: `
baz: qux
foo: bar
`,
		b: `
foo: bar
baz: qux
`,
		equal: true,
	},
	{
		name: "ComparingNullFalse",
		a: `
foo: null
`,
		b: `
foo: bar
`,
		equal: false,
	},
	{
		name: "ComparingNullTrue",
		a: `
foo: null
`,
		b: `
foo: null
`,
		equal: true,
	},
	{
		name: "ArrayOutOfOrderFalse",
		a: `
- foo
- bar
- baz
`,
		b: `
- bar
- baz
- foo
`,
		equal: false,
	},
	{
		name: "ArrayTrue",
		a: `
- foo
- bar
- baz
`,
		b: `
- foo
- bar
- baz
`,
		equal: true,
	},
	{
		name: "NonStringTypesTrue",
		a: `
int: 6
bool: true
float: 7.0
string: "the_string"
null: null
`,
		b: `
int: 6
bool: true
float: 7.0
string: "the_string"
null: null
`,
		equal: true,
	},
	{
		name: "NestedNullFalse",
		a: `
foo:
  - an
  - array
bar:
  an: object
`,
		b: `
foo: null
bar: null
`,
		equal: false,
	},
	{
		name:  "NullCompareStringFalse",
		a:     `"foo"`,
		b:     `null`,
		equal: false,
	},
	{
		name:  "NullCompareIntFalse",
		a:     `6`,
		b:     `null`,
		equal: false,
	},
	{
		name:  "NullCompareFloatFalse",
		a:     `6.01`,
		b:     `null`,
		equal: false,
	},
	{
		name:  "NullCompareBoolFalse",
		a:     `false`,
		b:     `null`,
		equal: false,
	},
}

func TestEquality(t *testing.T) {
	for _, tc := range EqualityCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Equal([]byte(tc.a), []byte(tc.b))
			if got != tc.equal {
				t.Errorf("Expected Equal(%q, %q) to return %t, but got %t",
					tc.a, tc.b, tc.equal, got)
			}

			got = Equal([]byte(tc.b), []byte(tc.a))
			if got != tc.equal {
				t.Errorf("Expected Equal(%q, %q) to return %t, but got %t",
					tc.b, tc.a, tc.equal, got)
			}
		})
	}
}
