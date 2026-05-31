package gaby

import (
	"bytes"
	"reflect"
	"testing"
)

func TestBasic(t *testing.T) {
	sample := []byte(`test:
  value: 10
test2: 20
`)

	val, err := ParseYAML(sample)
	if err != nil {
		t.Errorf("Failed to parse: %v", err)
		return
	}

	if result, ok := val.Search([]string{"test", "value"}...).Data().(int); ok {
		if result != 10 {
			t.Errorf("Wrong value of result: %v", result)
		}
	} else {
		t.Errorf("Didn't find test.value")
	}

	if _, ok := val.Search("test2", "value").Data().(string); ok {
		t.Errorf("Somehow found a field that shouldn't exist")
	}

	if result, ok := val.Search("test2").Data().(int); ok {
		if result != 20 {
			t.Errorf("Wrong value of result: %v", result)
		}
	} else {
		t.Errorf("Didn't find test2")
	}

	if result := val.Bytes(); !bytes.Equal(result, sample) {
		t.Errorf("Wrong []byte conversion: %s != %s", result, sample)
	}
}

func TestNilMethods(t *testing.T) {
	var n *YamlDoc
	if exp, act := "null", n.String(); exp != act {
		t.Errorf("Unexpected value: %v != %v", act, exp)
	}
	if exp, act := "null", string(n.Bytes()); exp != act {
		t.Errorf("Unexpected value: %v != %v", act, exp)
	}
	if n.Search("foo", "bar") != nil {
		t.Error("non nil result")
	}
	if n.Path("foo.bar") != nil {
		t.Error("non nil result")
	}
	if _, err := n.Array("foo"); err == nil {
		t.Error("expected error")
	}
	if err := n.ArrayAppend("foo", "bar"); err == nil {
		t.Error("expected error")
	}
	if err := n.ArrayRemove(1, "foo", "bar"); err == nil {
		t.Error("expected error")
	}
	if n.Exists("foo", "bar") {
		t.Error("expected false")
	}
	if n.Index(1) != nil {
		t.Error("non nil result")
	}
	if n.Children() != nil {
		t.Error("non nil result")
	}
	if len(n.ChildrenMap()) > 0 {
		t.Error("non nil result")
	}
	if err := n.Delete("foo"); err == nil {
		t.Error("expected error")
	}
}

var bigSample = []byte(`a:
  nested1:
    value1: 5
"": 
  can we access: "this?"
what/a/pain: "ouch1"
what~a~pain: "ouch2"
what~/a/~pain: "ouch3"
what.a.pain: "ouch4"
what~.a.~pain: "ouch5"
b: 10
c:
  - "first"
  - "second"
  - nested2:
      value2: 15
  - 
    - "fifth"
    - "sixth"
  - "fourth"
d:
  "":
    what about: "this?"
`)

