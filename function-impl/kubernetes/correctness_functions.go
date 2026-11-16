// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

// vet-correctness: what the API server would reject, or accept and silently ignore, that the
// resource types' schemas do not express. OpenAPI schemas carry types, enums and required
// fields, but not label syntax, the size of a ConfigMap, or one field's agreement with another.
// The checks live in one function so that more can be added without a Trigger or a CEL
// expression each. Each finding is a FailedAttribute whose Issue names its check.

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
	// The server's image may have no zoneinfo; a CronJob's timeZone is checked against Go's copy.
	_ "time/tzdata"

	"github.com/cockroachdb/errors"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"

	"github.com/confighub/sdk/configkit/k8skit"
)

// The checks of vet-correctness, by the names their findings carry.
const (
	checkSelectorMatchesTemplate = "selector-matches-template"
	checkLabelSyntax             = "label-syntax"
	checkNameSyntax              = "name-syntax"
	checkPortNames               = "port-names"
	checkPortReferences          = "port-references"
	checkVolumeReferences        = "volume-references"
	checkExactlyOneOf            = "exactly-one-of"
	checkConfigMapSize           = "configmap-size"
	checkCronJob                 = "cronjob"
	checkDeprecatedFields        = "deprecated-fields"
	checkDeprecatedAPI           = "deprecated-api"
)

// correctnessCheckNames are vet-correctness's checks, which its check and exclude-check
// arguments name.
var correctnessCheckNames = []string{
	checkConfigMapSize, checkCronJob, checkDeprecatedAPI, checkDeprecatedFields, checkExactlyOneOf, checkLabelSyntax, checkNameSyntax,
	checkPortNames, checkPortReferences, checkSelectorMatchesTemplate, checkVolumeReferences,
}

// maxConfigMapSize is the most data a ConfigMap may hold, as the API server counts it: the
// values of data and the decoded values of binaryData.
const maxConfigMapSize = 1024 * 1024

// correctnessFinding is one thing a check found wrong with a resource, at path.
type correctnessFinding struct {
	check   string
	path    string
	value   any
	message string
}

// selectChecks is the checks to run given the check and exclude-check arguments, each a
// comma-separated list of names from all, in the order of all.
func selectChecks(all []string, include, exclude string) ([]string, error) {
	parse := func(param, list string) ([]string, error) {
		var names []string
		for _, name := range strings.Split(list, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !slices.Contains(all, name) {
				return nil, errors.Errorf("%s: unknown check %q; the checks are %s", param, name, strings.Join(all, ", "))
			}
			names = append(names, name)
		}
		return names, nil
	}
	included, err := parse("check", include)
	if err != nil {
		return nil, err
	}
	excluded, err := parse("exclude-check", exclude)
	if err != nil {
		return nil, err
	}
	if len(included) == 0 {
		included = all
	}
	var selected []string
	for _, name := range all {
		if slices.Contains(included, name) && !slices.Contains(excluded, name) {
			selected = append(selected, name)
		}
	}
	return selected, nil
}

func registerCorrectnessFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	if err := fh.RegisterFunction("vet-correctness", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-correctness",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "check",
					Required:      false,
					Description:   "Comma-separated names of the checks to run. Omitted, all of them. The checks are " + strings.Join(correctnessCheckNames, ", "),
					DataType:      api.DataTypeCSV,
				},
				{
					ParameterName: "exclude-check",
					Required:      false,
					Description:   "Comma-separated names of checks not to run",
					DataType:      api.DataTypeCSV,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no resource fails a check, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:   false,
			Validating: true,
			Hermetic:   true,
			Idempotent: true,
			Description: "Fails a resource the Kubernetes API server would reject, or accept and silently ignore part of, for reasons the schemas do not " +
				"express: " + checkSelectorMatchesTemplate + " (a controller's selector selects its own pod template's labels), " +
				checkLabelSyntax + " (label keys and values are valid), " + checkNameSyntax + " (metadata.name has its type's " +
				"syntax), " + checkPortNames + " (container port names are valid and unique in the container), " +
				checkPortReferences + " (a probe or lifecycle handler naming a port names one its container declares), " +
				checkVolumeReferences + " (each volume mount and device names a volume of the pod), " + checkExactlyOneOf +
				" (one field of each required group is set, such as a PodDisruptionBudget's minAvailable or maxUnavailable), " +
				checkConfigMapSize + " (a ConfigMap's data is at most 1 MiB), " + checkCronJob + " (a CronJob's schedule " +
				"parses, its timeZone exists, and its pods restart OnFailure or Never), " + checkDeprecatedFields +
				" (no superseded field is set without the field replacing it, or with a different value), and " +
				checkDeprecatedAPI + " (the resource's type, or the version of it, is not deprecated). Name formats, " +
				"required groups, and deprecated fields and types are declared per resource type, a CRD's deprecated " +
				"versions included. Each failure is an Issue identified by its check. Fix: what the Issue says, for the check it names.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var include, exclude string
			for _, arg := range fArgs.Arguments {
				s, _ := arg.Value.(string)
				switch arg.ParameterName {
				case "check":
					include = s
				case "exclude-check":
					exclude = s
				}
			}
			selected, err := selectChecks(correctnessCheckNames, include, exclude)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			return k8sFnVetCorrectness(rp, fArgs.ParsedData, selected, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func k8sFnVetCorrectness(rp yamlkit.ResourceProvider, parsedData gaby.Container, selected []string, options *api.FunctionOptions) (gaby.Container, any, error) {
	result := api.ValidationResult{Passed: true}
	_, err := yamlkit.VisitResourcesFiltered(parsedData, &result, rp, options, func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		vr := output.(*api.ValidationResult)
		resource, ok := doc.Data().(map[string]any)
		if !ok {
			return vr, nil
		}
		for _, f := range correctnessFindings(resourceInfo.ResourceType, resource, selected) {
			vr.Passed = false
			vr.FailedAttributes = append(vr.FailedAttributes, api.AttributeValue{
				AttributeInfo: api.AttributeInfo{
					AttributeIdentifier: api.AttributeIdentifier{ResourceInfo: *resourceInfo, Path: api.ResolvedPath(f.path)},
					AttributeMetadata:   api.AttributeMetadata{AttributeName: api.AttributeNameNone},
				},
				Value:  f.value,
				Issues: []api.Issue{{Identifier: f.check, Message: f.message}},
			})
		}
		return vr, nil
	})
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, result, nil
}

// correctnessFindings runs the selected checks on one resource of resourceType.
func correctnessFindings(resourceType api.ResourceType, resource map[string]any, selected []string) []correctnessFinding {
	var findings []correctnessFinding
	for _, check := range selected {
		switch check {
		case checkSelectorMatchesTemplate:
			findings = append(findings, selectorMatchesTemplate(resourceType, resource)...)
		case checkLabelSyntax:
			findings = append(findings, labelSyntax(resourceType, resource)...)
		case checkNameSyntax:
			findings = append(findings, nameSyntax(resourceType, resource)...)
		case checkPortNames:
			findings = append(findings, portNames(resourceType, resource)...)
		case checkPortReferences:
			findings = append(findings, portReferences(resourceType, resource)...)
		case checkVolumeReferences:
			findings = append(findings, volumeReferences(resourceType, resource)...)
		case checkExactlyOneOf:
			findings = append(findings, exactlyOneOf(resourceType, resource)...)
		case checkDeprecatedFields:
			findings = append(findings, deprecatedFields(resourceType, resource)...)
		case checkDeprecatedAPI:
			findings = append(findings, deprecatedAPI(resourceType)...)
		case checkConfigMapSize:
			if resourceType == "v1/ConfigMap" {
				findings = append(findings, configMapSize(resource)...)
			}
		case checkCronJob:
			if resourceType == "batch/v1/CronJob" {
				findings = append(findings, cronJob(resource)...)
			}
		}
	}
	return findings
}

// lookup is the value at path in obj, a dot path whose segments escape a dot as ~1, and whether
// there is one.
func lookup(obj any, path string) (any, bool) {
	for _, segment := range gaby.DotPathToSlice(path) {
		switch o := obj.(type) {
		case map[string]any:
			v, ok := o[segment]
			if !ok {
				return nil, false
			}
			obj = v
		case []any:
			i, err := strconv.Atoi(segment)
			if err != nil || i < 0 || i >= len(o) {
				return nil, false
			}
			obj = o[i]
		default:
			return nil, false
		}
	}
	return obj, true
}

// match is a value found at a path that may hold wildcards, with the path resolved, and what each
// wildcard matched, in order.
type match struct {
	path     string
	value    any
	wildcard []string
}

// lookupAll is every value at path in obj, a dot path in which * matches any element of a list or
// any key of a map.
func lookupAll(obj any, path string) []match {
	var out []match
	var walk func(obj any, segments []string, resolved []string, wildcard []string)
	walk = func(obj any, segments []string, resolved []string, wildcard []string) {
		if len(segments) == 0 {
			out = append(out, match{path: dotPath(resolved), value: obj, wildcard: slices.Clone(wildcard)})
			return
		}
		segment, rest := segments[0], segments[1:]
		switch o := obj.(type) {
		case map[string]any:
			if segment == "*" {
				for _, key := range sortedKeys(o) {
					walk(o[key], rest, append(resolved, key), append(wildcard, key))
				}
			} else if v, ok := o[segment]; ok {
				walk(v, rest, append(resolved, segment), wildcard)
			}
		case []any:
			if segment == "*" {
				for i, v := range o {
					walk(v, rest, append(resolved, strconv.Itoa(i)), append(wildcard, strconv.Itoa(i)))
				}
			} else if i, err := strconv.Atoi(segment); err == nil && i >= 0 && i < len(o) {
				walk(o[i], rest, append(resolved, segment), wildcard)
			}
		}
	}
	walk(obj, gaby.DotPathToSlice(path), nil, nil)
	return out
}

// dotPath joins path segments into a dot path, escaping the dots within them.
func dotPath(segments []string) string {
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = yamlkit.EscapeDotsInPathSegment(segment)
	}
	return strings.Join(escaped, ".")
}

