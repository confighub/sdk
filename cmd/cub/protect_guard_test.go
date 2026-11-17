// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestWithoutProtectGuard(t *testing.T) {
	stamp := func(kv ...string) *goclientnew.GuardStamp {
		s := goclientnew.GuardStamp{}
		for i := 0; i < len(kv); i += 2 {
			s[kv[i]] = kv[i+1]
		}
		return &s
	}
	in := func(key string, values ...string) goclientnew.ClearanceRequirement {
		return goclientnew.ClearanceRequirement{Key: key, Operator: "In", Values: values}
	}
	clearance := func(requirements ...goclientnew.ClearanceRequirement) *goclientnew.Clearance {
		c := goclientnew.Clearance(requirements)
		return &c
	}

	tests := []struct {
		name                      string
		guards                    *goclientnew.GuardStamp
		clearance                 *goclientnew.Clearance
		editGuards, editClearance bool
		wantGuards                *goclientnew.GuardStamp
		wantClearance             *goclientnew.Clearance
	}{
		{
			name:          "removes the guard and the clearance admitting it",
			guards:        stamp("Trigger", "stamp", "owner", "platform"),
			clearance:     clearance(in("Trigger", "stamp"), in("owner", "platform")),
			editGuards:    true,
			editClearance: true,
			wantGuards:    stamp("owner", "platform"),
			wantClearance: clearance(in("owner", "platform")),
		},
		{
			name:          "keeps a guard with another value and the other values cleared",
			guards:        stamp("Trigger", "other"),
			clearance:     clearance(in("Trigger", "stamp", "other")),
			editGuards:    true,
			editClearance: true,
			wantGuards:    stamp("Trigger", "other"),
			wantClearance: clearance(in("Trigger", "other")),
		},
		{
			name:          "leaves what the command was given explicitly",
			guards:        stamp("Trigger", "stamp"),
			clearance:     clearance(in("Trigger", "stamp")),
			wantGuards:    stamp("Trigger", "stamp"),
			wantClearance: clearance(in("Trigger", "stamp")),
		},
		{
			name:          "keeps an Exists clearance, which admits more than the guard",
			clearance:     clearance(goclientnew.ClearanceRequirement{Key: "Trigger", Operator: "Exists"}),
			editGuards:    true,
			editClearance: true,
			wantClearance: clearance(goclientnew.ClearanceRequirement{Key: "Trigger", Operator: "Exists"}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotGuards, gotClearance := withoutProtectGuard(tt.guards, tt.clearance, "Trigger", "stamp", tt.editGuards, tt.editClearance)
			if !reflect.DeepEqual(gotGuards, tt.wantGuards) {
				t.Errorf("guards = %v, want %v", gotGuards, tt.wantGuards)
			}
			if !reflect.DeepEqual(gotClearance, tt.wantClearance) {
				t.Errorf("clearance = %v, want %v", gotClearance, tt.wantClearance)
			}
		})
	}
}
