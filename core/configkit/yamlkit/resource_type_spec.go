// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"sigs.k8s.io/yaml"
)

// A resource-type spec is one declaration per resource type, replacing the per-concern
// tables that each name a subset of the types. See docs/design/resource-type-specs.md.
//
// The declaration is toolchain-neutral: it lives here, beside the path registry it compiles
// into, so a toolchain that has a keyed list to declare can carry a spec without a new
// lookup table. What is specific to one toolchain hangs off the spec in that toolchain's own
// type.
//
// Specs are data, not Go. Every type here round-trips through YAML or JSON, and a spec set is
// the document a file holds today and a ConfigHub Unit is meant to hold later -- which is why
// a shape is embedded by name rather than by pointer, and why a set carries its shapes with
// it rather than referring to Go values that only exist in one process.

// MergeKeyField describes a strategic merge patch key for an array field.
// Path is the dot-separated path to the array field (gaby dot syntax), relative to whatever
// root the declaration sits at. Wildcards (*) represent any array index within the path.
// Key is the field name within array items used as the merge key.
//
// ExtraKeys names the remaining fields of a composite key, for the arrays Kubernetes
// declares with more than one x-kubernetes-list-map-key: a container port is identified by
// its number and its protocol, and a topology spread constraint by its topology key and
// its whenUnsatisfiable. Two elements that agree on Key alone but differ in an ExtraKey
// are different elements, and matching them would merge one into the other.
type MergeKeyField struct {
	Path      string   `json:"path"`
	Key       string   `json:"key"`
	ExtraKeys []string `json:"extraKeys,omitempty"`
}

// Keys returns the full key list, Key first.
func (f MergeKeyField) Keys() []string {
	if len(f.ExtraKeys) == 0 {
		return []string{f.Key}
	}
	return append([]string{f.Key}, f.ExtraKeys...)
}

// ExclusiveFieldGroup declares a set of sibling fields of which at most one may be present,
// at a path relative to whatever root the declaration sits at.
//
// Path is the dot-separated path to the object holding them, with "*" for any array index.
// Members are the mutually exclusive fields. Discriminator names the sibling that says which
// member is valid, where the schema has one, and AllowedMember maps each of its values to
// the member it permits -- a value with no entry permits none.
//
// This is Kubernetes' patchStrategy:"retainKeys" expressed as data. The API server rejects a
// resource with two members set, so a merge that adds one member and cannot remove the other
// produces configuration that will not apply.
type ExclusiveFieldGroup struct {
	Path          string            `json:"path"`
	Members       []string          `json:"members"`
	Discriminator string            `json:"discriminator,omitempty"`
	AllowedMember map[string]string `json:"allowedMember,omitempty"`
}

// AttributePath is one location of an attribute within a resource type or shape, relative to
// whatever root the declaration sits at. Everything else about the attribute -- its data type,
// getter, setter, embedded accessor -- comes from the descriptor registered once per attribute
// name, because none of it varies by resource type. What varies is only where the attribute
// lives, which is what a spec says.
//
// DataType overrides the descriptor's for this path alone. An attribute may hold paths of more
// than one data type; the descriptor's is the default, not a constraint the paths must match.
type AttributePath struct {
	Path     string       `json:"path"`
	DataType api.DataType `json:"dataType,omitempty"`

	// Default is the value a defaulting function writes here, for attributes that carry one.
	// It is per path rather than per attribute because the whole point of a defaulting
	// attribute is that each of its paths gets a different value; the descriptor cannot hold
	// it. A path with a Default registers a visitor setter carrying that value.
	Default any `json:"default,omitempty"`

	// Needed and Provided override the descriptor's flags for this path alone, for an
	// attribute whose paths do not all play the same role. configmap-hash is the case: a
	// ConfigMap's own annotation offers the hash and a pod template's wants it, under one
	// attribute name, and a descriptor carries one pair of flags for every path under it.
	Needed   bool `json:"needed,omitempty"`
	Provided bool `json:"provided,omitempty"`

	// Target is the resource type this path names, for a path that refers to another
	// resource: a Rollout's spec.strategy.canary.stableService names a v1/Service. It
	// compiles to the ResourceType property that needs and provides matching selects on,
	// and fills the target parameter of the attribute's getter and setter.
	//
	// Like Default it is per path and not per attribute, for the same reason: a type's
	// reference paths point at different types, so the descriptor cannot hold it.
	Target api.ResourceType `json:"target,omitempty"`
}

// ShapeEmbed places a shape at a path within a type or another shape. Shape is the shape's
// name in the set's Shapes; Path is relative to the embedding declaration's root, and may be
// empty to embed at that root.
type ShapeEmbed struct {
	Shape string `json:"shape"`
	Path  string `json:"path,omitempty"`
}

