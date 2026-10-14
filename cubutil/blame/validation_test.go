// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func failed(resourceType, resourceName, path, message string) goclientnew.AttributeValue {
	return goclientnew.AttributeValue{ResourceType: resourceType, ResourceName: resourceName, Path: path,
		Issues: []goclientnew.Issue{{Message: message}}}
}

func resultList(function string, passed bool, attributes ...goclientnew.AttributeValue) goclientnew.ValidationResultList {
	list := goclientnew.AttributeValueList(attributes)
	return goclientnew.ValidationResultList{{FunctionName: function, Passed: passed, FailedAttributes: &list}}
}

// TestBlameValidationsForPath: a failed check names a field by its merge key, by its position,
// or by a value containing it, and is a warning or an error by the gate it is in.
func TestBlameValidationsForPath(t *testing.T) {
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	const deployment, name = "apps/v1/Deployment", "prod/web"
	unit := &goclientnew.Unit{
		ValidationResults: map[string]goclientnew.ValidationResultList{
			"prod/no-placeholders/vet-placeholders": resultList("vet-placeholders", false,
				failed(deployment, name, "spec.template.spec.containers.?name=web.image", "placeholder")),
			"prod/replicas/vet-celexpr": resultList("vet-celexpr", false,
				failed(deployment, name, "spec.template.spec.containers.0.image", "positional")),
			"prod/whole-container/vet-celexpr": resultList("vet-celexpr", false,
				failed(deployment, name, "spec.template.spec.containers.?name=web", "ancestor")),
			"prod/other-resource/vet-celexpr": resultList("vet-celexpr", false,
				failed("v1/Service", name, "spec.template.spec.containers.?name=web.image", "other resource")),
			"prod/passed/vet-celexpr": resultList("vet-celexpr", true,
				failed(deployment, name, "spec.template.spec.containers.?name=web.image", "passed")),
			"prod/not-a-gate/vet-celexpr": resultList("vet-celexpr", false,
				failed(deployment, name, "spec.template.spec.containers.?name=web.image", "no gate")),
		},
		ValidationErrors: map[string]bool{
			"prod/no-placeholders/vet-placeholders": true,
			"prod/whole-container/vet-celexpr":      true,
			"prod/other-resource/vet-celexpr":       true,
			"prod/passed/vet-celexpr":               true,
		},
		ValidationWarnings: map[string]bool{"prod/replicas/vet-celexpr": true},
	}
	got := blameValidationsForPath(identifier, unit, deployment, name, "web",
		"spec.template.spec.containers.?name=web.image", "spec.template.spec.containers.?name=web;@0.image")

	var messages []string
	for _, v := range got {
		messages = append(messages, v.Issues[0].Message)
	}
	if strings.Join(messages, ",") != "placeholder,positional,ancestor" {
		t.Fatalf("matched %v, want placeholder,positional,ancestor", messages)
	}
	if got[1].Warning != true || got[0].Warning || got[2].Warning {
		t.Errorf("only the gate in ValidationWarnings is a warning: %+v", got)
	}
	if got[0].Path != "" || got[1].Path != "spec.template.spec.containers.0.image" || got[2].Path != "spec.template.spec.containers.?name=web" {
		t.Errorf("Path is set only where the result named the field another way: %q %q %q", got[0].Path, got[1].Path, got[2].Path)
	}

	f := &Field{Validation: got}
	if flags := blameFlags(f, 0, false); flags != "EW" {
		t.Errorf("flags = %q, want EW", flags)
	}
	if text := FormatValidation(got[1]); text != "warning prod/replicas/vet-celexpr at spec.template.spec.containers.0.image: positional" {
		t.Errorf("FormatValidation = %q", text)
	}

	// A CEL result names its attributes without issues of their own, and says what is wrong
	// in the result's details.
	list := goclientnew.AttributeValueList{{ResourceType: deployment, ResourceName: name, Path: "spec.replicas"}}
	cel := &goclientnew.Unit{
		ValidationResults: map[string]goclientnew.ValidationResultList{"prod/cel/vet-cel": {{
			FunctionName: "vet-cel", Details: []string{"replicas must be more than 1"}, FailedAttributes: &list}}},
		ValidationErrors: map[string]bool{"prod/cel/vet-cel": true},
	}
	celGot := blameValidationsForPath(identifier, cel, deployment, name, "web", "spec.replicas", "spec.replicas")
	if len(celGot) != 1 || FormatValidation(celGot[0]) != "error prod/cel/vet-cel: replicas must be more than 1" {
		t.Errorf("CEL result: %+v", celGot)
	}

	// With no message at all, the value the function found is what it said.
	found := Validation{Gate: "prod/p/vet-placeholders", Value: "confighubplaceholder"}
	if text := FormatValidation(found); text != `error prod/p/vet-placeholders: found "confighubplaceholder"` {
		t.Errorf("FormatValidation = %q", text)
	}
}

func TestPositionalPath(t *testing.T) {
	for stored, want := range map[string]string{
		"spec.template.spec.containers.?name=web;@0.image":                "spec.template.spec.containers.0.image",
		"spec.template.spec.containers.?name=web;@1.env.?name=A;@2.value": "spec.template.spec.containers.1.env.2.value",
		"spec.template.spec.containers.?name=web.image":                   "spec.template.spec.containers.?name=web.image",
		"spec.replicas": "spec.replicas",
	} {
		if got := positionalPath(stored); got != want {
			t.Errorf("positionalPath(%q) = %q, want %q", stored, got, want)
		}
	}
}
