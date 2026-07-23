// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestClusterOCIEndpoint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://localhost:9090", "oci://localhost:9092"},
		{"http://127.0.0.1:9090", "oci://localhost:9092"},
		{"https://hub.confighub.com", "oci://oci.hub.confighub.com:443"},
		{"https://hub.confighub.com:443", "oci://oci.hub.confighub.com:443"},
		{"https://pr-3415.testhub.confighub.net", "oci://oci.pr-3415.testhub.confighub.net:443"},
	}
	for _, c := range cases {
		got, err := clusterOCIEndpoint(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("clusterOCIEndpoint(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClusterOCIInsecure(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://localhost:9090", true},
		{"http://127.0.0.1:9090", true},
		{"http://my-internal-cub:9090", true},
		{"HTTP://localhost:9090", true},
		{"https://hub.confighub.com", false},
		{"https://hub.confighub.com:443", false},
	}
	for _, c := range cases {
		got, err := clusterOCIInsecure(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("clusterOCIInsecure(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestClusterOCIEndpointFromContainer(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://localhost:9090", "oci://host.docker.internal:9092"},
		{"http://127.0.0.1:9090", "oci://host.docker.internal:9092"},
		{"https://hub.confighub.com", "oci://oci.hub.confighub.com:443"},
	}
	for _, c := range cases {
		got, err := clusterOCIEndpointFromContainer(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("clusterOCIEndpointFromContainer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClusterAPIEndpointFromContainer(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Loopback hosts are rewritten to host.docker.internal, scheme/port/path kept.
		{"http://localhost:9090", "http://host.docker.internal:9090"},
		{"http://127.0.0.1:9090", "http://host.docker.internal:9090"},
		{"http://localhost", "http://host.docker.internal"},
		// Non-loopback hosts pass through (trailing slash trimmed).
		{"https://hub.confighub.com", "https://hub.confighub.com"},
		{"https://hub.confighub.com/", "https://hub.confighub.com"},
		{"https://pr-3415.testhub.confighub.net", "https://pr-3415.testhub.confighub.net"},
	}
	for _, c := range cases {
		got, err := clusterAPIEndpointFromContainer(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("clusterAPIEndpointFromContainer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
