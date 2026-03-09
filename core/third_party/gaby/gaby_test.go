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
