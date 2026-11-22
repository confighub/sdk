// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Benchmarks of parsing, converting and rendering configuration data, the costs that dominate a
// function invocation on a large Unit.
package k8skit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/confighub/sdk/core/third_party/gaby"
	sigsyaml "sigs.k8s.io/yaml"
)

// The benchmarks read a Flux install -- 38 documents, 13 of them CRDs, 654 kB -- from the
// repository's test data, or the file BENCH_FILE names.
var benchFile = filepath.Join("..", "..", "..", "test-data", "datasets", "confighubops",
	"unit-data", "confighub-prod", "fluxcd.yaml")

func load(b *testing.B) []byte {
	b.Helper()
	path := benchFile
	if override := os.Getenv("BENCH_FILE"); override != "" {
		path = override
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Skipf("benchmark data not available: %v", err)
	}
	return data
}

func BenchmarkParseAll(b *testing.B) {
	data := load(b)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := gaby.ParseAll(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBytes(b *testing.B) {
	data := load(b)
	docs, _ := gaby.ParseAll(data)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = docs.Bytes()
	}
}

func BenchmarkNativeToYAML(b *testing.B) {
	data := load(b)
	p := &K8sResourceProviderType{}
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := p.NativeToYAML(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkYAMLToNative(b *testing.B) {
	data := load(b)
	p := &K8sResourceProviderType{}
	y, _ := p.NativeToYAML(data)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := p.YAMLToNative(y); err != nil {
			b.Fatal(err)
		}
	}
}

// The whole handler path for one function: convert, parse, serialize, convert back.
func BenchmarkFullRoundTrip(b *testing.B) {
	data := load(b)
	p := &K8sResourceProviderType{}
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		y, _ := p.NativeToYAML(data)
		docs, _ := gaby.ParseAll(y)
		out := docs.Bytes()
		if _, err := p.YAMLToNative(out); err != nil {
			b.Fatal(err)
		}
	}
}

// Copying parsed documents, which the handler does in place of parsing again.
func BenchmarkDeepCopy(b *testing.B) {
	data := load(b)
	docs, err := gaby.ParseAll(data)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = docs.DeepCopy()
	}
}

func jsonDocs(b *testing.B) [][]byte {
	data := load(b)
	p := &K8sResourceProviderType{}
	y, _ := p.NativeToYAML(data)
	docs, _ := gaby.ParseAll(y)
	var out [][]byte
	for _, d := range docs {
		j, err := d.MarshalJSON()
		if err != nil {
			b.Fatal(err)
		}
		out = append(out, j)
	}
	return out
}

func total(docs [][]byte) int64 {
	var n int64
	for _, d := range docs {
		n += int64(len(d))
	}
	return n
}

// Per-resource JSON into gaby (yaml.v3 node trees), the path a jsonb-primary store would take
// if functions kept their current node-tree representation.
func BenchmarkGabyParseJSON(b *testing.B) {
	docs := jsonDocs(b)
	b.SetBytes(total(docs))
	for b.Loop() {
		for _, d := range docs {
			if _, err := gaby.ParseJSON(d); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Per-resource JSON into Go maps with encoding/json, for comparison.
func BenchmarkStdJSONUnmarshal(b *testing.B) {
	docs := jsonDocs(b)
	b.SetBytes(total(docs))
	for b.Loop() {
		for _, d := range docs {
			var v map[string]any
			if err := json.Unmarshal(d, &v); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkStdJSONMarshal(b *testing.B) {
	docs := jsonDocs(b)
	var vs []map[string]any
	for _, d := range docs {
		var v map[string]any
		_ = json.Unmarshal(d, &v)
		vs = append(vs, v)
	}
	b.SetBytes(total(docs))
	for b.Loop() {
		for _, v := range vs {
			if _, err := json.Marshal(v); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Rendering YAML from JSON per resource: the cost of a read if only jsonb were stored.
func BenchmarkRenderYAMLFromJSON(b *testing.B) {
	docs := jsonDocs(b)
	b.SetBytes(total(docs))
	for b.Loop() {
		for _, d := range docs {
			if _, err := sigsyaml.JSONToYAML(d); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkGabyRenderFromJSON(b *testing.B) {
	docs := jsonDocs(b)
	var parsed []*gaby.YamlDoc
	for _, d := range docs {
		p, _ := gaby.ParseJSON(d)
		parsed = append(parsed, p)
	}
	b.SetBytes(total(docs))
	for b.Loop() {
		for _, p := range parsed {
			_ = p.Bytes()
		}
	}
}