// fillWildcards replaces the wildcards of path, in order, with the values matched.
func fillWildcards(path string, matched []string) string {
	segments := gaby.DotPathToSlice(path)
	next := 0
	for i, segment := range segments {
		if segment == "*" && next < len(matched) {
			segments[i] = matched[next]
			next++
		}
	}
	return dotPath(segments)
}

// keyPath is the path of key in the map at prefix, with a dot in the key escaped.
func keyPath(prefix, key string) string {
	return prefix + "." + strings.ReplaceAll(key, ".", "~1")
}

// selectorMatchesTemplate fails a resource whose own-pod selector does not select the labels of
// its pod template, which the API server rejects for every controller with a selector. The
// paths are the pod-label-selector and pod-template-labels attributes of its type. A matchLabels
// selector's matchExpressions, beside it, are evaluated too.
func selectorMatchesTemplate(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	selectors := k8skit.DeclaredAttributePaths(k8skit.AttributeNamePodLabelSelector)[resourceType]
	templates := k8skit.DeclaredAttributePaths(k8skit.AttributeNamePodTemplateLabels)[resourceType]
	if len(selectors) != 1 || len(templates) != 1 {
		return nil
	}
	// A selector may hold matchExpressions alone, so the selector holding matchLabels is read
	// whether or not matchLabels is there.
	parent, hasParent := strings.CutSuffix(selectors[0], ".matchLabels")
	selector, hasSelector := lookup(resource, selectors[0])
	var expressions any
	if hasParent {
		expressions, _ = lookup(resource, parent+".matchExpressions")
	}
	if (!hasSelector || selector == nil) && expressions == nil {
		return nil
	}
	labels, _ := lookup(resource, templates[0])
	labelMap, _ := labels.(map[string]any)
	var findings []correctnessFinding
	matchLabels, _ := selector.(map[string]any)
	keys := sortedKeys(matchLabels)
	for _, key := range keys {
		want := fmt.Sprint(matchLabels[key])
		if got, ok := labelMap[key]; !ok || fmt.Sprint(got) != want {
			findings = append(findings, correctnessFinding{
				check: checkSelectorMatchesTemplate, path: keyPath(selectors[0], key), value: want,
				message: fmt.Sprintf("the selector requires %s=%s, which the pod template's labels at %s do not have, so the API server rejects it", key, want, templates[0]),
			})
		}
	}
	// matchExpressions sit beside matchLabels.
	if hasParent {
		list, _ := expressions.([]any)
		for i, e := range list {
			expression, _ := e.(map[string]any)
			if expression == nil || selectsLabels(expression, labelMap) {
				continue
			}
			findings = append(findings, correctnessFinding{
				check: checkSelectorMatchesTemplate, path: fmt.Sprintf("%s.matchExpressions.%d", parent, i), value: nil,
				message: fmt.Sprintf("matchExpressions %d does not select the pod template's labels at %s, so the API server rejects it", i, templates[0]),
			})
		}
	}
	return findings
}

