// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A user given by UUID needs no lookup, so this runs without a server; a group or a service
// account is always looked up.
func TestPermissionUserByIDAndMalformedSubjects(t *testing.T) {
	const userID = "11111111-1111-1111-1111-111111111111"

	permissions := goclientnew.Permissions{}
	if err := parsePermissions([]string{"View:" + userID}, &permissions); err != nil {
		t.Fatal(err)
	}
	if got := permissions["View"].UserIDs; !reflect.DeepEqual(got, map[string]bool{userID: true}) {
		t.Errorf("UserIDs = %v", got)
	}

	patch := map[string]interface{}{}
	if err := parsePermissionsIntoPatchMap([]string{"-View:" + userID}, patch); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"View": map[string]interface{}{"UserIDs": map[string]interface{}{userID: nil}}}
	if !reflect.DeepEqual(patch, want) {
		t.Errorf("patch = %v, want %v", patch, want)
	}

	for permStr, wantErr := range map[string]string{
		"View":                 "serviceaccount:ServiceAccountSlugOrID",
		"View:serviceaccount:": "no service account after",
		"View:group:":          "no group after",
	} {
		if _, err := parsePermissionGrant(permStr); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: error %v, want one containing %q", permStr, err, wantErr)
		}
	}
}

// A group's members are named as in --permission, and only a service account can be one: these are
// refused before any lookup, so they run without a server.
func TestGroupMemberMustBeAServiceAccount(t *testing.T) {
	for member, wantErr := range map[string]string{
		"serviceaccount:":                      "no service account after",
		"group:platform":                       "a group cannot be a member of a group",
		"alice@example.com":                    "managed in the identity provider",
		"11111111-1111-1111-1111-111111111111": "managed in the identity provider",
	} {
		if _, err := groupMemberUserID(member); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: error %v, want one containing %q", member, err, wantErr)
		}
	}
}
