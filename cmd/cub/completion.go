// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Shell completion for entity references.
//
// A reference completes in two steps. With no space on the line, the spaces are
// offered first, each as "space/" with no trailing blank so the shell stays on
// the word; once the word names a space, the entities in it are offered as
// "space/slug". The completed form is the qualified one on purpose: a command
// assembled with the tab key then carries its own space wherever it is pasted
// or scripted, and keeps working when the default space is no longer consulted.
// With --space on the line the space is already known, so the bare slugs in it
// are offered instead.
//
// Completion runs from cobra's hidden __complete command, outside the target
// command's own pre-run, so no space has been resolved; it reads the --space
// flag and the context itself. Nothing may reach stdout except through the
// return value, and a server that does not answer promptly must not hang the
// shell, so every lookup runs under a short deadline and every failure
// completes to nothing.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// completionTimeout bounds one completion request end to end. A tab press that
// takes longer than this is worse than one that offers nothing.
const completionTimeout = 3 * time.Second

// refKind is one kind of entity a reference can name, with the lookup that
// lists the slugs of that kind in a space. The zero UUID means every space.
type refKind struct {
	name  string
	slugs func(ctx context.Context, spaceID goclientnew.UUID) ([]string, error)
	// organizationLevel marks a kind that is in no space, which completes flat, from the
	// organization, with no space/ prefix.
	organizationLevel bool
}

// slugLister adapts one cubapi list function to the slugs lookup. Only the
// slug is selected, since nothing else is read.
func slugLister[E any](
	list func(context.Context, *cubapi.Client, cubapi.Where, cubapi.ListOpts) ([]*E, error),
	slugOf func(*E) string,
) func(context.Context, goclientnew.UUID) ([]string, error) {
	return func(ctx context.Context, spaceID goclientnew.UUID) ([]string, error) {
		where := cubapi.Where{}
		if spaceID != (goclientnew.UUID{}) {
			where = where.SpaceID(spaceID)
		}
		items, err := list(ctx, cubClient, where, cubapi.ListOpts{Select: "Slug"})
		if err != nil {
			return nil, err
		}
		slugs := make([]string, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			if slug := slugOf(item); slug != "" {
				slugs = append(slugs, slug)
			}
		}
		sort.Strings(slugs)
		// An organization-wide listing repeats a slug once per space that holds it,
		// and the same word cannot be offered twice.
		return slices.Compact(slugs), nil
	}
}