// Declaration is the body a shape and a resource-type spec share: structure facts with every
// path stated relative to the root the declaration sits at. A spec's root is the resource; a
// shape's root is wherever it is embedded.
type Declaration struct {
	// Embeds places shapes within this declaration.
	Embeds []ShapeEmbed `json:"embeds,omitempty"`

	// What the merge engine reads, before any path is normalized. These are inputs to the
	// path registry's key function rather than entries in it, which is why they are not
	// attributes.
	MergeKeys       []MergeKeyField       `json:"mergeKeys,omitempty"`
	ExclusiveFields []ExclusiveFieldGroup `json:"exclusiveFields,omitempty"`

	// MapKeyPaths name the freeform maps whose children are dynamic keys rather than schema
	// fields -- a labels map, an annotations map. Each must end in ".*", because the question
	// normalizePath asks is always "is the thing to my left a map, so my next segment is a
	// key?", and it asks it with a path ending in the wildcard. A path without that suffix can
	// never match one and is silently dead; CompileSpecSets rejects it rather than accepting a
	// declaration that does nothing.
	MapKeyPaths []string `json:"mapKeyPaths,omitempty"`

	// Attributes: what the functions read, keyed by attribute name. This is the path registry
	// transposed -- indexed by resource type rather than by concern -- so registering a type
	// is one edit rather than one edit per attribute the type has.
	Attributes map[api.AttributeName][]AttributePath `json:"attributes,omitempty"`
}

// Scope says what a resource type's names are scoped by. Every toolchain already has the
// concept -- ResourceProvider carries RemoveScopeFromResourceName and ScopelessResourceNamePath
// -- but not the same values, so the values are the toolchain's to define. Kubernetes has two,
// below; another toolchain may have others, or more than two.
type Scope string

const (
	// ScopeNamespaced and ScopeCluster are Kubernetes' scopes: a namespaced type's names are
	// scoped by a namespace, a cluster-scoped type's are not.
	ScopeNamespaced Scope = "Namespaced"
	ScopeCluster    Scope = "Cluster"
)

// ResourceTypeSpec is what any toolchain declares about one resource type. ToolchainType may
// be left empty, in which case the containing SpecSet's applies.
//
// The three fields below the declaration are carried rather than interpreted: the compiler
// stores them and hands them back, and what they mean is the toolchain's business. They are here
// rather than on a per-toolchain type because the concepts generalize -- ResourceTypesAreSimilar
// and the scope of a resource name are already on the ResourceProvider interface -- and because
// one spec file, one loader and one stored representation is what §6.1 and §9.3 need. A spec
// registering a CRD has to be able to say the CRD's scope, and it is written in the same file as
// everything else.
type ResourceTypeSpec struct {
	Type          api.ResourceType        `json:"type"`
	ToolchainType workerapi.ToolchainType `json:"toolchainType,omitempty"`
	Declaration

	// Scope is what this type's names are scoped by, in the toolchain's own terms.
	Scope Scope `json:"scope,omitempty"`

	// SimilarityClass names a set of types that are interchangeable enough that a mutation to
	// one can be replayed against another -- Kubernetes workload controllers, which carry a pod
	// spec in the same place, are the case it exists for. Two types are similar when they
	// declare the same class. It is a free string so that a toolchain names its own classes.
	SimilarityClass string `json:"similarityClass,omitempty"`

	// ApplyPriority orders this type against others when a set of resources is applied
	// together; lower goes first. A pointer, because zero is a usable priority and "unset" has
	// to be distinguishable from it.
	ApplyPriority *int `json:"applyPriority,omitempty"`

	// Schema is where this type's schema is, for a type the set's SchemaLocations do not
	// address. Two values are special: empty means "try the set's locations", and "none"
	// means this type has no schema to find. The difference matters to a validator, which
	// otherwise cannot tell a type it skipped from a type that passed -- the distinction
	// vet-schemas passes silently over today.
	Schema string `json:"schema,omitempty"`
}

// SchemaNone is ResourceTypeSpec.Schema for a type known to have no schema anywhere. Stating
// it stops a validator reporting the type as validated when it was skipped, and stops a
// fetch that can only ever fail.
const SchemaNone = "none"

// SpecSet is a set of resource-type specs and the shapes they embed: one file, and the unit a
// stored spec Unit would hold. ToolchainType is the default for specs that do not state one,
// so a single-toolchain file states it once.
type SpecSet struct {
	ToolchainType workerapi.ToolchainType `json:"toolchainType,omitempty"`
	Shapes        map[string]Declaration  `json:"shapes,omitempty"`
	ResourceTypes []ResourceTypeSpec      `json:"resourceTypes,omitempty"`

	// SchemaLocations are where a type's schema is fetched from, most authoritative first.
	// Each is a template over the resource type, in the syntax kubeconform reads, so one
	// entry covers every type rather than each type naming its own URL -- which would be the
	// per-type table this file exists to remove.
	//
	// They are per set rather than per type because that is the granularity at which they
	// vary: a catalog covers a whole family of types, and adding a catalog is one line.
	// ResourceTypeSpec.Schema is for the type whose schema is not where the templates say.
	SchemaLocations []string `json:"schemaLocations,omitempty"`
}