func TestDotPath(t *testing.T) {
	type testCase struct {
		path  string
		value string
	}
	tests := []testCase{
		{
			path:  "foo",
			value: "null",
		},
		{
			path:  "a.doesnotexist",
			value: "null",
		},
		{
			path: "a",
			value: `nested1:
  value1: 5
`,
		},
		{
			path:  "what/a/pain",
			value: `"ouch1"` + "\n",
		},
		{
			path:  "what~0a~0pain",
			value: `"ouch2"` + "\n",
		},
		{
			path:  "what~0/a/~0pain",
			value: `"ouch3"` + "\n",
		},
		{
			path:  "what~1a~1pain",
			value: `"ouch4"` + "\n",
		},
		{
			path:  "what~0~1a~1~0pain",
			value: `"ouch5"` + "\n",
		},
		/* special cases, not supported yet
			{
				path:  "",
				value: `"can we access": "this?"` + "\n",
			},
		{
			path:  ".can we access",
			value: `"this?"`,
		},
		{
			path:  "d.",
			value: `{"what about":"this?"}`,
		},
		{
			path:  "d..what about",
			value: `"this?"`,
		}, */
		{
			path:  "c.1",
			value: `"second"` + "\n",
		},
		{
			path:  "c.2.nested2.value2",
			value: `15` + "\n",
		},
		{
			path:  "c.notindex.value2",
			value: "null",
		},
		{
			path:  "c.10.value2",
			value: "null",
		},
	}

	root, err := ParseYAML(bigSample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	for _, test := range tests {
		t.Run(test.path, func(tt *testing.T) {
			result := root.Path(test.path)
			if exp, act := test.value, result.String(); exp != act {
				tt.Errorf("Wrong result: %v != %v", act, exp)
			}
		})
	}
}

func TestArrayWildcard(t *testing.T) {
	sample := []byte(`test:
  - value: 10
  - value: 20
`)

	val, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if act, ok := val.Search([]string{"test", "0", "value"}...).Data().(int); ok {
		if exp := int(10); !reflect.DeepEqual(act, exp) {
			t.Errorf("Wrong result: %v != %v", act, exp)
		}
	} else {
		t.Errorf("Didn't find test.0.value")
	}

	if act, ok := val.Search([]string{"test", "1", "value"}...).Data().(int); ok {
		if exp := int(20); !reflect.DeepEqual(act, exp) {
			t.Errorf("Wrong result: %v != %v", act, exp)
		}
	} else {
		t.Errorf("Didn't find test.1.value")
	}

	if act, ok := val.Search([]string{"test", "*", "value"}...).Data().([]interface{}); ok {
		if exp := []interface{}{10, 20}; !reflect.DeepEqual(act, exp) {
			t.Errorf("Wrong result: %v != %v", act, exp)
		}
	} else {
		t.Errorf("Didn't find test.*.value")
	}

	if act := val.Search([]string{"test", "*", "notmatched"}...); act != nil {
		t.Errorf("Expected nil result, received: %v", act)
	}

	if act, ok := val.Search([]string{"test", "*"}...).Data().([]interface{}); ok {
		if exp := []interface{}{map[string]interface{}{"value": 10}, map[string]interface{}{"value": int(20)}}; !reflect.DeepEqual(act, exp) {
			t.Errorf("Wrong result: %v != %v", act, exp)
		}
	} else {
		t.Errorf("Didn't find test.*.value")
	}
}

func TestArrayAppendWithSet(t *testing.T) {
	gObj := New()
	if _, err := gObj.Set([]interface{}{}, "foo"); err != nil {
		t.Fatal(err)
	}
	if _, err := gObj.Set(1, "foo", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err := gObj.Set([]interface{}{}, "foo", "-", "baz"); err != nil {
		t.Fatal(err)
	}
	if _, err := gObj.Set(2, "foo", "1", "baz", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err := gObj.Set(3, "foo", "1", "baz", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err := gObj.Set(4, "foo", "-"); err != nil {
		t.Fatal(err)
	}

	exp := `foo:
- 1
- baz:
  - 2
  - 3
- 4
`
	if act := gObj.String(); act != exp {
		t.Errorf("Unexpected value: %v != %v", act, exp)
	}
}

func TestSetAnnotations(t *testing.T) {
	sample := []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  annotations:
    a: b
`)
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	doc.Set("now", "metadata", "annotations", "confighub.com/resolved-at")
	exp := `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  annotations:
    a: b
    confighub.com/resolved-at: now
`
	if act := doc.String(); act != exp {
		t.Errorf("Unexpected value: %v != %v", act, exp)
	}
}

func TestSetNewAnnotations(t *testing.T) {
	sample := []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
`)
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	// doc.Set("now", "metadata", "annotations", "confighub.com/resolved-at")
	doc.Set("now", "metadata", "annotations", "confighub.com/resolved-at")
	exp := `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  annotations:
    confighub.com/resolved-at: now
`
	if act := doc.String(); act != exp {
		t.Errorf("Unexpected value: %v != %v", act, exp)
	}
}

func TestSetPNewAnnotations(t *testing.T) {
	sample := []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
`)
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	// doc.Set("now", "metadata", "annotations", "confighub.com/resolved-at")
	doc.SetP("now", `metadata.annotations."confighub.com/resolved-at"`)
	exp := `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  annotations:
    confighub.com/resolved-at: now
`
	if act := doc.String(); act != exp {
		t.Errorf("Unexpected value: %v != %v", act, exp)
	}
}

// TestSetIntoNullAnnotations verifies that descending into a null-scalar
// field (e.g. `annotations:` with no value) coerces it into a mapping
// instead of returning "unexpected node kind 8" (see issue #4504).
func TestSetIntoNullAnnotations(t *testing.T) {
	sample := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: demo
  annotations:
data:
  k: v
`)
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if _, err := doc.Set("my-slug", "metadata", "annotations", "confighub.com/UnitSlug"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	exp := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: demo
  annotations:
    confighub.com/UnitSlug: my-slug
data:
  k: v
`
	if act := doc.String(); act != exp {
		t.Errorf("Unexpected value:\nGot:\n%v\nWant:\n%v", act, exp)
	}
}

// TestSetPIntoNullAnnotations is the SetP equivalent of TestSetIntoNullAnnotations.
// This mirrors how EnsureConfigHubContextOnData invokes SetP in production.
func TestSetPIntoNullAnnotations(t *testing.T) {
	sample := []byte("metadata:\n  annotations:\n")
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if _, err := doc.SetP("my-slug", `metadata.annotations.confighub~1com/UnitSlug`); err != nil {
		t.Fatalf("SetP failed: %v", err)
	}
	exp := `metadata:
  annotations:
    confighub.com/UnitSlug: my-slug
`
	if act := doc.String(); act != exp {
		t.Errorf("Unexpected value:\nGot:\n%v\nWant:\n%v", act, exp)
	}
}

// TestSetExpandIntoNullSequence verifies that when the next path segment is
// an integer, a null intermediate is coerced into a sequence (not a mapping).
// Together with SetExpand this materializes a single-element list.
func TestSetExpandIntoNullSequence(t *testing.T) {
	sample := []byte(`spec:
  template:
    spec:
      containers:
`)
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if _, err := doc.SetExpand("nginx", "spec", "template", "spec", "containers", "0", "image"); err != nil {
		t.Fatalf("SetExpand failed: %v", err)
	}
	exp := `spec:
  template:
    spec:
      containers:
      - image: nginx
`
	if act := doc.String(); act != exp {
		t.Errorf("Unexpected value:\nGot:\n%v\nWant:\n%v", act, exp)
	}
}

// TestSetAppendIntoNullSequence verifies that "-" on a null intermediate
// coerces it into a sequence and appends, without needing SetExpand.
func TestSetAppendIntoNullSequence(t *testing.T) {
	sample := []byte(`spec:
  containers:
`)
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if _, err := doc.Set("nginx", "spec", "containers", "-", "image"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	exp := `spec:
  containers:
  - image: nginx
`
	if act := doc.String(); act != exp {
		t.Errorf("Unexpected value:\nGot:\n%v\nWant:\n%v", act, exp)
	}
}

// TestSetIntoNonNullScalarStillErrors verifies that descending into a
// non-null scalar field (e.g. `annotations: foo`) still errors, since
// that's a real type collision rather than an empty placeholder.
func TestSetIntoNonNullScalarStillErrors(t *testing.T) {
	sample := []byte("metadata:\n  annotations: foo\n")
	doc, err := ParseYAML(sample)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if _, err := doc.Set("my-slug", "metadata", "annotations", "confighub.com/UnitSlug"); err == nil {
		t.Errorf("expected error setting into non-null scalar, got nil")
	}
}

func TestExtractAndInjectCommentsToKeys(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "line comment on scalar value",
			input: `replicas: 3 # Line comment on replicas
paused: false
`,
		},
		{
			name: "line comment on sequence value",
			input: `ports: # Middle line comment
- containerPort: 4318
startupProbe:
  httpGet:
    path: /health
    port: 4318
`,
		},
		{
			name: "head comment",
			input: `# Head comment
field: value
`,
		},
		{
			name: "mixed comments",
			input: `# Head comment on replicas
replicas: 3 # Line comment on replicas
paused: false
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseYAML([]byte(tt.input))
			if err != nil {
				t.Fatalf("Failed to parse: %v", err)
			}

			doc.ExtractCommentsToKeys()
			extracted := doc.String()
			t.Logf("After ExtractCommentsToKeys:\n%s", extracted)

			doc2, err := ParseYAML([]byte(extracted))
			if err != nil {
				t.Fatalf("Failed to re-parse: %v", err)
			}
			doc2.InjectCommentsFromKeys()
			result := doc2.String()
			t.Logf("After InjectCommentsFromKeys:\n%s", result)

			if result != tt.input {
				t.Errorf("Round-trip failed:\n  want: %q\n  got:  %q", tt.input, result)
			}
		})
	}
}

