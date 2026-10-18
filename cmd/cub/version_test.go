// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/cubapi"
)

// A server that cannot be reached leaves cub unusable against it, so cub version
// reports why and fails after printing the client version, while --client never
// contacts the server at all.
func TestVersionWithUnreachableServer(t *testing.T) {
	newKeyTestContextManager(t)
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	client, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: url})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	savedClient := cubClientNew
	t.Cleanup(func() { cubClientNew = savedClient; versionClientOnly = false })

	cubClientNew = client.API
	versionClientOnly = false
	err = versionCmdRun(versionCmd, nil)
	if err == nil {
		t.Fatal("cub version succeeded against an unreachable server")
	}
	for _, want := range []string{"could not fetch the server version", "cub version --client"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	cubClientNew = nil
	versionClientOnly = true
	if err := versionCmdRun(versionCmd, nil); err != nil {
		t.Errorf("cub version --client: %v", err)
	}
}