// LoadSpecSet parses a spec set from YAML or JSON.
func LoadSpecSet(data []byte) (SpecSet, error) {
	var set SpecSet
	if err := yaml.UnmarshalStrict(data, &set); err != nil {
		return SpecSet{}, fmt.Errorf("parsing resource type spec set: %w", err)
	}
	return set, nil
}

// MarshalSpecSet renders a spec set as YAML, the form LoadSpecSet reads.
func MarshalSpecSet(set SpecSet) ([]byte, error) {
	data, err := yaml.Marshal(set)
	if err != nil {
		return nil, fmt.Errorf("rendering resource type spec set: %w", err)
	}
	return data, nil
}

// specKey identifies a spec. ResourceType is not unique across toolchains -- it means
// group/version/Kind in k8skit, the ConfigHub entity type in cubkit, and the declared config
// schema in appyamlkit -- so the toolchain is part of the key rather than a label on it.
type specKey struct {
	toolchainType workerapi.ToolchainType
	resourceType  api.ResourceType
}

// CompiledSpecs is the immutable result of compiling spec sets: the structure lookups the
// merge engine reads, with every embed expanded and every relative path joined to where it
// sits.
//
// It is built during construction and read-only afterwards. Reads are on the merge hot path
// and are two map lookups, as they were when the lookups were package globals.
type CompiledSpecs struct {
	mergeKeys       map[specKey]map[string][]string
	exclusiveFields map[specKey]map[string]ExclusiveFields
	mapKeyPaths     map[specKey]map[string]bool
	attributes      map[specKey]map[api.AttributeName][]AttributePath

	// shapeEmbeds records where each shape ended up, per type, in declaration order. The
	// expansion above turns an embed into paths and forgets it came from a shape; a function
	// that wants "where is this type's PodSpec" needs the question answered rather than a table
	// restating the answer.
	shapeEmbeds map[specKey]map[string][]string

	// facts carries what a toolchain declares about a type without the compiler interpreting
	// it: scope, similarity class, apply priority, and where its schema is.
	facts map[specKey]typeFacts

	// schemaLocations is per toolchain rather than per type, because that is the granularity
	// at which a schema catalog varies. Sets contributing to one toolchain are concatenated in
	// the order they were compiled, so the first set's locations are tried first.
	schemaLocations map[workerapi.ToolchainType][]string
}

// typeFacts is the carried, uninterpreted part of a resource type spec.
type typeFacts struct {
	scope           Scope
	similarityClass string
	applyPriority   *int
	schema          string
}

// CompileSpecSets compiles spec sets into one immutable snapshot. Shapes are resolved across
// all the sets together, so a set registering a new type can embed a shape another set
// declares -- a CRD carrying an ordinary PodSpec is the case that needs it. A shape name
// declared twice is an error rather than a silent win for whichever set was passed last.
func CompileSpecSets(sets ...SpecSet) (*CompiledSpecs, error) {
	shapes := make(map[string]Declaration)
	for _, set := range sets {
		for name, shape := range set.Shapes {
			if _, duplicate := shapes[name]; duplicate {
				return nil, fmt.Errorf("shape %q declared in more than one spec set", name)
			}
			shapes[name] = shape
		}
	}

	compiled := &CompiledSpecs{
		mergeKeys:       make(map[specKey]map[string][]string),
		exclusiveFields: make(map[specKey]map[string]ExclusiveFields),
		mapKeyPaths:     make(map[specKey]map[string]bool),
		attributes:      make(map[specKey]map[api.AttributeName][]AttributePath),
		shapeEmbeds:     make(map[specKey]map[string][]string),
		facts:           make(map[specKey]typeFacts),
		schemaLocations: make(map[workerapi.ToolchainType][]string),
	}
	for _, set := range sets {
		if len(set.SchemaLocations) > 0 {
			compiled.schemaLocations[set.ToolchainType] = append(
				compiled.schemaLocations[set.ToolchainType], set.SchemaLocations...)
		}
		for _, spec := range set.ResourceTypes {
			toolchainType := spec.ToolchainType
			if toolchainType == "" {
				toolchainType = set.ToolchainType
			}
			if toolchainType == "" {
				return nil, fmt.Errorf("resource type %s: no toolchain type, on the spec or its set", spec.Type)
			}
			key := specKey{toolchainType: toolchainType, resourceType: spec.Type}
			if err := compiled.addDeclaration(key, "", spec.Declaration, shapes, nil); err != nil {
				return nil, fmt.Errorf("resource type %s/%s: %w", toolchainType, spec.Type, err)
			}
			facts := compiled.facts[key]
			if spec.Scope != "" {
				facts.scope = spec.Scope
			}
			if spec.SimilarityClass != "" {
				facts.similarityClass = spec.SimilarityClass
			}
			if spec.ApplyPriority != nil {
				facts.applyPriority = spec.ApplyPriority
			}
			if spec.Schema != "" {
				facts.schema = spec.Schema
			}
			compiled.facts[key] = facts
		}
	}
	return compiled, nil
}