func TestMultiDocCommentPreservation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "comment above leading separator",
			input: "# file header\n---\nfoo: bar\n",
		},
		{
			name:  "leading separator no comment",
			input: "---\nfoo: bar\n",
		},
		{
			name:  "no separator",
			input: "foo: bar\n",
		},
		{
			name:  "separator between docs",
			input: "foo: bar\n---\nbaz: qux\n",
		},
		{
			name:  "comment above and below separator",
			input: "# Before separator\n---\n# After separator\nfield: value\n",
		},
		{
			name:  "doc header survives extract/inject cycle",
			input: "# Document header\n---\n# Regular head comment\napiVersion: apps/v1\nkind: Deployment\n",
		},
		{
			name:  "doc header with multiple docs",
			input: "# Header\n---\nfoo: bar\n---\nbaz: qux\n",
		},
	}

	// Also test that doc headers survive an ExtractCommentsToKeys/InjectCommentsFromKeys cycle
	// (simulating NativeToYAML then YAMLToNative for YAML-native kits).
	extractInjectTests := []struct {
		name  string
		input string
	}{
		{
			name:  "doc header preserved through extract/inject",
			input: "# Document header\n---\n# Regular comment\napiVersion: apps/v1\nkind: Deployment\n",
		},
		{
			name:  "doc header with line comment through extract/inject",
			input: "# Header\n---\nreplicas: 3 # inline\npaused: false\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs, err := ParseAll([]byte(tt.input))
			if err != nil {
				t.Fatalf("Failed to parse: %v", err)
			}
			output := string(docs.Bytes())
			if output != tt.input {
				t.Errorf("Round-trip failed:\n  want: %q\n  got:  %q", tt.input, output)
			}

			docs2, err := ParseAll([]byte(output))
			if err != nil {
				t.Fatalf("Failed to re-parse: %v", err)
			}
			output2 := string(docs2.Bytes())
			if output2 != output {
				t.Errorf("Not stable:\n  first:  %q\n  second: %q", output, output2)
			}
		})
	}

	for _, tt := range extractInjectTests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse, extract comments to keys, inject back, check round-trip
			docs, err := ParseAll([]byte(tt.input))
			if err != nil {
				t.Fatalf("Failed to parse: %v", err)
			}
			for _, doc := range docs {
				doc.ExtractCommentsToKeys()
			}
			intermediate := string(docs.Bytes())
			t.Logf("After extract:\n%s", intermediate)

			docs2, err := ParseAll([]byte(intermediate))
			if err != nil {
				t.Fatalf("Failed to re-parse: %v", err)
			}
			for _, doc := range docs2 {
				doc.InjectCommentsFromKeys()
			}
			output := string(docs2.Bytes())
			t.Logf("After inject:\n%s", output)

			if output != tt.input {
				t.Errorf("Round-trip failed:\n  want: %q\n  got:  %q", tt.input, output)
			}
		})
	}
}
