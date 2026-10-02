// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"regexp"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// The lists of a Link are set by flags whose value is a tuple: the place in a Unit a value is
// read from or written to is <resource-type>:<resource-name>:<path>, as it is for a guard, with
// a name before it or an expression after it joined by "=". Each flag, when given, states its
// whole list.

var linkElementArgs struct {
	upstreamPaths, downstreamPaths, manualBindings []string
	upstreamGetters, downstreamSetters             []string
}

const resourcePathForm = "<resource-type>:<resource-name>:<path>"

func addLinkElementFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&linkElementArgs.upstreamPaths, "upstream-path", nil,
		"value a TransformPaths link reads from the upstream unit, as <name>="+resourcePathForm+" (repeatable). "+
			"The name is what an expression or a setter refers to it by, as .Params.<name> in a template and params.<name> in CEL")
	cmd.Flags().StringArrayVar(&linkElementArgs.downstreamPaths, "downstream-path", nil,
		"value a TransformPaths link writes to the downstream unit, as [<key>=]"+resourcePathForm+"=[<data-type>:][<evaluator>:]<expression> (repeatable). "+
			"The evaluator is template, the default, or cel; the data type is string, the default, int or bool. "+
			"The upstream values the expression refers to are recorded as its parameters. The key identifies the entry when two versions of the link are merged")
	cmd.Flags().StringArrayVar(&linkElementArgs.manualBindings, "manual-binding", nil,
		"binding stated for the link, as [<key>[:<attribute-name>]=]<needed "+resourcePathForm+">[=[<data-type>:]<provided "+resourcePathForm+">] (repeatable): "+
			"the place in the downstream unit that needs a value, then the place in the upstream unit that provides it. "+
			"The data type is string by default. An Insert link's one binding gives only the needed place, optionally followed by =<data-type>")
	cmd.Flags().StringArrayVar(&linkElementArgs.upstreamGetters, "upstream-getter", nil,
		"function a TransformPaths link runs on the upstream unit for a value, as <name>=<function> [<argument>...] (repeatable), "+
			"the function written as on the cub function do command line")
	cmd.Flags().StringArrayVar(&linkElementArgs.downstreamSetters, "downstream-setter", nil,
		"function a TransformPaths link runs on the downstream unit, as [<key>=]<function> [<argument>...] (repeatable), "+
			"the function written as on the cub function do command line. The upstream values its arguments refer to are recorded as its parameters")
}

// parseResourcePath reads <resource-type>:<resource-name>:<path>. The path is the rest of the
// value, so it may hold a colon.
func parseResourcePath(flag, value, tuple string) (*goclientnew.ResourceInfo, string, error) {
	parts := strings.SplitN(tuple, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, "", errors.Newf("--%s %q: %q must be %s", flag, value, tuple, resourcePathForm)
	}
	return &goclientnew.ResourceInfo{ResourceType: parts[0], ResourceName: parts[1]}, parts[2], nil
}

// leadingKey splits off a <key>= that begins a value. What precedes the first "=" is a key when
// it could not be a resource: when it has fewer than two colons.
func leadingKey(value string) (key, rest string) {
	before, after, found := strings.Cut(value, "=")
	if !found || strings.Count(before, ":") >= 2 {
		return "", value
	}
	return before, after
}

// upstreamParameterRegexp finds the upstream values an expression or an argument refers to:
// .Params.<name> in a template and params.<name> in CEL.
var upstreamParameterRegexp = regexp.MustCompile(`(?:\.Params\.|\bparams\.)([A-Za-z_][A-Za-z0-9_]*)`)