// addDeclaration joins every path in the declaration to prefix and records it, then recurses
// into the shapes it embeds. embedding names the shapes on the current expansion path, so a
// shape reached twice by different routes is fine and a shape that reaches itself is not.
func (c *CompiledSpecs) addDeclaration(
	key specKey,
	prefix string,
	declaration Declaration,
	shapes map[string]Declaration,
	embedding []string,
) error {
	for _, field := range declaration.MergeKeys {
		if c.mergeKeys[key] == nil {
			c.mergeKeys[key] = make(map[string][]string)
		}
		c.mergeKeys[key][JoinRelativePath(prefix, field.Path)] = field.Keys()
	}

	for _, group := range declaration.ExclusiveFields {
		if c.exclusiveFields[key] == nil {
			c.exclusiveFields[key] = make(map[string]ExclusiveFields)
		}
		c.exclusiveFields[key][JoinRelativePath(prefix, group.Path)] = ExclusiveFields{
			Members:       group.Members,
			Discriminator: group.Discriminator,
			AllowedMember: group.AllowedMember,
		}
	}

	for _, path := range declaration.MapKeyPaths {
		joined := JoinRelativePath(prefix, path)
		if !strings.HasSuffix(joined, ".*") {
			return fmt.Errorf("map-key path %q does not end in \".*\"; it can never match, "+
				"because a map-key path names the map whose children are dynamic keys", joined)
		}
		if c.mapKeyPaths[key] == nil {
			c.mapKeyPaths[key] = make(map[string]bool)
		}
		c.mapKeyPaths[key][joined] = true
	}

	for attributeName, paths := range declaration.Attributes {
		if c.attributes[key] == nil {
			c.attributes[key] = make(map[api.AttributeName][]AttributePath)
		}
		for _, attributePath := range paths {
			attributePath.Path = JoinRelativePath(prefix, attributePath.Path)
			c.attributes[key][attributeName] = append(c.attributes[key][attributeName], attributePath)
		}
	}

	for _, embed := range declaration.Embeds {
		shape, declared := shapes[embed.Shape]
		if !declared {
			return fmt.Errorf("embeds undeclared shape %q at %q", embed.Shape, JoinRelativePath(prefix, embed.Path))
		}
		for _, ancestor := range embedding {
			if ancestor == embed.Shape {
				return fmt.Errorf("shape %q embeds itself: %s", embed.Shape,
					strings.Join(append(append([]string{}, embedding...), embed.Shape), " -> "))
			}
		}
		embeddedAt := JoinRelativePath(prefix, embed.Path)
		if c.shapeEmbeds[key] == nil {
			c.shapeEmbeds[key] = make(map[string][]string)
		}
		c.shapeEmbeds[key][embed.Shape] = append(c.shapeEmbeds[key][embed.Shape], embeddedAt)

		err := c.addDeclaration(
			key,
			embeddedAt,
			shape,
			shapes,
			append(embedding, embed.Shape),
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// JoinRelativePath joins a relative path to the prefix it sits under. Either may be empty.
func JoinRelativePath(prefix, relative string) string {
	switch {
	case prefix == "":
		return relative
	case relative == "":
		return prefix
	default:
		return prefix + "." + relative
	}
}

// numericSegmentRegexp matches one or more digits (a numeric array index).
var numericSegmentRegexp = regexp.MustCompile(`^[0-9]+$`)

// NormalizeStructurePath replaces numeric array indices and associative segments in a
// dot-separated path with "*", which is the form the structure lookups are keyed by.
//
// This is deliberately narrower than normalizePath: the structure lookups are what decide
// whether a segment is a dynamic map key in the first place, so consulting them here would
// make their key function depend on their contents.
func NormalizeStructurePath(path string) string {
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		if numericSegmentRegexp.MatchString(segment) || strings.HasPrefix(segment, "?") {
			segments[i] = "*"
		}
	}
	return strings.Join(segments, ".")
}

// MergeKeysForPath returns the merge key field names for the given resource type and array
// path, checking the type's own entries before those declared for api.ResourceTypeAny.
func (c *CompiledSpecs) MergeKeysForPath(
	toolchainType workerapi.ToolchainType,
	resourceType api.ResourceType,
	path string,
) ([]string, bool) {
	normalized := NormalizeStructurePath(path)
	for _, rt := range []api.ResourceType{resourceType, api.ResourceTypeAny} {
		if keys, ok := c.mergeKeys[specKey{toolchainType, rt}][normalized]; ok {
			return keys, true
		}
	}
	return nil, false
}

// ExclusiveFieldsForPath returns the mutually exclusive sibling fields of the object at the
// given path, checking the type's own entries before those declared for api.ResourceTypeAny.
//
// The table this replaced had no wildcard fallback, unlike the merge-key and map-key tables
// beside it. Nothing declares a wildcard union today, so adding it changes no answer; it means
// the first one declared works rather than being silently ignored for every type.
func (c *CompiledSpecs) ExclusiveFieldsForPath(
	toolchainType workerapi.ToolchainType,
	resourceType api.ResourceType,
	path string,
) (ExclusiveFields, bool) {
	normalized := NormalizeStructurePath(path)
	for _, rt := range []api.ResourceType{resourceType, api.ResourceTypeAny} {
		if fields, ok := c.exclusiveFields[specKey{toolchainType, rt}][normalized]; ok {
			return fields, true
		}
	}
	return ExclusiveFields{}, false
}

// IsMapKeyPath reports whether the path is a freeform map whose children are dynamic keys
// (label keys, annotation keys) rather than schema-defined fields. Child segments of such a
// path are wildcarded during normalization. The path should end with ".*" to ask about a
// map's children.
func (c *CompiledSpecs) IsMapKeyPath(
	toolchainType workerapi.ToolchainType,
	resourceType api.ResourceType,
	path string,
) bool {
	normalized := NormalizeStructurePath(path)
	for _, rt := range []api.ResourceType{resourceType, api.ResourceTypeAny} {
		if c.mapKeyPaths[specKey{toolchainType, rt}][normalized] {
			return true
		}
	}
	return false
}

// ScopeOf returns the scope a type declares, or "" if it declares none.
func (c *CompiledSpecs) ScopeOf(toolchainType workerapi.ToolchainType, resourceType api.ResourceType) Scope {
	return c.facts[specKey{toolchainType, resourceType}].scope
}

// SimilarityClassOf returns the similarity class a type declares, or "" if it declares none.
// Two types are similar when both declare the same non-empty class.
func (c *CompiledSpecs) SimilarityClassOf(toolchainType workerapi.ToolchainType, resourceType api.ResourceType) string {
	return c.facts[specKey{toolchainType, resourceType}].similarityClass
}

// ApplyPriorityOf returns the apply priority a type declares, and whether it declares one.
func (c *CompiledSpecs) ApplyPriorityOf(toolchainType workerapi.ToolchainType, resourceType api.ResourceType) (int, bool) {
	priority := c.facts[specKey{toolchainType, resourceType}].applyPriority
	if priority == nil {
		return 0, false
	}
	return *priority, true
}

// SchemaLocationsFor returns where a toolchain's schemas are fetched from, most authoritative
// first, as templates over the resource type. Empty for a toolchain that declares none, which
// is every toolchain whose formats have no schema to fetch.
func (c *CompiledSpecs) SchemaLocationsFor(toolchainType workerapi.ToolchainType) []string {
	return slices.Clone(c.schemaLocations[toolchainType])
}

// SchemaFor returns what a type declares about where its own schema is: a location that
// overrides the toolchain's templates, SchemaNone for a type known to have none, or "" for the
// ordinary case of "look where the templates say".
func (c *CompiledSpecs) SchemaFor(toolchainType workerapi.ToolchainType, resourceType api.ResourceType) string {
	return c.facts[specKey{toolchainType, resourceType}].schema
}

// ResourceTypesWithScope returns every type declaring the given scope, sorted.
func (c *CompiledSpecs) ResourceTypesWithScope(toolchainType workerapi.ToolchainType, scope Scope) []api.ResourceType {
	var types []api.ResourceType
	for key, facts := range c.facts {
		if key.toolchainType == toolchainType && facts.scope == scope {
			types = append(types, key.resourceType)
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

// DeclaredAttributes returns the attribute paths declared for one resource type of one
// toolchain, keyed by attribute name, with every embed expanded and every relative path joined
// to where it sits. The returned slices are the compiled snapshot's own and must not be
// modified.
func (c *CompiledSpecs) DeclaredAttributes(
	toolchainType workerapi.ToolchainType,
	resourceType api.ResourceType,
) map[api.AttributeName][]AttributePath {
	return c.attributes[specKey{toolchainType, resourceType}]
}

// ResourceTypesWithAttributes returns every resource type declaring at least one attribute for
// the toolchain, sorted, so registration is deterministic.
func (c *CompiledSpecs) ResourceTypesWithAttributes(toolchainType workerapi.ToolchainType) []api.ResourceType {
	return resourceTypesFor(toolchainType, c.attributes)
}

// DeclaredReference is one path that names another resource: which type declares it, where the
// path is, what attribute it is a path of, and which type it points at.
type DeclaredReference struct {
	ResourceType  api.ResourceType
	AttributeName api.AttributeName
	Path          string
	Target        api.ResourceType
}

// ReferencePaths returns every declared attribute path carrying a Target, sorted by resource
// type, then attribute name, then path, then target. A path that several targets are declared
// for -- a field naming any of three workload controllers -- appears once per target, which is
// how the requirement is registered and how it is unioned.
//
// Sorted because a registry assembled by ranging over maps differs run to run, and it is
// served that way on a function server's /paths route.
func (c *CompiledSpecs) ReferencePaths(toolchainType workerapi.ToolchainType) []DeclaredReference {
	var references []DeclaredReference
	for _, resourceType := range c.ResourceTypesWithAttributes(toolchainType) {
		byAttribute := c.DeclaredAttributes(toolchainType, resourceType)
		for _, attributeName := range sortedAttributeNames(byAttribute) {
			for _, declared := range byAttribute[attributeName] {
				if declared.Target == "" {
					continue
				}
				references = append(references, DeclaredReference{
					ResourceType:  resourceType,
					AttributeName: attributeName,
					Path:          declared.Path,
					Target:        declared.Target,
				})
			}
		}
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].ResourceType != references[j].ResourceType {
			return references[i].ResourceType < references[j].ResourceType
		}
		if references[i].AttributeName != references[j].AttributeName {
			return references[i].AttributeName < references[j].AttributeName
		}
		if references[i].Path != references[j].Path {
			return references[i].Path < references[j].Path
		}
		return references[i].Target < references[j].Target
	})
	return references
}

// ShapePaths returns where a shape sits within a resource type, in declaration order, or nil if
// the type does not embed it. A type embedding a shape more than once -- three container arrays
// under one PodSpec -- gets one entry per embed.
func (c *CompiledSpecs) ShapePaths(
	toolchainType workerapi.ToolchainType,
	resourceType api.ResourceType,
	shapeName string,
) []string {
	return c.shapeEmbeds[specKey{toolchainType, resourceType}][shapeName]
}

// ResourceTypesEmbeddingShape returns every resource type embedding the named shape, sorted.
func (c *CompiledSpecs) ResourceTypesEmbeddingShape(
	toolchainType workerapi.ToolchainType,
	shapeName string,
) []api.ResourceType {
	types := make([]api.ResourceType, 0, len(c.shapeEmbeds))
	for key, byShape := range c.shapeEmbeds {
		if key.toolchainType == toolchainType && len(byShape[shapeName]) > 0 {
			types = append(types, key.resourceType)
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

// RenderStructure writes every structure entry compiled for one toolchain in a stable order,
// as tab-separated lines. It is the enumeration the compiled snapshot otherwise has no read
// surface for -- MergeKeysForPath and its siblings are point queries -- and exists so a
// differential test can compare a whole snapshot rather than the paths someone thought to ask
// about.
func (c *CompiledSpecs) RenderStructure(toolchainType workerapi.ToolchainType) string {
	var b strings.Builder

	b.WriteString("# merge keys: resourceType\tpath\tkeys\n")
	for _, rt := range resourceTypesFor(toolchainType, c.mergeKeys) {
		byPath := c.mergeKeys[specKey{toolchainType, rt}]
		for _, path := range sortedStringKeys(byPath) {
			fmt.Fprintf(&b, "mergeKey\t%s\t%s\t%s\n", rt, path, strings.Join(byPath[path], ","))
		}
	}

	b.WriteString("\n# exclusive fields: resourceType\tpath\tmembers\tdiscriminator\tallowedMember\n")
	for _, rt := range resourceTypesFor(toolchainType, c.exclusiveFields) {
		byPath := c.exclusiveFields[specKey{toolchainType, rt}]
		for _, path := range sortedStringKeys(byPath) {
			group := byPath[path]
			members := append([]string(nil), group.Members...)
			sort.Strings(members)
			allowed := make([]string, 0, len(group.AllowedMember))
			for value, member := range group.AllowedMember {
				allowed = append(allowed, value+"="+member)
			}
			sort.Strings(allowed)
			fmt.Fprintf(&b, "exclusive\t%s\t%s\t%s\t%s\t%s\n",
				rt, path, strings.Join(members, ","), group.Discriminator, strings.Join(allowed, ","))
		}
	}

	b.WriteString("\n# map-key paths: resourceType\tpath\n")
	for _, rt := range resourceTypesFor(toolchainType, c.mapKeyPaths) {
		byPath := c.mapKeyPaths[specKey{toolchainType, rt}]
		for _, path := range sortedStringKeys(byPath) {
			fmt.Fprintf(&b, "mapKey\t%s\t%s\n", rt, path)
		}
	}

	return b.String()
}

// RenderAttributes writes every attribute path declared for one toolchain in a stable order,
// as tab-separated lines. It is the attribute counterpart of RenderStructure, and exists for
// the same reason: an attribute that never reaches the path registry -- one whose paths are
// handed straight to a visitor -- is covered by no other capture.
func (c *CompiledSpecs) RenderAttributes(toolchainType workerapi.ToolchainType) string {
	var b strings.Builder
	// A target is written only where there is one, so that adding references to a type leaves
	// every line that has none untouched in the capture.
	b.WriteString("# declared attributes: resourceType\tattributeName\tpath\tdataType[\ttarget]\n")
	for _, rt := range resourceTypesFor(toolchainType, c.attributes) {
		byAttribute := c.attributes[specKey{toolchainType, rt}]
		for _, attributeName := range sortedAttributeNames(byAttribute) {
			paths := append([]AttributePath(nil), byAttribute[attributeName]...)
			sort.Slice(paths, func(i, j int) bool {
				if paths[i].Path != paths[j].Path {
					return paths[i].Path < paths[j].Path
				}
				return paths[i].Target < paths[j].Target
			})
			for _, path := range paths {
				fmt.Fprintf(&b, "attribute\t%s\t%s\t%s\t%s", rt, attributeName, path.Path, path.DataType)
				if path.Target != "" {
					fmt.Fprintf(&b, "\t%s", path.Target)
				}
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// resourceTypesFor returns the resource types one of the lookups holds for a toolchain, sorted.
func resourceTypesFor[V any](toolchainType workerapi.ToolchainType, lookup map[specKey]V) []api.ResourceType {
	types := make([]api.ResourceType, 0, len(lookup))
	for key := range lookup {
		if key.toolchainType == toolchainType {
			types = append(types, key.resourceType)
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// AttributeDescriptor is everything about an attribute that does not vary by resource type:
// what the value is, how to read and write it, and how it is recorded. Specs say where an
// attribute lives; this says what it is. It stays in Go because getters and setters are Go.
type AttributeDescriptor struct {
	// DataType is the default for declared paths that do not state one of their own. An
	// attribute may hold paths of more than one type, so this is a default and not a
	// constraint.
	DataType api.DataType

	EmbeddedAccessorType   api.EmbeddedAccessorType
	EmbeddedAccessorConfig string

	api.AttributeNeedsProvidesDetails
	Enricher AttributeEnricher

	IsNeeded   bool
	IsProvided bool

	// Derived names attributes registered at a suffix of every path declared for this one.
	// A hostname declares one path; the subdomain and the domain within it are read through
	// embedded accessors at "<path>#subdomain" and "<path>#domain", and a type that has a
	// hostname always has both. Declaring the suffix once beats declaring three paths per
	// type that have to be kept in step. Each derived attribute has its own descriptor, so it
	// carries its own accessor, getter and setter.
	Derived []DerivedAttribute

	// DescribePaths asks the toolchain's PathEnricher for a description of each declared path,
	// from whatever schema it has. It is per attribute because it is a real cost -- a schema
	// lookup per path at registration -- and not every attribute wants it: nothing reads the
	// value at an immutable path, so describing one buys nothing.
	DescribePaths bool
}

// hasRegistrationDetails reports whether the descriptor has anything to pass as registration
// details. Passing an empty, non-nil AttributeRegistrationDetails is not equivalent to passing
// none: it makes registerPaths give every path an empty Details it would otherwise not have.
func (d AttributeDescriptor) hasRegistrationDetails() bool {
	return d.Enricher != nil ||
		len(d.ProvidedProperties) > 0 ||
		len(d.NeededRequired) > 0 ||
		len(d.NeededPreferred) > 0
}

// RegisterAttributeGroup declares that one attribute name stands for the union of others, so a
// function reading it through GetPathRegistryForAttributeName sees every member's paths. The
// group itself owns no paths and appears in no spec; it exists so attributes that must be
// distinguishable when written can still be read together.
func RegisterAttributeGroup(
	resourceProvider ResourceProvider,
	groupName api.AttributeName,
	members ...api.AttributeName,
) {
	attributeRegistry := resourceProvider.GetAttributeRegistry()
	descriptor, exists := attributeRegistry[groupName]
	if !exists {
		descriptor = &api.AttributeDescriptor{AttributeName: groupName}
		attributeRegistry[groupName] = descriptor
	}
	descriptor.AttributeGroup = members
}

// defaultValueAs coerces a declared default to the data type its path is registered with.
// A spec read from YAML or JSON has no integers -- every number arrives as a float64 -- so an
// int-typed path would otherwise be handed a value it rejects, and silently keep whatever was
// already there.
func defaultValueAs(value any, dataType api.DataType) any {
	number, isFloat := value.(float64)
	if !isFloat || dataType != api.DataTypeInt {
		return value
	}
	return int(number)
}

// DerivedAttribute is an attribute registered at a suffix of another attribute's paths.
type DerivedAttribute struct {
	AttributeName api.AttributeName
	PathSuffix    string
}

// PathEnricher adds per-path detail that only a toolchain can compute -- a field description
// read out of its schema, say -- to the paths about to be registered for one resource type.
type PathEnricher func(api.ResourceType, api.PathToVisitorInfoType)

// RegisterDeclaredAttributePaths registers every attribute path the compiled specs declare for
// a toolchain, pairing each with the descriptor registered for its attribute name. It is the
// data-driven replacement for hand-rolled loops over per-concern tables.
//
// Registration order is deterministic: resource types and attribute names are both sorted, so
// two runs of the same specs produce the same registry.
func RegisterDeclaredAttributePaths(
	resourceProvider ResourceProvider,
	compiled *CompiledSpecs,
	toolchainType workerapi.ToolchainType,
	descriptors map[api.AttributeName]AttributeDescriptor,
	enrich PathEnricher,
) error {
	for _, resourceType := range compiled.ResourceTypesWithAttributes(toolchainType) {
		byAttribute := compiled.DeclaredAttributes(toolchainType, resourceType)
		for _, attributeName := range sortedAttributeNames(byAttribute) {
			// A path carrying a Target is registered by the toolchain's reference pass, which
			// is where the requirement it states meets the other sources of references --
			// kustomize's table, for Kubernetes -- and where the target's own name is
			// registered as the matching provides. A descriptor holds one getter, one setter
			// and one set of properties for every path under its name, and a reference's are
			// per target, so it cannot describe one. A type whose only paths for an attribute
			// are references therefore needs no descriptor for it.
			declaredPaths := pathsWithoutTarget(byAttribute[attributeName])
			if len(declaredPaths) == 0 {
				continue
			}

			descriptor, described := descriptors[attributeName]
			if !described {
				return fmt.Errorf("resource type %s declares attribute %q, which has no descriptor",
					resourceType, attributeName)
			}

			// A path may state its own needed/provided role, so paths are registered a role at
			// a time: the flags are an argument of the registration call rather than a field of
			// each path.
			registerOne := func(name api.AttributeName, d AttributeDescriptor, suffix string,
				group []AttributePath, isNeeded, isProvided bool) {
				pathInfos := make(api.PathToVisitorInfoType, len(group))
				for _, declared := range group {
					dataType := declared.DataType
					if dataType == "" {
						dataType = d.DataType
					}
					path := api.UnresolvedPath(declared.Path + suffix)
					pathInfo := &api.PathVisitorInfo{
						Path:                   path,
						AttributeName:          name,
						DataType:               dataType,
						EmbeddedAccessorType:   d.EmbeddedAccessorType,
						EmbeddedAccessorConfig: d.EmbeddedAccessorConfig,
					}
					if declared.Default != nil {
						pathInfo.Details = &api.AttributeDetails{
							DefaultValue: defaultValueAs(declared.Default, dataType),
						}
					}
					pathInfos[path] = pathInfo
				}
				if d.DescribePaths && enrich != nil {
					enrich(resourceType, pathInfos)
				}

				var details *AttributeRegistrationDetails
				if d.hasRegistrationDetails() {
					details = &AttributeRegistrationDetails{
						AttributeNeedsProvidesDetails: d.AttributeNeedsProvidesDetails,
						Enricher:                      d.Enricher,
					}
				}

				RegisterPathsByAttributeName(
					resourceProvider, name, resourceType, pathInfos, details, isNeeded, isProvided)
			}

			registerByRole := func(name api.AttributeName, d AttributeDescriptor, suffix string) {
				for _, group := range groupPathsByRole(declaredPaths, d) {
					registerOne(name, d, suffix, group.paths, group.isNeeded, group.isProvided)
				}
			}

			registerByRole(attributeName, descriptor, "")
			for _, derived := range descriptor.Derived {
				derivedDescriptor, described := descriptors[derived.AttributeName]
				if !described {
					return fmt.Errorf("attribute %q derives %q, which has no descriptor",
						attributeName, derived.AttributeName)
				}
				registerByRole(derived.AttributeName, derivedDescriptor, derived.PathSuffix)
			}
		}
	}
	return nil
}

// pathRole is a set of declared paths sharing a needed/provided role.
type pathRole struct {
	isNeeded   bool
	isProvided bool
	paths      []AttributePath
}

// groupPathsByRole splits declared paths by the role each plays, which is the descriptor's
// unless the path states one of its own. Order is deterministic: the descriptor's role first.
func groupPathsByRole(paths []AttributePath, d AttributeDescriptor) []pathRole {
	var groups []pathRole
	for _, path := range paths {
		isNeeded, isProvided := d.IsNeeded, d.IsProvided
		if path.Needed || path.Provided {
			isNeeded, isProvided = path.Needed, path.Provided
		}
		placed := false
		for i := range groups {
			if groups[i].isNeeded == isNeeded && groups[i].isProvided == isProvided {
				groups[i].paths = append(groups[i].paths, path)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, pathRole{isNeeded: isNeeded, isProvided: isProvided, paths: []AttributePath{path}})
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].isNeeded != groups[j].isNeeded {
			return groups[i].isNeeded == d.IsNeeded
		}
		return groups[i].isProvided == d.IsProvided
	})
	return groups
}

// pathsWithoutTarget returns the declared paths that are not references.
func pathsWithoutTarget(paths []AttributePath) []AttributePath {
	kept := make([]AttributePath, 0, len(paths))
	for _, path := range paths {
		if path.Target == "" {
			kept = append(kept, path)
		}
	}
	return kept
}

func sortedAttributeNames[V any](m map[api.AttributeName]V) []api.AttributeName {
	names := make([]api.AttributeName, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}
