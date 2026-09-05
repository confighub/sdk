// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"sigs.k8s.io/yaml"
)

// A CustomResourceDefinition already carries most of a resource-type spec. Its structural schema
// says which arrays are keyed and by what, which sibling fields are mutually exclusive, which
// objects are freeform maps, and -- for CRDs written against a modern apiserver -- which fields
// cannot change after creation. Reading it is what makes registering a type something a user can
// prepare rather than something transcribed by hand from validation source, which is how the
// built-in types' immutable fields had to be collected.
//
// What a CRD does not say is which of its fields name *other* resources, what its apply order is,
// and which types it is interchangeable with. Those are the fields a human adds, and this leaves
// them out rather than guessing.
//
// See §8 of docs/design/resource-type-specs.md.

// CRDSpec is one resource type's generated spec: one per version the CRD declares, since a
// version is part of the resource type.
type CRDSpec struct {
	// Spec holds what the schema states outright. Everything in it was read, not inferred.
	Spec yamlkit.ResourceTypeSpec

	// SuggestedEmbeds are the shapes a structural match found -- an object with a containers
	// array whose items have name and image is a PodSpec. This is a heuristic and is reported
	// separately so a caller can present it as something to confirm: a wrong embed is a wrong
	// merge key, which is worse than none.
	SuggestedEmbeds []yamlkit.ShapeEmbed

	// Notes are what the reader saw and declined to decide. Each is a sentence for a human.
	Notes []string
}

// CRDSpecOptions controls the reading.
type CRDSpecOptions struct {
	// MatchPodSpec runs the structural PodSpec match. With it off, the arrays inside a pod
	// spec are declared explicitly instead, which is what a caller wants when the match is
	// wrong for their type.
	MatchPodSpec bool
}

// DefaultCRDSpecOptions returns the options the command line uses.
func DefaultCRDSpecOptions() CRDSpecOptions {
	return CRDSpecOptions{MatchPodSpec: true}
}

// SpecsFromCRD reads every CustomResourceDefinition in a YAML document and returns one CRDSpec
// per declared version. A document holding anything else is an error rather than an empty
// result, since a caller who pointed at the wrong file should hear about it.
func SpecsFromCRD(data []byte, options CRDSpecOptions) ([]CRDSpec, error) {
	documents, err := splitYAMLDocuments(data)
	if err != nil {
		return nil, err
	}

	var specs []CRDSpec
	crdsSeen := 0
	for _, document := range documents {
		var crd map[string]any
		if err := yaml.Unmarshal(document, &crd); err != nil {
			return nil, fmt.Errorf("parsing document: %w", err)
		}
		if len(crd) == 0 {
			continue
		}
		if stringAt(crd, "kind") != "CustomResourceDefinition" {
			continue
		}
		crdsSeen++
		versionSpecs, err := specsFromOneCRD(crd, options)
		if err != nil {
			return nil, err
		}
		specs = append(specs, versionSpecs...)
	}
	if crdsSeen == 0 {
		return nil, fmt.Errorf("no CustomResourceDefinition found")
	}
	return specs, nil
}

func specsFromOneCRD(crd map[string]any, options CRDSpecOptions) ([]CRDSpec, error) {
	spec, _ := crd["spec"].(map[string]any)
	group := stringAt(spec, "group")
	kind := stringAt(mapAt(spec, "names"), "kind")
	if group == "" || kind == "" {
		return nil, fmt.Errorf("CustomResourceDefinition %q declares no spec.group or spec.names.kind",
			stringAt(mapAt(crd, "metadata"), "name"))
	}

	// A CRD's own scope is the one fact about the type that needs no inference at all.
	scope := yamlkit.Scope(stringAt(spec, "scope"))

	versions, _ := spec["versions"].([]any)
	if len(versions) == 0 {
		return nil, fmt.Errorf("CustomResourceDefinition %s/%s declares no versions", group, kind)
	}

	specs := make([]CRDSpec, 0, len(versions))
	for _, entry := range versions {
		version, _ := entry.(map[string]any)
		name := stringAt(version, "name")
		if name == "" {
			continue
		}
		schema := mapAt(mapAt(version, "schema"), "openAPIV3Schema")
		resourceType := api.ResourceType(group + "/" + name + "/" + kind)
		if len(schema) == 0 {
			specs = append(specs, CRDSpec{
				Spec:  yamlkit.ResourceTypeSpec{Type: resourceType, Scope: scope},
				Notes: []string{"version " + name + " declares no schema, so only its scope could be read"},
			})
			continue
		}
		specs = append(specs, specFromSchema(resourceType, scope, schema, options))
	}
	return specs, nil
}

// reader accumulates what one version's schema states.
type reader struct {
	mergeKeys       []yamlkit.MergeKeyField
	exclusiveFields []yamlkit.ExclusiveFieldGroup
	mapKeyPaths     []string
	immutablePaths  []string
	podSpecPaths    []string
	notes           []string
	options         CRDSpecOptions
}

