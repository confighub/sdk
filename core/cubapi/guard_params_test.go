// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A Change without guards or clearance sends none of the parameters, and one with them sends
// each as JSON, the form every endpoint that writes a Unit's configuration reads them in.
func TestChangeGuardParams(t *testing.T) {
	gp, err := Change{}.GuardParams()
	if err != nil || gp.Guards != nil || gp.InheritedGuards != nil || gp.Clearance != nil {
		t.Fatalf("empty Change sent guard parameters: %+v %v", gp, err)
	}
	gp, err = Change{
		Guards:          goclientnew.GuardStamp{"Override": "External"},
		InheritedGuards: goclientnew.GuardStamp{"Policy": "PSS"},
		Clearance:       goclientnew.Clearance{{Key: "Link", Operator: "Exists"}},
	}.GuardParams()
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]*string{
		`{"Override":"External"}`:              gp.Guards,
		`{"Policy":"PSS"}`:                     gp.InheritedGuards,
		`[{"Key":"Link","Operator":"Exists"}]`: gp.Clearance,
	} {
		if got == nil || *got != name {
			t.Errorf("got %v, want %s", got, name)
		}
	}
}