var (
	kindSpace = &refKind{name: "space", slugs: slugLister(
		func(ctx context.Context, c *cubapi.Client, w cubapi.Where, o cubapi.ListOpts) ([]*goclientnew.ExtendedSpace, error) {
			return cubapi.ListSpaces(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedSpace) string {
			if e.Space == nil {
				return ""
			}
			return e.Space.Slug
		})}
	kindUnit = &refKind{name: "unit", slugs: slugLister(
		func(ctx context.Context, c *cubapi.Client, w cubapi.Where, o cubapi.ListOpts) ([]*goclientnew.ExtendedUnit, error) {
			return cubapi.ListUnits(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedUnit) string {
			if e.Unit == nil {
				return ""
			}
			return e.Unit.Slug
		})}
	// A variant argument takes <component>/<variant> or a space slug; its lister is the
	// space lister, and complete adds the components, held open.
	kindVariantOrSpace = &refKind{name: "variant", slugs: kindSpace.slugs}
	// A resource argument takes <kind>/[<namespace>/]<name>, completed by completeResource
	// from the resources in scope; it has no slug lister.
	kindResource  = &refKind{name: "resource"}
	kindComponent = &refKind{name: "component", organizationLevel: true, slugs: slugLister(cubapi.ListComponents,
		func(e *goclientnew.ExtendedComponent) string {
			if e.Component == nil {
				return ""
			}
			return e.Component.Slug
		})}
	kindServiceAccount = &refKind{name: "serviceaccount", organizationLevel: true, slugs: slugLister(cubapi.ListServiceAccounts,
		func(e *goclientnew.ExtendedServiceAccount) string {
			if e.ServiceAccount == nil {
				return ""
			}
			return e.ServiceAccount.Slug
		})}
	kindTarget = &refKind{name: "target", slugs: slugLister(cubapi.ListTargets,
		func(e *goclientnew.ExtendedTarget) string {
			if e.Target == nil {
				return ""
			}
			return e.Target.Slug
		})}
	kindTrigger = &refKind{name: "trigger", slugs: slugLister(cubapi.ListTriggers,
		func(e *goclientnew.ExtendedTrigger) string {
			if e.Trigger == nil {
				return ""
			}
			return e.Trigger.Slug
		})}
	kindFilter = &refKind{name: "filter", slugs: slugLister(
		func(ctx context.Context, c *cubapi.Client, w cubapi.Where, o cubapi.ListOpts) ([]*goclientnew.ExtendedFilter, error) {
			return cubapi.ListFilters(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedFilter) string {
			if e.Filter == nil {
				return ""
			}
			return e.Filter.Slug
		})}
	kindInvocation = &refKind{name: "invocation", slugs: slugLister(cubapi.ListInvocations,
		func(e *goclientnew.ExtendedInvocation) string {
			if e.Invocation == nil {
				return ""
			}
			return e.Invocation.Slug
		})}
	kindChangeSet = &refKind{name: "changeset", slugs: slugLister(cubapi.ListChangeSets,
		func(e *goclientnew.ExtendedChangeSet) string {
			if e.ChangeSet == nil {
				return ""
			}
			return e.ChangeSet.Slug
		})}
	kindChangeOrder = &refKind{name: "changeorder", slugs: slugLister(cubapi.ListChangeOrders,
		func(e *goclientnew.ExtendedChangeOrder) string {
			if e.ChangeOrder == nil {
				return ""
			}
			return e.ChangeOrder.Slug
		})}
	kindChangeWorkflow = &refKind{name: "changeworkflow", slugs: slugLister(cubapi.ListChangeWorkflows,
		func(e *goclientnew.ExtendedChangeWorkflow) string {
			if e.ChangeWorkflow == nil {
				return ""
			}
			return e.ChangeWorkflow.Slug
		})}
	kindTag = &refKind{name: "tag", slugs: slugLister(cubapi.ListTags,
		func(e *goclientnew.ExtendedTag) string {
			if e.Tag == nil {
				return ""
			}
			return e.Tag.Slug
		})}
	kindView = &refKind{name: "view", slugs: slugLister(cubapi.ListViews,
		func(e *goclientnew.ExtendedView) string {
			if e.View == nil {
				return ""
			}
			return e.View.Slug
		})}
	kindChannel = &refKind{name: "channel", slugs: slugLister(
		func(ctx context.Context, c *cubapi.Client, w cubapi.Where, o cubapi.ListOpts) ([]*goclientnew.ExtendedChannel, error) {
			return cubapi.ListChannels(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedChannel) string {
			if e.Channel == nil {
				return ""
			}
			return e.Channel.Slug
		})}
	kindAttribute = &refKind{name: "attribute", slugs: slugLister(cubapi.ListAttributes,
		func(e *goclientnew.ExtendedAttribute) string {
			if e.Attribute == nil {
				return ""
			}
			return e.Attribute.Slug
		})}
	kindLink = &refKind{name: "link", slugs: slugLister(cubapi.ListLinks,
		func(e *goclientnew.ExtendedLink) string {
			if e.Link == nil {
				return ""
			}
			return e.Link.Slug
		})}
	kindWorker = &refKind{name: "worker", slugs: slugLister(
		func(ctx context.Context, c *cubapi.Client, w cubapi.Where, o cubapi.ListOpts) ([]*goclientnew.ExtendedBridgeWorker, error) {
			return cubapi.ListBridgeWorkers(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedBridgeWorker) string {
			if e.BridgeWorker == nil {
				return ""
			}
			return e.BridgeWorker.Slug
		})}
)

// refArgKinds says which kind of reference each positional argument of a
// command names, by position. A nil entry is a positional that is not a
// reference (a new slug, a file, a number), and a position past the end of the
// list is left to the shell's default completion, which is how "unit update
// <unit> [config-file]" completes a file for its second argument.
//
// The table is the single place that says where references appear, so that
// adding completion to a command is one line here rather than a hook in its
// file. TestReferenceCompletionsNameRealCommands pins every entry to a command
// that exists.
var refArgKinds = map[string][]*refKind{
	"attribute get": {kindAttribute}, "attribute update": {kindAttribute}, "attribute delete": {kindAttribute},
	"changeorder get": {kindChangeOrder}, "changeorder update": {kindChangeOrder}, "changeorder delete": {kindChangeOrder},
	"changeset get": {kindChangeSet}, "changeset update": {kindChangeSet}, "changeset delete": {kindChangeSet},
	"channel get": {kindChannel}, "channel update": {kindChannel}, "channel delete": {kindChannel},
	"changeworkflow get": {kindChangeWorkflow}, "changeworkflow update": {kindChangeWorkflow}, "changeworkflow delete": {kindChangeWorkflow},
	"component get": {kindComponent}, "component update": {kindComponent}, "component delete": {kindComponent},
	"component open": {kindComponent}, "component tree": {kindComponent},
	"filter get": {kindFilter}, "filter update": {kindFilter}, "filter delete": {kindFilter},
	"group add-worker": {nil, kindWorker}, "group remove-worker": {nil, kindWorker},
	"invocation get": {kindInvocation}, "invocation update": {kindInvocation}, "invocation delete": {kindInvocation},
	"invocation invoke get": {kindInvocation}, "invocation invoke set": {kindInvocation}, "invocation invoke vet": {kindInvocation},
	"serviceaccount get": {kindServiceAccount}, "serviceaccount update": {kindServiceAccount}, "serviceaccount delete": {kindServiceAccount},
	"serviceaccount key add": {kindServiceAccount}, "serviceaccount key list": {kindServiceAccount}, "serviceaccount key delete": {kindServiceAccount},
	"serviceaccount token create": {kindServiceAccount}, "serviceaccount token list": {kindServiceAccount}, "serviceaccount token revoke": {kindServiceAccount},
	"review-comment get": {kindUnit}, "review-comment list": {kindUnit}, "review-comment create": {kindUnit},
	"review-comment update": {kindUnit}, "review-comment delete": {kindUnit},
	"k8s collect":     {kindTarget},
	"link get":        {kindLink},
	"link delete":     {kindLink},
	"link create":     {nil, kindUnit, kindUnit, kindSpace},
	"link update":     {kindLink, kindUnit, kindUnit, kindSpace},
	"mutation get":    {kindUnit},
	"mutation list":   {kindUnit},
	"release publish": {kindVariantOrSpace},
	"resource data":   {kindResource},
	"revision get":    {kindUnit},
	"revision data":   {kindUnit},
	"space get":       {kindSpace}, "space update": {kindSpace}, "space delete": {kindSpace}, "space open": {kindSpace},
	"tag get": {kindTag}, "tag update": {kindTag}, "tag delete": {kindTag},
	"target get": {kindTarget}, "target update": {kindTarget}, "target delete": {kindTarget},
	"target access": {kindTarget, kindUnit},
	"target create": {nil, nil, kindWorker},
	"trigger get":   {kindTrigger}, "trigger update": {kindTrigger}, "trigger delete": {kindTrigger},
	"unit get": {kindUnit}, "unit update": {kindUnit}, "unit delete": {kindUnit}, "unit open": {kindUnit},
	"unit blame": {kindUnit}, "unit cancel": {kindUnit}, "unit conflicts": {kindUnit},
	"unit data": {kindUnit}, "unit diff": {kindUnit}, "unit data-edit": {kindUnit}, "unit mutation-sources": {kindUnit},
	"unit path":      {kindUnit},
	"unit set-guard": {kindUnit}, "unit set-protection": {kindUnit},
	"unit set-target":  {kindUnit, kindTarget},
	"unit tag":         {kindTag},
	"unit-action get":  {kindUnit},
	"unit-action data": {kindUnit},
	"unit-event get":   {kindUnit},
	"variant approve":  {kindVariantOrSpace}, "variant demote": {kindVariantOrSpace}, "variant diff": {kindVariantOrSpace}, "variant promote": {kindVariantOrSpace},
	"variant blame": {kindVariantOrSpace}, "variant conflicts": {kindVariantOrSpace}, "variant delete": {kindVariantOrSpace},
	"variant create": {nil, kindVariantOrSpace},
	"view get":       {kindView}, "view update": {kindView}, "view delete": {kindView},
	"worker get": {kindWorker}, "worker update": {kindWorker}, "worker delete": {kindWorker},
	"worker get-envs": {kindWorker}, "worker get-secret": {kindWorker},
	"worker list-function": {kindWorker}, "worker list-status": {kindWorker},
	"worker key add": {kindWorker}, "worker key list": {kindWorker}, "worker key delete": {kindWorker},
	"changeorder rename": {kindChangeOrder, nil}, "changeset rename": {kindChangeSet, nil},
	"changeworkflow rename": {kindChangeWorkflow, nil}, "channel rename": {kindChannel, nil}, "filter rename": {kindFilter, nil},
	"invocation rename": {kindInvocation, nil}, "link rename": {kindLink, nil}, "space rename": {kindSpace, nil},
	"tag rename": {kindTag, nil}, "target rename": {kindTarget, nil}, "trigger rename": {kindTrigger, nil},
	"unit rename": {kindUnit, nil}, "view rename": {kindView, nil}, "worker rename": {kindWorker, nil},
	"serviceaccount rename": {kindServiceAccount, nil},
}

// refFlagKinds maps a flag name to the kind of reference it takes, for every
// command that defines a flag of that name. Flag names are entity names
// throughout the CLI, which is what makes one map serve the whole tree.
var refFlagKinds = map[string]*refKind{
	"space":          kindSpace,
	"from-space":     kindSpace,
	"upstream-space": kindSpace,
	"unit":           kindUnit,
	"upstream-unit":  kindUnit,
	"with-unit":      kindUnit,
	"target":         kindTarget,
	"component":      kindComponent,
	"trigger":        kindTrigger,
	"invocation":     kindInvocation,
	"filter":         kindFilter,
	"changeset":      kindChangeSet,
	"changeorder":    kindChangeOrder,
	"tag":            kindTag,
	"view":           kindView,
	"channel":        kindChannel,
	"worker":         kindWorker,
}

// installReferenceCompletions attaches the completers to the command tree. It
// runs once, from main, after every init has added its commands.
func installReferenceCompletions(root *cobra.Command) {
	for path, kinds := range refArgKinds {
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil || cmd == nil || cmd.ValidArgsFunction != nil {
			continue
		}
		kinds := kinds
		cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) >= len(kinds) || kinds[len(args)] == nil {
				return nil, cobra.ShellCompDirectiveDefault
			}
			return liveCompleter().complete(kinds[len(args)], toComplete)
		}
	}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for name, kind := range refFlagKinds {
			if cmd.LocalFlags().Lookup(name) == nil {
				continue
			}
			kind := kind
			_ = cmd.RegisterFlagCompletionFunc(name, func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				return liveCompleter().complete(kind, toComplete)
			})
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)
	// --variant and --resource are not entity references, so they are attached to the
	// commands that define them rather than found by name: the --variant of "cub space
	// create" and "cub variant upload" is a label value, and must not complete as one.
	for _, cmd := range spaceScopedCommands {
		_ = cmd.RegisterFlagCompletionFunc("variant", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return liveCompleter().completeVariant(toComplete)
		})
	}
	for _, cmd := range resourceFlagCommands {
		_ = cmd.RegisterFlagCompletionFunc("resource", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return liveCompleter().completeResource(toComplete)
		})
	}
}