// selectsLabels is whether a label selector requirement selects labels. An operator it does not
// know selects them, so that it is not reported.
func selectsLabels(requirement map[string]any, labels map[string]any) bool {
	key := fmt.Sprint(requirement["key"])
	value, has := labels[key]
	var values []string
	if list, ok := requirement["values"].([]any); ok {
		for _, v := range list {
			values = append(values, fmt.Sprint(v))
		}
	}
	switch requirement["operator"] {
	case "In":
		return has && slices.Contains(values, fmt.Sprint(value))
	case "NotIn":
		return !has || !slices.Contains(values, fmt.Sprint(value))
	case "Exists":
		return has
	case "DoesNotExist":
		return !has
	}
	return true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// labelSyntax fails a label key that is not a qualified name, or a value longer than 63
// characters or with other characters than alphanumerics, '-', '_' and '.', on the resource and
// on the pods it produces.
func labelSyntax(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	paths := append([]string{"metadata.labels"}, k8skit.DeclaredAttributePaths(k8skit.AttributeNamePodTemplateLabels)[resourceType]...)
	var findings []correctnessFinding
	for _, path := range slices.Compact(paths) {
		labels, _ := lookup(resource, path)
		labelMap, _ := labels.(map[string]any)
		for _, key := range sortedKeys(labelMap) {
			if errs := validation.IsQualifiedName(key); len(errs) > 0 {
				findings = append(findings, correctnessFinding{
					check: checkLabelSyntax, path: keyPath(path, key), value: key,
					message: fmt.Sprintf("label key %q is invalid: %s", key, strings.Join(errs, "; ")),
				})
			}
			value, isString := labelMap[key].(string)
			if !isString {
				findings = append(findings, correctnessFinding{
					check: checkLabelSyntax, path: keyPath(path, key), value: fmt.Sprint(labelMap[key]),
					message: fmt.Sprintf("label %s is not a string; quote it", key),
				})
				continue
			}
			if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
				findings = append(findings, correctnessFinding{
					check: checkLabelSyntax, path: keyPath(path, key), value: value,
					message: fmt.Sprintf("label %s has an invalid value %q: %s", key, value, strings.Join(errs, "; ")),
				})
			}
		}
	}
	return findings
}

// configMapSize fails a ConfigMap holding more than the API server accepts.
func configMapSize(resource map[string]any) []correctnessFinding {
	total := 0
	if data, ok := resource["data"].(map[string]any); ok {
		for _, v := range data {
			total += len(fmt.Sprint(v))
		}
	}
	if binary, ok := resource["binaryData"].(map[string]any); ok {
		for _, v := range binary {
			s := fmt.Sprint(v)
			if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
				total += len(decoded)
			} else {
				total += len(s)
			}
		}
	}
	if total <= maxConfigMapSize {
		return nil
	}
	return []correctnessFinding{{
		check: checkConfigMapSize, path: "data", value: total,
		message: fmt.Sprintf("the ConfigMap holds %d bytes of data, more than the %d the API server accepts", total, maxConfigMapSize),
	}}
}

// cronJob fails a CronJob whose schedule does not parse, whose timeZone is not one Go knows, or
// whose pods restart Always, which a Job's may not.
func cronJob(resource map[string]any) []correctnessFinding {
	var findings []correctnessFinding
	if schedule, ok := lookup(resource, "spec.schedule"); ok {
		s := fmt.Sprint(schedule)
		if err := parseCronSchedule(s); err != nil {
			findings = append(findings, correctnessFinding{
				check: checkCronJob, path: "spec.schedule", value: s,
				message: fmt.Sprintf("the schedule %q does not parse: %v", s, err),
			})
		}
	}
	if zone, ok := lookup(resource, "spec.timeZone"); ok && zone != nil {
		z := fmt.Sprint(zone)
		if _, err := time.LoadLocation(z); err != nil || z == "" || strings.EqualFold(z, "local") {
			findings = append(findings, correctnessFinding{
				check: checkCronJob, path: "spec.timeZone", value: z,
				message: fmt.Sprintf("the time zone %q is not in the IANA time zone database", z),
			})
		}
	}
	const restartPolicy = "spec.jobTemplate.spec.template.spec.restartPolicy"
	policy, ok := lookup(resource, restartPolicy)
	if p := fmt.Sprint(policy); !ok || policy == nil || (p != "OnFailure" && p != "Never") {
		var value any
		message := "the pod template sets no restartPolicy, which defaults to Always; a Job's pods restart OnFailure or Never"
		if ok && policy != nil {
			value = p
			message = fmt.Sprintf("the pod template's restartPolicy is %s; a Job's pods restart OnFailure or Never", p)
		}
		findings = append(findings, correctnessFinding{check: checkCronJob, path: restartPolicy, value: value, message: message})
	}
	return findings
}

