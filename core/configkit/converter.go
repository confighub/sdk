// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package configkit

import (
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

type ConfigConverter interface {
	NativeToYAML(data []byte) ([]byte, error)
	YAMLToNative(yamlData []byte) ([]byte, error)
	DataType() api.DataType
}

// TreeConverter is implemented by converters whose native form is YAML, so that converting is a
// change to the parsed documents -- moving comments between $comment$ keys and YAML's comment fields
// -- rather than a different syntax. TreeToNative renders parsed documents in the native form
// directly, where YAMLToNative serializes them, parses them again and then converts.
//
// There is deliberately no method for the other direction yet. Parsing native data and moving its
// comments in place yields documents that render the same as converting the text and parsing the
// result, but not the same documents: serializing restyles some scalars (a plain nginx:2 in a flow
// mapping comes back single-quoted, non-ASCII text double-quoted, and comment values take the style
// the encoder chooses), and mutation records and getters render values from those styles.
type TreeConverter interface {
	// TreeToNative renders documents in the native form. It may change the documents it is given.
	// For documents parsed from YAML this converter produced, it returns exactly what YAMLToNative
	// returns for their serialization.
	TreeToNative(docs gaby.Container) ([]byte, error)
}

// RenderNative renders documents in the native form. It may change the documents it is given, so
// a caller that keeps using them passes a copy.
func RenderNative(converter ConfigConverter, docs gaby.Container) ([]byte, error) {
	if treeConverter, ok := converter.(TreeConverter); ok {
		return treeConverter.TreeToNative(docs)
	}
	return converter.YAMLToNative(docs.Bytes())
}

// YAMLTreeToNative is TreeToNative for a toolchain whose native form is YAML: it turns $comment$
// keys back into YAML comments, in place, and renders the documents.
func YAMLTreeToNative(docs gaby.Container) ([]byte, error) {
	for _, doc := range docs {
		if err := doc.InjectCommentsFromKeys(); err != nil {
			return nil, err
		}
	}
	return docs.Bytes(), nil
}

// YAMLNativeToYAML is NativeToYAML for a toolchain whose native form is YAML: it turns comments into
// $comment$ keys, so that comments read the same in every format. Data that does not parse is
// returned as it is, for the parse that follows to report.
func YAMLNativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	docs, err := gaby.ParseAll(data)
	if err != nil {
		return data, nil //nolint:nilerr // the parse that follows reports it, as callers expect
	}
	// Data that is only comments parses to no documents, which would render as nothing.
	if len(docs) == 0 {
		return data, nil
	}
	for _, doc := range docs {
		if err := doc.ExtractCommentsToKeys(); err != nil {
			return nil, err
		}
	}
	return docs.Bytes(), nil
}

// YAMLYAMLToNative is YAMLToNative for a toolchain whose native form is YAML. Data that does not
// parse is returned as it is.
func YAMLYAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return yamlData, nil
	}
	docs, err := gaby.ParseAll(yamlData)
	if err != nil {
		return yamlData, nil //nolint:nilerr // returned unchanged, as callers expect
	}
	// Data that is only comments parses to no documents, which would render as nothing.
	if len(docs) == 0 {
		return yamlData, nil
	}
	return YAMLTreeToNative(docs)
}