func specFromSchema(resourceType api.ResourceType, scope yamlkit.Scope, schema map[string]any,
	options CRDSpecOptions) CRDSpec {
	r := &reader{options: options}
	// metadata is registered universally and status is not authored configuration, so neither
	// is read. Everything else under the root is.
	for _, property := range sortedKeys(mapAt(schema, "properties")) {
		switch property {
		case "apiVersion", "kind", "metadata":
			continue
		case "status":
			r.note("status is not authored configuration and was not read")
			continue
		}
		r.walk(mapAt(mapAt(schema, "properties"), property), property)
	}

	generated := CRDSpec{
		Spec: yamlkit.ResourceTypeSpec{Type: resourceType, Scope: scope},
	}
	if scope == "" {
		r.note("the CRD declares no spec.scope, so the type states none")
	}

	embeds := r.shapeEmbeds()
	// A shape declares the structure beneath it, so declaring it again per type would be two
	// statements of one fact -- which is what specs exist to stop. Paths the suggested embed
	// covers are dropped from what is emitted explicitly.
	covered := func(path string) bool {
		for _, embed := range embeds {
			if embed.Path == "" || path == embed.Path || strings.HasPrefix(path, embed.Path+".") {
				return true
			}
		}
		return false
	}

	for _, mergeKey := range r.mergeKeys {
		if !covered(mergeKey.Path) {
			generated.Spec.MergeKeys = append(generated.Spec.MergeKeys, mergeKey)
		}
	}
	for _, group := range r.exclusiveFields {
		if !covered(group.Path) {
			generated.Spec.ExclusiveFields = append(generated.Spec.ExclusiveFields, group)
		}
	}
	for _, path := range r.mapKeyPaths {
		if !covered(strings.TrimSuffix(path, ".*")) {
			generated.Spec.MapKeyPaths = append(generated.Spec.MapKeyPaths, path)
		}
	}
	// Immutability is the CRD's own statement about its fields, not part of any shape, so it
	// survives the embed.
	if len(r.immutablePaths) > 0 {
		attributePaths := make([]yamlkit.AttributePath, 0, len(r.immutablePaths))
		for _, path := range r.immutablePaths {
			attributePaths = append(attributePaths, yamlkit.AttributePath{Path: path})
		}
		generated.Spec.Attributes = map[api.AttributeName][]yamlkit.AttributePath{
			AttributeNameImmutable: attributePaths,
		}
	}

	generated.SuggestedEmbeds = embeds
	generated.Notes = r.notes
	return generated
}

// shapeEmbeds turns the structural matches into embeds. A match at a path ending in ".spec" is a
// PodTemplateSpec's spec, so the shape to embed is PodTemplate one level up -- which is what puts
// the pod template's own metadata.labels where the pod-template-labels attribute expects it.
func (r *reader) shapeEmbeds() []yamlkit.ShapeEmbed {
	if !r.options.MatchPodSpec || len(r.podSpecPaths) == 0 {
		return nil
	}
	var embeds []yamlkit.ShapeEmbed
	for _, path := range r.podSpecPaths {
		if parent, found := strings.CutSuffix(path, ".spec"); found {
			embeds = append(embeds, yamlkit.ShapeEmbed{Shape: ShapePodTemplate, Path: parent})
			continue
		}
		embeds = append(embeds, yamlkit.ShapeEmbed{Shape: ShapePodSpec, Path: path})
	}
	return embeds
}

func (r *reader) note(note string) {
	r.notes = append(r.notes, note)
}

// walk reads one schema node and everything under it. path is where the node sits within the
// resource, in the dot syntax a spec uses, with "*" for an array element.
func (r *reader) walk(schema map[string]any, path string) {
	if len(schema) == 0 {
		return
	}
	r.readImmutability(schema, path)

	switch stringAt(schema, "type") {
	case "array":
		r.readMergeKeys(schema, path)
		r.walk(mapAt(schema, "items"), path+".*")
	case "object", "":
		r.readMapKeys(schema, path)
		r.readExclusiveFields(schema, path)
		if r.isPodSpec(schema) {
			r.podSpecPaths = append(r.podSpecPaths, path)
		}
		properties := mapAt(schema, "properties")
		for _, property := range sortedKeys(properties) {
			r.walk(mapAt(properties, property), path+"."+property)
		}
	}
}