// cronField is the range of one field of a standard cron schedule, and the names it accepts.
type cronField struct {
	name     string
	min, max int
	names    []string // names[i] is the value min+i
}

var cronFields = []cronField{
	{name: "minute", min: 0, max: 59},
	{name: "hour", min: 0, max: 23},
	{name: "day of month", min: 1, max: 31},
	{name: "month", min: 1, max: 12, names: []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}},
	{name: "day of week", min: 0, max: 6, names: []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}},
}

// cronDescriptors are the schedules a CronJob accepts in place of five fields.
var cronDescriptors = []string{"@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly"}

// parseCronSchedule checks a CronJob schedule as the API server parses it: five fields, or a
// descriptor such as @daily. A time zone belongs in spec.timeZone, and the API server rejects
// one in the schedule.
func parseCronSchedule(schedule string) error {
	schedule = strings.TrimSpace(schedule)
	if strings.Contains(schedule, "TZ") {
		return errors.New("a time zone belongs in spec.timeZone, not in the schedule")
	}
	if strings.HasPrefix(schedule, "@") {
		if slices.Contains(cronDescriptors, schedule) {
			return nil
		}
		if every, ok := strings.CutPrefix(schedule, "@every "); ok {
			_, err := time.ParseDuration(every)
			return err
		}
		return errors.Errorf("%s is not a descriptor; the descriptors are %s", schedule, strings.Join(cronDescriptors, ", "))
	}
	fields := strings.Fields(schedule)
	if len(fields) != len(cronFields) {
		return errors.Errorf("it has %d fields, not 5 (minute hour day-of-month month day-of-week)", len(fields))
	}
	for i, field := range fields {
		if err := parseCronField(field, cronFields[i]); err != nil {
			return errors.Wrapf(err, "the %s field %q", cronFields[i].name, field)
		}
	}
	return nil
}

// parseCronField checks one field: a comma-separated list of *, ?, a value, or a range, each
// optionally with a /step.
func parseCronField(field string, f cronField) error {
	value := func(s string) (int, error) {
		for i, name := range f.names {
			if strings.EqualFold(s, name) {
				return f.min + i, nil
			}
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, errors.Errorf("%q is not a number", s)
		}
		// Sunday is 0 and only 0: the parser the API server uses refuses 7.
		if n < f.min || n > f.max {
			return 0, errors.Errorf("%d is outside %d-%d", n, f.min, f.max)
		}
		return n, nil
	}
	for _, item := range strings.Split(field, ",") {
		base, step, hasStep := strings.Cut(item, "/")
		if hasStep {
			if n, err := strconv.Atoi(step); err != nil || n <= 0 {
				return errors.Errorf("the step %q is not a positive number", step)
			}
		}
		switch {
		case base == "*" || base == "?":
		case strings.Contains(base, "-"):
			lo, hi, _ := strings.Cut(base, "-")
			l, err := value(lo)
			if err != nil {
				return err
			}
			h, err := value(hi)
			if err != nil {
				return err
			}
			if l > h {
				return errors.Errorf("the range %s runs backwards", base)
			}
		default:
			if _, err := value(base); err != nil {
				return err
			}
		}
	}
	return nil
}

// nameSyntax fails a metadata.name the API server would reject for the resource's type. A name
// holding a placeholder is not checked.
func nameSyntax(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	name, ok := lookup(resource, "metadata.name")
	s, isString := name.(string)
	if !ok || !isString || s == "" || strings.Contains(s, yamlkit.PlaceHolderBlockApplyString) {
		return nil
	}
	format := k8skit.NameFormatOf(resourceType)
	var errs []string
	switch format {
	case k8skit.NameFormatDNS1123Label:
		errs = validation.IsDNS1123Label(s)
	case k8skit.NameFormatDNS1035Label:
		errs = validation.IsDNS1035Label(s)
	case k8skit.NameFormatPathSegment:
		errs = isPathSegmentName(s)
	default:
		errs = validation.IsDNS1123Subdomain(s)
	}
	if len(errs) == 0 {
		return nil
	}
	return []correctnessFinding{{
		check: checkNameSyntax, path: "metadata.name", value: s,
		message: fmt.Sprintf("the name %q is not a valid %s, which a %s's name has to be: %s", s, format, resourceType, strings.Join(errs, "; ")),
	}}
}

