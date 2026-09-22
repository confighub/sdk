// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestHandleSelectParameterOutputFormats(t *testing.T) {
	autoSelect := func() string { return "SpaceID,Slug" }

	tests := []struct {
		name         string
		outputFormat string
		selectParam  string
		globalSelect string
		want         string
	}{
		{name: "default output auto-selects", want: "SpaceID,Slug"},
		{name: "wide output auto-selects", outputFormat: "wide", want: "SpaceID,Slug"},
		{name: "name output auto-selects", outputFormat: "name", want: "SpaceID,Slug"},
		{name: "custom-columns auto-selects", outputFormat: "custom-columns=Slug", want: "SpaceID,Slug"},
		{name: "json selects all fields", outputFormat: "json", want: ""},
		{name: "yaml selects all fields", outputFormat: "yaml", want: ""},
		{name: "jq selects all fields", outputFormat: "jq=.[].Slug", want: ""},
		{name: "yq selects all fields", outputFormat: "yq=.[].Slug", want: ""},
		{name: "explicit select wins over json", outputFormat: "json", selectParam: "Slug", want: "Slug"},
		{name: "global select wins over json", outputFormat: "json", globalSelect: "Slug", want: "Slug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func(saved string) { outputFormat = saved }(outputFormat)
			outputFormat = tt.outputFormat

			if got := handleSelectParameter(tt.selectParam, tt.globalSelect, autoSelect); got != tt.want {
				t.Errorf("handleSelectParameter() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Commands without an auto-select function already request all fields.
func TestHandleSelectParameterNoAutoSelectFunc(t *testing.T) {
	defer func(saved string) { outputFormat = saved }(outputFormat)
	outputFormat = "json"

	if got := handleSelectParameter("", "", nil); got != "" {
		t.Errorf("handleSelectParameter() = %q, want %q", got, "")
	}
}

// A label or annotation key routinely contains dots -- "app.kubernetes.io/name" is the
// Kubernetes convention -- and the column path is split on dots, so the key has to be
// rejoined to be found. Both spellings of the path have to work: the bare one and the one
// qualified with the entity name.
func TestGetValueDottedMapKeys(t *testing.T) {
	unit := &goclientnew.ExtendedUnit{Unit: &goclientnew.Unit{
		Labels:      map[string]string{"tier": "backend", "app.kubernetes.io/name": "web"},
		Annotations: map[string]string{"owner": "sre", "confighub.com/managed-by": "cub"},
	}}
	provider := NewDynamicColumnProvider(new(goclientnew.ExtendedUnit))

	tests := map[string]string{
		"Labels.tier":                               "backend",
		"Unit.Labels.tier":                          "backend",
		"Labels.app.kubernetes.io/name":             "web",
		"Unit.Labels.app.kubernetes.io/name":        "web",
		"Annotations.owner":                         "sre",
		"Annotations.confighub.com/managed-by":      "cub",
		"Unit.Annotations.confighub.com/managed-by": "cub",
		// A key that is absent is an empty cell, not a missing field.
		"Labels.app.kubernetes.io/version": "",
	}
	for col, want := range tests {
		if got := provider.GetValue(unit, col); got != want {
			t.Errorf("GetValue(%q) = %q, want %q", col, got, want)
		}
	}
}

// The column name a dotted key produces names the key, and the field selected from the
// server is the map itself -- the parts after "Labels." are a key, not fields.
func TestDottedMapKeyColumnNameAndSelect(t *testing.T) {
	provider := NewDynamicColumnProvider(new(goclientnew.ExtendedUnit))
	const col = "Unit.Labels.app.kubernetes.io/name"
	if got, want := columnToHeader(provider, col), "Label:app.kubernetes.io/name"; got != want {
		t.Errorf("columnToHeader(%q) = %q, want %q", col, got, want)
	}
	selected := strings.Split(buildSelectList("Unit", []string{col}, "", nil, nil, nil, nil), ",")
	if len(selected) != 1 || selected[0] != "Labels" {
		t.Errorf("buildSelectList for %q selected %v, want [Labels]", col, selected)
	}
}

// lookupMapKey prefers the longest key, so a path that continues past a dotted key into
// its value still navigates.
func TestLookupMapKeyPrefersLongestKey(t *testing.T) {
	m := reflect.ValueOf(map[string]string{"a": "short", "a.b": "long"})
	for _, tt := range []struct {
		parts    []string
		want     string
		consumed int
	}{
		{parts: []string{"a", "b"}, want: "long", consumed: 2},
		{parts: []string{"a"}, want: "short", consumed: 1},
		{parts: []string{"a", "c"}, want: "short", consumed: 1},
		{parts: []string{"z"}, want: "", consumed: 0},
	} {
		value, consumed := lookupMapKey(m, tt.parts)
		got := ""
		if value.IsValid() {
			got = value.String()
		}
		if got != tt.want || consumed != tt.consumed {
			t.Errorf("lookupMapKey(%v) = %q, %d; want %q, %d", tt.parts, got, consumed, tt.want, tt.consumed)
		}
	}
}
