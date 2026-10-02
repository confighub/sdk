// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestParseClearanceSpecs(t *testing.T) {
	tests := []struct {
		spec string
		want goclientnew.ClearanceRequirement
	}{
		{"owner", goclientnew.ClearanceRequirement{Key: "owner", Operator: string(api.ClearanceOperatorExists)}},
		{"!policy-exception", goclientnew.ClearanceRequirement{Key: "policy-exception", Operator: string(api.ClearanceOperatorDoesNotExist)}},
		{"owner=platform", goclientnew.ClearanceRequirement{Key: "owner", Operator: string(api.ClearanceOperatorIn), Values: []string{"platform"}}},
		{"owner=platform;sre", goclientnew.ClearanceRequirement{Key: "owner", Operator: string(api.ClearanceOperatorIn), Values: []string{"platform", "sre"}}},
		{"owner!=platform; sre", goclientnew.ClearanceRequirement{Key: "owner", Operator: string(api.ClearanceOperatorNotIn), Values: []string{"platform", "sre"}}},
	}
	for _, test := range tests {
		t.Run(test.spec, func(t *testing.T) {
			clearance, err := parseClearanceSpecs([]string{test.spec})
			if err != nil {
				t.Fatal(err)
			}
			if len(clearance) != 1 || !reflect.DeepEqual(clearance[0], test.want) {
				t.Fatalf("got %+v, want %+v", clearance, test.want)
			}
			// What is displayed is what would be typed to set it again.
			again, err := parseClearanceSpecs(strings.Fields(formatClearance(&clearance)))
			if err != nil || !reflect.DeepEqual(again, clearance) {
				t.Errorf("%q does not parse back to %+v: %+v, %v", formatClearance(&clearance), clearance, again, err)
			}
		})
	}
}

// A comma is what separates the values of a repeatable flag, so it is not used inside one. A list
// written with commas is refused with the separator to use, since a guard value cannot hold one.
func TestParseClearanceSpecRefusesCommas(t *testing.T) {
	for _, spec := range []string{"owner=platform,sre", "owner!=platform,sre", "!owner,team"} {
		_, err := parseClearanceSpecs([]string{spec})
		if err == nil {
			t.Errorf("%q was accepted", spec)
		}
	}
	_, err := parseClearanceSpecs([]string{"owner=platform,sre"})
	if err == nil || !strings.Contains(err.Error(), `";"`) {
		t.Errorf("the error does not name the separator: %v", err)
	}
}