// refCompleter holds what completing a reference needs from the outside: the
// spaces, how a space reference becomes an id, the --space in effect, and a
// context to run under. Tests supply fakes; liveCompleter reads the real ones.
type refCompleter struct {
	ctx         context.Context
	spaceFlag   string
	variantFlag string
	spaces      func(ctx context.Context) ([]string, error)
	spaceID     func(ctx context.Context, ref string) (goclientnew.UUID, error)
	// components lists the Component slugs; variants the Variant labels of one Component's
	// spaces; variantSpaceID resolves a <component>/<variant> reference as --variant does.
	components     func(ctx context.Context) ([]string, error)
	variants       func(ctx context.Context, component string) ([]string, error)
	variantSpaceID func(ctx context.Context, ref string) (goclientnew.UUID, error)
	// resources lists the kind and name of every resource in a space, or in the
	// organization for the zero UUID.
	resources func(ctx context.Context, spaceID goclientnew.UUID) ([]resourceCandidate, error)
}

// resourceCandidate is one resource as --resource names it: its kind, as written in the
// configuration, and its name without the namespace.
type resourceCandidate struct {
	Kind string
	Name string
}

// liveCompleter builds the completer over the API client. The client is
// normally already up, since the root's persistent pre-run ran for the hidden
// __complete command; it is initialized here when that did not happen. A
// completer with no context completes to nothing.
func liveCompleter() *refCompleter {
	if cubClient == nil {
		active := contextManager.ActiveContext()
		if active == nil {
			return &refCompleter{}
		}
		if _, err := InitializeClient(active); err != nil {
			return &refCompleter{}
		}
	}
	// The deadline outlives this call on purpose: the context is handed to the
	// lookups, and the process exits as soon as the completion is printed.
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	_ = cancel
	componentID := func(ctx context.Context, ref string) (goclientnew.UUID, error) {
		if id, err := uuid.Parse(ref); err == nil {
			return goclientnew.UUID(id), nil
		}
		component, err := cubapi.ResolveComponent(ctx, cubClient, cubapi.ParseRef(ref), cubapi.ResolveOpts{Select: "ComponentID,Slug"})
		if err != nil {
			return goclientnew.UUID{}, err
		}
		return component.Component.ComponentID, nil
	}
	return &refCompleter{
		ctx:         ctx,
		spaceFlag:   spaceFlag,
		variantFlag: variantFlag,
		spaces: func(ctx context.Context) ([]string, error) {
			return kindSpace.slugs(ctx, goclientnew.UUID{})
		},
		spaceID: func(ctx context.Context, ref string) (goclientnew.UUID, error) {
			if id, err := uuid.Parse(ref); err == nil {
				return goclientnew.UUID(id), nil
			}
			space, err := cubapi.ResolveSpace(ctx, cubClient, cubapi.ParseRef(ref), cubapi.ResolveOpts{Select: "SpaceID,Slug"})
			if err != nil {
				return goclientnew.UUID{}, err
			}
			return space.Space.SpaceID, nil
		},
		components: func(ctx context.Context) ([]string, error) {
			components, err := cubapi.ListComponents(ctx, cubClient, cubapi.Where{}, cubapi.ListOpts{Select: "Slug"})
			if err != nil {
				return nil, err
			}
			slugs := make([]string, 0, len(components))
			for _, component := range components {
				if component != nil && component.Component != nil && component.Component.Slug != "" {
					slugs = append(slugs, component.Component.Slug)
				}
			}
			sort.Strings(slugs)
			return slugs, nil
		},
		variants: func(ctx context.Context, component string) ([]string, error) {
			id, err := componentID(ctx, component)
			if err != nil {
				return nil, err
			}
			spaces, err := listVariantSpaces(ctx, id, "")
			if err != nil {
				return nil, err
			}
			variants := make([]string, 0, len(spaces))
			for _, space := range spaces {
				if space == nil || space.Space == nil {
					continue
				}
				if variant := space.Space.Labels["Variant"]; variant != "" {
					variants = append(variants, variant)
				}
			}
			sort.Strings(variants)
			return slices.Compact(variants), nil
		},
		variantSpaceID: func(ctx context.Context, ref string) (goclientnew.UUID, error) {
			component, variant, err := parseVariantRef(ref)
			if err != nil {
				return goclientnew.UUID{}, err
			}
			id, err := componentID(ctx, component)
			if err != nil {
				return goclientnew.UUID{}, err
			}
			spaces, err := listVariantSpaces(ctx, id, variant)
			if err != nil {
				return goclientnew.UUID{}, err
			}
			if len(spaces) != 1 {
				return goclientnew.UUID{}, fmt.Errorf("%d spaces are variant %s", len(spaces), ref)
			}
			return spaces[0].Space.SpaceID, nil
		},
		resources: func(ctx context.Context, spaceID goclientnew.UUID) ([]resourceCandidate, error) {
			where := cubapi.Where{}
			if spaceID != (goclientnew.UUID{}) {
				where = where.SpaceID(spaceID)
			}
			rows, err := cubapi.ListResources(ctx, cubClient, where, cubapi.ListOpts{Select: "ResourceID,ResourceType,ResourceName"})
			if err != nil {
				return nil, err
			}
			candidates := make([]resourceCandidate, 0, len(rows))
			for _, row := range rows {
				if row == nil || row.Resource == nil {
					continue
				}
				_, kind := splitResourceType(row.Resource.ResourceType)
				_, name := splitResourceName(row.Resource.ResourceName)
				candidates = append(candidates, resourceCandidate{Kind: kind, Name: name})
			}
			return candidates, nil
		},
	}
}