// isPathSegmentName is what the API server says is wrong with a name that has to be usable as one
// segment of a URL path: an RBAC role's or binding's.
func isPathSegmentName(name string) []string {
	var errs []string
	if name == "." || name == ".." {
		errs = append(errs, fmt.Sprintf("may not be %q", name))
	}
	for _, illegal := range []string{"/", "%"} {
		if strings.Contains(name, illegal) {
			errs = append(errs, fmt.Sprintf("may not contain %q", illegal))
		}
	}
	return errs
}

// containerAt is a container of a resource, at path, in the pod spec at podSpec.
type containerAt struct {
	path      string
	podSpec   string
	container map[string]any
}

// containersOf is each container of a resource of resourceType, in every container array of every
// pod spec it carries. A container is at its name, as ConfigHub's functions report it, or at its
// index when it has none.
func containersOf(resourceType api.ResourceType, resource map[string]any) []containerAt {
	var out []containerAt
	podSpecs := k8skit.PodSpecPaths(resourceType)
	for _, arrayPath := range k8skit.ContainerArrayPaths(resourceType) {
		podSpec := ""
		for _, p := range podSpecs {
			if strings.HasPrefix(arrayPath, p+".") {
				podSpec = p
			}
		}
		list, _ := lookup(resource, arrayPath)
		containers, _ := list.([]any)
		for i, c := range containers {
			container, ok := c.(map[string]any)
			if !ok {
				continue
			}
			path := fmt.Sprintf("%s.%d", arrayPath, i)
			if name, ok := container["name"].(string); ok && name != "" && !strings.ContainsAny(name, ".=") {
				path = arrayPath + ".?name=" + name
			}
			out = append(out, containerAt{path: path, podSpec: podSpec, container: container})
		}
	}
	return out
}

// portNames fails a container port name that is not an IANA service name, or that the container
// gives two ports.
func portNames(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	var findings []correctnessFinding
	for _, c := range containersOf(resourceType, resource) {
		ports, _ := c.container["ports"].([]any)
		seen := map[string]bool{}
		for i, p := range ports {
			port, _ := p.(map[string]any)
			name, ok := port["name"].(string)
			if !ok || name == "" {
				continue
			}
			path := fmt.Sprintf("%s.ports.%d.name", c.path, i)
			if errs := validation.IsValidPortName(name); len(errs) > 0 {
				findings = append(findings, correctnessFinding{
					check: checkPortNames, path: path, value: name,
					message: fmt.Sprintf("the port name %q is invalid: %s", name, strings.Join(errs, "; ")),
				})
			}
			if seen[name] {
				findings = append(findings, correctnessFinding{
					check: checkPortNames, path: path, value: name,
					message: fmt.Sprintf("the container names two ports %q", name),
				})
			}
			seen[name] = true
		}
	}
	return findings
}

// portReferencePaths are where, in a container, a probe or lifecycle handler names a port.
var portReferencePaths = []string{
	"livenessProbe.httpGet.port", "livenessProbe.tcpSocket.port",
	"readinessProbe.httpGet.port", "readinessProbe.tcpSocket.port",
	"startupProbe.httpGet.port", "startupProbe.tcpSocket.port",
	"lifecycle.postStart.httpGet.port", "lifecycle.postStart.tcpSocket.port",
	"lifecycle.preStop.httpGet.port", "lifecycle.preStop.tcpSocket.port",
}

// portReferences fails a probe or lifecycle handler that names a port its container does not
// declare. The API server accepts it, and the kubelet cannot resolve it, so the probe fails. A
// port given by number need not be declared.
func portReferences(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	var findings []correctnessFinding
	for _, c := range containersOf(resourceType, resource) {
		declared := map[string]bool{}
		ports, _ := c.container["ports"].([]any)
		for _, p := range ports {
			if port, ok := p.(map[string]any); ok {
				if name, ok := port["name"].(string); ok {
					declared[name] = true
				}
			}
		}
		for _, refPath := range portReferencePaths {
			value, ok := lookup(c.container, refPath)
			name, isString := value.(string)
			if !ok || !isString || name == "" || declared[name] {
				continue
			}
			if _, err := strconv.Atoi(name); err == nil {
				continue
			}
			findings = append(findings, correctnessFinding{
				check: checkPortReferences, path: c.path + "." + refPath, value: name,
				message: fmt.Sprintf("%s names the port %q, which the container does not declare", refPath, name),
			})
		}
	}
	return findings
}