// referencedParameters are the names of the upstream values the texts refer to, sorted.
func referencedParameters(texts ...string) []string {
	seen := map[string]bool{}
	for _, text := range texts {
		for _, match := range upstreamParameterRegexp.FindAllStringSubmatch(text, -1) {
			seen[match[1]] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var linkValueDataTypes = map[string]bool{"string": true, "int": true, "bool": true}

func parseUpstreamPath(value string) (*goclientnew.NamedPath, error) {
	name, tuple, found := strings.Cut(value, "=")
	if !found || name == "" || strings.Contains(name, ":") {
		return nil, errors.Newf("--upstream-path %q must be <name>=%s", value, resourcePathForm)
	}
	resource, path, err := parseResourcePath("upstream-path", value, tuple)
	if err != nil {
		return nil, err
	}
	return &goclientnew.NamedPath{Name: name, Resource: resource, Path: path}, nil
}

func parseDownstreamPath(value string) (*goclientnew.PathExpression, error) {
	malformed := errors.Newf("--downstream-path %q must be [<key>=]%s=[<data-type>:][<evaluator>:]<expression>", value, resourcePathForm)
	key, rest := leadingKey(value)
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return nil, malformed
	}
	// The path may hold an "=" of its own, in a ?<key>=<value> segment.
	path, expression, err := splitSetExpression(parts[2])
	if err != nil {
		return nil, malformed
	}
	element := &goclientnew.PathExpression{
		Key:       key,
		Resource:  &goclientnew.ResourceInfo{ResourceType: parts[0], ResourceName: parts[1]},
		Path:      path,
		DataType:  "string",
		Evaluator: templateEvaluator,
	}
	if dataType, after, found := strings.Cut(expression, ":"); found && linkValueDataTypes[dataType] {
		element.DataType, expression = dataType, after
	}
	if after, found := strings.CutPrefix(expression, templatePrefix); found {
		expression = after
	} else if after, found := strings.CutPrefix(expression, celPrefix); found {
		element.Evaluator, expression = celEvaluator, after
	}
	if expression == "" {
		return nil, malformed
	}
	element.Expression = expression
	element.Parameters = referencedParameters(expression)
	return element, nil
}

func parseManualBinding(value string) (*goclientnew.Binding, error) {
	malformed := errors.Newf("--manual-binding %q must be [<key>[:<attribute-name>]=]<needed %s>[=[<data-type>:]<provided %s>]",
		value, resourcePathForm, resourcePathForm)
	binding := &goclientnew.Binding{DataType: "string"}
	keyed, rest := leadingKey(value)
	binding.Key, binding.AttributeName, _ = strings.Cut(keyed, ":")

	needed := strings.SplitN(rest, ":", 3)
	if len(needed) != 3 || needed[0] == "" || needed[1] == "" || needed[2] == "" {
		return nil, malformed
	}
	binding.NeededResource = &goclientnew.ResourceInfo{ResourceType: needed[0], ResourceName: needed[1]}
	neededPath, provided, err := splitSetExpression(needed[2])
	if err != nil {
		// No "=" follows the path, so only the needed place is given.
		binding.NeededPath = needed[2]
		return binding, nil
	}
	binding.NeededPath = neededPath

	if !strings.Contains(provided, ":") {
		// A data type alone, as an Insert link's binding has.
		if provided == "" {
			return nil, malformed
		}
		binding.DataType = provided
		return binding, nil
	}
	if strings.Count(provided, ":") >= 3 {
		dataType, after, _ := strings.Cut(provided, ":")
		binding.DataType, provided = dataType, after
	}
	resource, path, err := parseResourcePath("manual-binding", value, provided)
	if err != nil {
		return nil, malformed
	}
	binding.ProvidedResource, binding.ProvidedPath = resource, path
	return binding, nil
}

// parseNamedFunction reads [<name>=]<function line>. A name is there when an "=" comes before
// any whitespace, which the function's own name never has.
func parseNamedFunction(flag, value string, nameRequired bool) (string, *goclientnew.FunctionInvocation, error) {
	name, line := "", value
	if equals := strings.IndexByte(value, '='); equals >= 0 && !strings.ContainsAny(value[:equals], " \t") {
		name, line = value[:equals], value[equals+1:]
	}
	if nameRequired && name == "" {
		return "", nil, errors.Newf("--%s %q must be <name>=<function> [<argument>...]", flag, value)
	}
	invocation, err := parseFunctionLine(line)
	if err != nil {
		return "", nil, errors.Wrapf(err, "--%s %q", flag, value)
	}
	if invocation == nil {
		return "", nil, errors.Newf("--%s %q names no function", flag, value)
	}
	return name, invocation, nil
}

// queueLinkElementEdits adds the edits the link's list flags ask for to the ones the command's
// write will make. A flag that is given states its whole list, so it works in a patch as well.
func queueLinkElementEdits() error {
	queue := func(flag, path string, count int, elements any) error {
		if count == 0 {
			return nil
		}
		edit, err := replaceListEdit("--"+flag, path, elements)
		if err != nil {
			return err
		}
		pendingFieldEdits = append(pendingFieldEdits, edit)
		return nil
	}

	upstreamPaths := make([]goclientnew.NamedPath, 0, len(linkElementArgs.upstreamPaths))
	for _, value := range linkElementArgs.upstreamPaths {
		element, err := parseUpstreamPath(value)
		if err != nil {
			return err
		}
		upstreamPaths = append(upstreamPaths, *element)
	}
	downstreamPaths := make([]goclientnew.PathExpression, 0, len(linkElementArgs.downstreamPaths))
	for _, value := range linkElementArgs.downstreamPaths {
		element, err := parseDownstreamPath(value)
		if err != nil {
			return err
		}
		downstreamPaths = append(downstreamPaths, *element)
	}
	manualBindings := make([]goclientnew.Binding, 0, len(linkElementArgs.manualBindings))
	for _, value := range linkElementArgs.manualBindings {
		element, err := parseManualBinding(value)
		if err != nil {
			return err
		}
		manualBindings = append(manualBindings, *element)
	}
	upstreamGetters := make([]goclientnew.NamedFunctionResult, 0, len(linkElementArgs.upstreamGetters))
	for _, value := range linkElementArgs.upstreamGetters {
		name, invocation, err := parseNamedFunction("upstream-getter", value, true)
		if err != nil {
			return err
		}
		upstreamGetters = append(upstreamGetters, goclientnew.NamedFunctionResult{Name: name, FunctionInvocation: invocation})
	}
	downstreamSetters := make([]goclientnew.ParameterizedFunction, 0, len(linkElementArgs.downstreamSetters))
	for _, value := range linkElementArgs.downstreamSetters {
		key, invocation, err := parseNamedFunction("downstream-setter", value, false)
		if err != nil {
			return err
		}
		downstreamSetters = append(downstreamSetters, goclientnew.ParameterizedFunction{
			Key: key, FunctionInvocation: invocation, Parameters: referencedParameters(functionArgumentTexts(invocation)...)})
	}

	for _, list := range []struct {
		flag, path string
		count      int
		elements   any
	}{
		{"upstream-path", "UpstreamPaths", len(upstreamPaths), upstreamPaths},
		{"upstream-getter", "UpstreamGetters", len(upstreamGetters), upstreamGetters},
		{"downstream-path", "DownstreamPaths", len(downstreamPaths), downstreamPaths},
		{"downstream-setter", "DownstreamSetters", len(downstreamSetters), downstreamSetters},
		{"manual-binding", "ManualBindings", len(manualBindings), manualBindings},
	} {
		if err := queue(list.flag, list.path, list.count, list.elements); err != nil {
			return err
		}
	}
	return nil
}

// functionArgumentTexts are the argument values of a function invocation that are text.
func functionArgumentTexts(invocation *goclientnew.FunctionInvocation) []string {
	var texts []string
	for _, argument := range invocation.Arguments {
		if argument.Value == nil {
			continue
		}
		if text, err := argument.Value.AsFunctionArgumentValue0(); err == nil {
			texts = append(texts, text)
		}
	}
	return texts
}

// hasLinkElementFlags reports whether any of the link's list flags was given.
func hasLinkElementFlags() bool {
	return len(linkElementArgs.upstreamPaths)+len(linkElementArgs.downstreamPaths)+len(linkElementArgs.manualBindings)+
		len(linkElementArgs.upstreamGetters)+len(linkElementArgs.downstreamSetters) > 0
}