// completeVariant completes a --variant reference in two steps, as a space-qualified
// reference does: the components first, each as "component/" and held open, then the
// variants of the named component as "component/variant".
func (c *refCompleter) completeVariant(toComplete string) ([]string, cobra.ShellCompDirective) {
	if c.ctx == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if component, variant, qualified := strings.Cut(toComplete, "/"); qualified {
		variants, err := c.variants(c.ctx, component)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(variants, variant, component+"/"), cobra.ShellCompDirectiveNoFileComp
	}
	components, err := c.components(c.ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := withPrefix(components, toComplete, "")
	for i := range out {
		out[i] += "/"
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// completeVariantOrSpace completes an argument that takes a variant as <component>/<variant>
// or as a space slug: the space slugs and the components, the latter held open with a
// slash, and once a component is typed, its variants. The slash on every component entry
// is what tells the two apart in the list.
func (c *refCompleter) completeVariantOrSpace(toComplete string) ([]string, cobra.ShellCompDirective) {
	if c.ctx == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if strings.Contains(toComplete, "/") {
		return c.completeVariant(toComplete)
	}
	spaces, err := c.spaces(c.ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	components, err := c.components(c.ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := withPrefix(spaces, toComplete, "")
	held := withPrefix(components, toComplete, "")
	for _, component := range held {
		out = append(out, component+"/")
	}
	sort.Strings(out)
	directive := cobra.ShellCompDirectiveNoFileComp
	if len(held) > 0 {
		directive |= cobra.ShellCompDirectiveNoSpace
	}
	return out, directive
}

// scopeID is the space the command line scopes to, from --variant or --space, or the zero
// UUID for the organization. It reports false when a scope was named but does not resolve.
func (c *refCompleter) scopeID() (goclientnew.UUID, bool) {
	if c.variantFlag != "" {
		id, err := c.variantSpaceID(c.ctx, c.variantFlag)
		return id, err == nil
	}
	if c.spaceFlag != "" && c.spaceFlag != "*" {
		id, err := c.spaceID(c.ctx, c.spaceFlag)
		return id, err == nil
	}
	return goclientnew.UUID{}, true
}

// completeResource completes a --resource reference from the resources in scope: the
// kinds first, each as "Kind/" and held open, then the names of the typed kind as
// "Kind/name". A namespace, the optional middle part, is typed by hand; once the word has
// a second slash nothing is offered.
func (c *refCompleter) completeResource(toComplete string) ([]string, cobra.ShellCompDirective) {
	if c.ctx == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	spaceID, ok := c.scopeID()
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	candidates, err := c.resources(c.ctx, spaceID)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if kind, name, qualified := strings.Cut(toComplete, "/"); qualified {
		if strings.Contains(name, "/") {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var names []string
		for _, candidate := range candidates {
			if strings.EqualFold(candidate.Kind, kind) {
				names = append(names, candidate.Name)
			}
		}
		sort.Strings(names)
		return withPrefix(slices.Compact(names), name, kind+"/"), cobra.ShellCompDirectiveNoFileComp
	}
	kinds := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		kinds = append(kinds, candidate.Kind)
	}
	sort.Strings(kinds)
	out := withPrefix(slices.Compact(kinds), toComplete, "")
	for i := range out {
		out[i] += "/"
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// complete returns the candidates for a reference of the given kind that
// starts with toComplete.
func (c *refCompleter) complete(kind *refKind, toComplete string) ([]string, cobra.ShellCompDirective) {
	if c.ctx == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if kind == kindSpace {
		spaces, err := c.spaces(c.ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(spaces, toComplete, ""), cobra.ShellCompDirectiveNoFileComp
	}
	if kind == kindVariantOrSpace {
		return c.completeVariantOrSpace(toComplete)
	}
	if kind == kindResource {
		return c.completeResource(toComplete)
	}
	if kind.organizationLevel {
		slugs, err := kind.slugs(c.ctx, goclientnew.UUID{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(slugs, toComplete, ""), cobra.ShellCompDirectiveNoFileComp
	}
	if space, name, qualified := strings.Cut(toComplete, "/"); qualified {
		// "*/" is the whole organization, which is the zero UUID.
		var spaceID goclientnew.UUID
		if space != "*" {
			var err error
			if spaceID, err = c.spaceID(c.ctx, space); err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
		slugs, err := kind.slugs(c.ctx, spaceID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(slugs, name, space+"/"), cobra.ShellCompDirectiveNoFileComp
	}
	if c.spaceFlag != "" && c.spaceFlag != "*" {
		spaceID, err := c.spaceID(c.ctx, c.spaceFlag)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		slugs, err := kind.slugs(c.ctx, spaceID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(slugs, toComplete, ""), cobra.ShellCompDirectiveNoFileComp
	}
	spaces, err := c.spaces(c.ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := withPrefix(spaces, toComplete, "")
	for i := range out {
		out[i] += "/"
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// withPrefix keeps the candidates that start with prefix and prepends lead to
// each, which is how a slug in a named space is offered as "space/slug".
func withPrefix(candidates []string, prefix, lead string) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			out = append(out, lead+candidate)
		}
	}
	return out
}

// The shells display the word they insert, so once "sqb/" is on the line a
// list of units reads "sqb/api  sqb/app" although only the slugs are news.
// Cobra's scripts have no hook for showing less than the inserted word, but
// zsh and bash both can: zsh moves a prefix out of the matched word with
// compset -P, and readline shows only what follows the last "/" of a filename
// completion. installCompletionCommand keeps cobra's completion command and
// patches the two scripts at the line that hands the candidates to the shell,
// which is why the anchors below are exact and TestCompletionScriptsHideQualifier
// pins them: a cobra upgrade that rewrites the script must fail that test rather
// than silently lose the behavior. A script that cannot be patched is emitted as
// generated, with a note on stderr, since a completion command that fails breaks
// every shell that sources it at startup.

const zshDescribeAnchor = `        if eval _describe $keepOrder "completions" completions $flagPrefix $noSpace; then`

const zshHideQualifier = `        # A qualified reference completes as "space/slug". Once the word names the
        # space, only the slug is news: when every candidate carries the word's own
        # "space/" prefix, move that prefix out of the match so the list shows the
        # slugs alone while the insertion still produces the whole reference.
        local refWord="${lastParam}"
        if [[ "${refWord}" == -*=* ]]; then
            refWord="${refWord#*=}"
        fi
        if [[ "${refWord}" == */* ]] && [ ${#completions} -ne 0 ]; then
            local qualifier="${refWord%/*}/"
            if [ ${#${(M)completions:#"${qualifier}"*}} -eq ${#completions} ]; then
                __cub_debug "Hiding qualifier ${qualifier} from the list"
                # The moved prefix already holds any "--flag=" part of the word, so
                # cobra's own flag prefix must not be inserted a second time.
                compset -P "*/"
                flagPrefix=""
                completions=("${(@)completions#"${qualifier}"}")
            fi
        fi
`

const bashStandardCaseAnchor = `        # Type: complete (normal completion)
        __cub_handle_standard_completion_case`

const bashHideQualifier = `        # A qualified reference completes as "space/slug". Once the word names the
        # space, only the slug is news: readline shows only what follows the last
        # "/" of a filename completion, so when every candidate carries the word's
        # own "space/" prefix, have it display them that way.
        if [[ "${cur}" == */* ]] && [[ $(type -t compopt) == builtin ]]; then
            local qualifier="${cur%/*}/" hideQualifier=1 candidate
            for candidate in "${completions[@]}"; do
                [[ "${candidate}" == "${qualifier}"* ]] || { hideQualifier=0; break; }
            done
            if (( hideQualifier )); then
                __cub_debug "Hiding qualifier ${qualifier} from the list"
                compopt -o filenames
            fi
        fi
`

// patchCompletionScript inserts patch before the first occurrence of anchor.
func patchCompletionScript(script, anchor, patch string) (string, bool) {
	at := strings.Index(script, anchor)
	if at < 0 {
		return script, false
	}
	return script[:at] + patch + script[at:], true
}

// installCompletionCommand creates cobra's completion command and wraps the
// zsh and bash generators so their scripts hide a space qualifier from the list.
func installCompletionCommand(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	for shell, gen := range map[string]func(*cobra.Command, io.Writer, bool) error{
		"zsh": func(root *cobra.Command, w io.Writer, noDesc bool) error {
			if noDesc {
				return root.GenZshCompletionNoDesc(w)
			}
			return root.GenZshCompletion(w)
		},
		"bash": func(root *cobra.Command, w io.Writer, noDesc bool) error {
			return root.GenBashCompletionV2(w, !noDesc)
		},
	} {
		cmd, _, err := root.Find([]string{"completion", shell})
		if err != nil || cmd == nil || cmd.Name() != shell {
			continue
		}
		shell, gen := shell, gen
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			noDesc, _ := cmd.Flags().GetBool("no-descriptions")
			var buf bytes.Buffer
			if err := gen(cmd.Root(), &buf, noDesc); err != nil {
				return err
			}
			script, ok := patchShellCompletion(shell, buf.String())
			if !ok {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: the %s completion script has changed shape; space qualifiers will show in completion lists\n", shell)
			}
			_, err := io.WriteString(cmd.OutOrStdout(), script)
			return err
		}
	}
}

// patchShellCompletion applies the qualifier-hiding patch for one shell.
func patchShellCompletion(shell, script string) (string, bool) {
	switch shell {
	case "zsh":
		return patchCompletionScript(script, zshDescribeAnchor, zshHideQualifier)
	case "bash":
		return patchCompletionScript(script, bashStandardCaseAnchor, bashHideQualifier)
	}
	return script, false
}