// readMergeKeys reads the two spellings of "this array is keyed": the structural-schema one that
// a modern CRD carries, and the patch-strategy annotation that older generators emit.
func (r *reader) readMergeKeys(schema map[string]any, path string) {
	if stringAt(schema, "x-kubernetes-list-type") == "map" {
		if keys := stringsAt(schema, "x-kubernetes-list-map-keys"); len(keys) > 0 {
			// Nil rather than an empty slice for a single-key array, so a rendered spec says
			// nothing about extra keys instead of saying there are none.
			var extraKeys []string
			if len(keys) > 1 {
				extraKeys = keys[1:]
			}
			r.mergeKeys = append(r.mergeKeys, yamlkit.MergeKeyField{
				Path: path, Key: keys[0], ExtraKeys: extraKeys,
			})
			return
		}
		r.note(path + " is declared a list-type map with no x-kubernetes-list-map-keys, so it has no key")
	}
	if key := stringAt(schema, "x-kubernetes-patch-merge-key"); key != "" {
		r.mergeKeys = append(r.mergeKeys, yamlkit.MergeKeyField{Path: path, Key: key})
	}
}

// readMapKeys reads the two ways a CRD says "the children of this object are keys, not fields":
// an explicit additionalProperties schema, and preserve-unknown-fields on an object with no
// properties of its own. An object with both properties and unknown fields is not a map -- its
// children are a mix -- and is left alone.
func (r *reader) readMapKeys(schema map[string]any, path string) {
	if path == "" {
		return
	}
	if additional, present := schema["additionalProperties"]; present {
		if enabled, isBool := additional.(bool); !isBool || enabled {
			r.mapKeyPaths = append(r.mapKeyPaths, path+".*")
			return
		}
	}
	if preserve, _ := schema["x-kubernetes-preserve-unknown-fields"].(bool); preserve && len(mapAt(schema, "properties")) == 0 {
		r.mapKeyPaths = append(r.mapKeyPaths, path+".*")
	}
}

// readExclusiveFields reads a oneOf over sibling properties, which is how a CRD spells the union
// that Kubernetes' built-in types spell as retainKeys. Only the unambiguous form is taken: two or
// more branches, each naming exactly one property.
func (r *reader) readExclusiveFields(schema map[string]any, path string) {
	branches, _ := schema["oneOf"].([]any)
	if len(branches) < 2 {
		return
	}
	var members []string
	for _, entry := range branches {
		branch, _ := entry.(map[string]any)
		named := map[string]bool{}
		for _, required := range stringsAt(branch, "required") {
			named[required] = true
		}
		for _, property := range sortedKeys(mapAt(branch, "properties")) {
			named[property] = true
		}
		if len(named) != 1 {
			r.note(path + " has a oneOf this could not read as a set of mutually exclusive fields")
			return
		}
		for member := range named {
			members = append(members, member)
		}
	}
	sort.Strings(members)
	r.exclusiveFields = append(r.exclusiveFields, yamlkit.ExclusiveFieldGroup{Path: path, Members: members})
}

// readImmutability reads x-kubernetes-validations. A plain self == oldSelf is immutability stated
// outright. Anything else mentioning oldSelf is a conditional -- "immutable once set" is the
// common one -- and is reported rather than taken, because a field wrongly called immutable makes
// vet-immutable fail a change that is legal.
func (r *reader) readImmutability(schema map[string]any, path string) {
	validations, _ := schema["x-kubernetes-validations"].([]any)
	for _, entry := range validations {
		validation, _ := entry.(map[string]any)
		rule := strings.Join(strings.Fields(stringAt(validation, "rule")), " ")
		switch {
		case rule == "self == oldSelf":
			r.immutablePaths = append(r.immutablePaths, path)
		case strings.Contains(rule, "oldSelf"):
			r.note(path + " has the validation " + strconv.Quote(rule) +
				", which constrains changes without forbidding them; add it as immutable by hand if it means that")
		}
	}
}

// isPodSpec is the structural match: an object carrying a containers array whose elements have a
// name and an image is a pod spec, whatever the CRD calls it.
func (r *reader) isPodSpec(schema map[string]any) bool {
	containers := mapAt(mapAt(schema, "properties"), "containers")
	if stringAt(containers, "type") != "array" {
		return false
	}
	element := mapAt(mapAt(containers, "items"), "properties")
	if len(element) == 0 {
		return false
	}
	_, hasName := element["name"]
	_, hasImage := element["image"]
	return hasName && hasImage
}

// splitYAMLDocuments splits a multi-document YAML stream. A CRD bundle is the usual input and is
// commonly several documents in one file.
func splitYAMLDocuments(data []byte) ([][]byte, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("input is empty")
	}
	var documents [][]byte
	for _, document := range strings.Split("\n"+string(data), "\n---") {
		if strings.TrimSpace(document) != "" {
			documents = append(documents, []byte(document))
		}
	}
	return documents, nil
}

func mapAt(parent map[string]any, key string) map[string]any {
	child, _ := parent[key].(map[string]any)
	return child
}

func stringAt(parent map[string]any, key string) string {
	value, _ := parent[key].(string)
	return value
}

func stringsAt(parent map[string]any, key string) []string {
	entries, _ := parent[key].([]any)
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		if value, isString := entry.(string); isString {
			values = append(values, value)
		}
	}
	return values
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
