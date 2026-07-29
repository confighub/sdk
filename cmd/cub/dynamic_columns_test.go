// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

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