// volumeReferences fails a volume mount or device naming no volume of its pod, which the API
// server rejects.
func volumeReferences(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	volumes := map[string]map[string]bool{} // pod spec path -> its volumes' names
	for _, podSpec := range k8skit.PodSpecPaths(resourceType) {
		volumes[podSpec] = map[string]bool{}
		list, _ := lookup(resource, podSpec+".volumes")
		entries, _ := list.([]any)
		for _, v := range entries {
			if volume, ok := v.(map[string]any); ok {
				if name, ok := volume["name"].(string); ok {
					volumes[podSpec][name] = true
				}
			}
		}
	}
	var findings []correctnessFinding
	for _, c := range containersOf(resourceType, resource) {
		for _, field := range []string{"volumeMounts", "volumeDevices"} {
			list, _ := c.container[field].([]any)
			for i, m := range list {
				mount, _ := m.(map[string]any)
				name, ok := mount["name"].(string)
				if !ok || volumes[c.podSpec][name] {
					continue
				}
				findings = append(findings, correctnessFinding{
					check: checkVolumeReferences, path: fmt.Sprintf("%s.%s.%d.name", c.path, field, i), value: name,
					message: fmt.Sprintf("%s %d names the volume %q, which the pod does not have", field, i, name),
				})
			}
		}
	}
	return findings
}

// exactlyOneOf fails an object holding a required exclusive group with none of its members set.
// One with several set is a schema matter, and the API server's.
func exactlyOneOf(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	required := k8skit.RequiredExclusiveFields(resourceType)
	paths := make([]string, 0, len(required))
	for path := range required {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	var findings []correctnessFinding
	for _, path := range paths {
		group := required[path]
		for _, m := range lookupAll(resource, path) {
			object, ok := m.value.(map[string]any)
			if !ok {
				continue
			}
			if slices.ContainsFunc(group.Members, func(member string) bool { _, set := object[member]; return set }) {
				continue
			}
			findings = append(findings, correctnessFinding{
				check: checkExactlyOneOf, path: m.path, value: nil,
				message: fmt.Sprintf("%s sets none of %s; set one", m.path, strings.Join(group.Members, ", ")),
			})
		}
	}
	return findings
}

// deprecatedFields fails a field the API has superseded that is set without the field replacing
// it, or with a replacement holding something else: the API server rejects an Ingress whose class
// annotation and ingressClassName differ, and a pod's serviceAccountName silently overrides its
// serviceAccount.
func deprecatedFields(resourceType api.ResourceType, resource map[string]any) []correctnessFinding {
	var findings []correctnessFinding
	for _, declared := range k8skit.DeprecatedPaths(resourceType) {
		for _, m := range lookupAll(resource, declared.Path) {
			replacement := fillWildcards(declared.Replacement, m.wildcard)
			value := fmt.Sprint(m.value)
			replacedBy, replaced := lookup(resource, replacement)
			message := fmt.Sprintf("%s is deprecated; set %s instead", m.path, replacement)
			if replaced {
				if fmt.Sprint(replacedBy) == value {
					continue
				}
				message = fmt.Sprintf("%s is %q and %s, which replaces it, is %q; remove %s", m.path, value, replacement, fmt.Sprint(replacedBy), m.path)
			}
			findings = append(findings, correctnessFinding{check: checkDeprecatedFields, path: m.path, value: value, message: message})
		}
	}
	return findings
}

// deprecatedAPI fails a resource whose type, or the version of it, its spec declares deprecated,
// with the warning the spec gives.
func deprecatedAPI(resourceType api.ResourceType) []correctnessFinding {
	deprecated, warning := k8skit.DeprecationOf(resourceType)
	if !deprecated {
		return nil
	}
	if warning == "" {
		warning = string(resourceType) + " is deprecated"
	}
	return []correctnessFinding{{check: checkDeprecatedAPI, path: "apiVersion", value: string(resourceType), message: warning}}
}
