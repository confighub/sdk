// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestClusterOCIEndpoint(t *testing.T) {
	cases := []struct {
		ociURL, apiURL, want string
	}{
		// Recorded registry (from /api/info at login) wins over the derivation.
		{"http://oci.localhost:9092", "http://localhost:9090", "oci://oci.localhost:9092"},
		{"https://oci.hub.confighub.com", "https://hub.confighub.com", "oci://oci.hub.confighub.com:443"},
		{"https://registry.example.com:8443", "https://hub.confighub.com", "oci://registry.example.com:8443"},
		// A registry somewhere other than oci.<api-host> — the case the
		// derivation cannot see.
		{"https://ghcr.example.net", "https://hub.confighub.com", "oci://ghcr.example.net:443"},
		{"http://oci.example.com", "http://hub.example.com", "oci://oci.example.com:80"},
		// Nothing recorded: derived from the API URL as before.
		{"", "http://localhost:9090", "oci://localhost:9092"},
		{"", "http://127.0.0.1:9090", "oci://localhost:9092"},
		{"", "https://hub.confighub.com", "oci://oci.hub.confighub.com:443"},
		{"", "https://hub.confighub.com:443", "oci://oci.hub.confighub.com:443"},
		{"", "https://pr-3415.testhub.confighub.net", "oci://oci.pr-3415.testhub.confighub.net:443"},
	}
	for _, c := range cases {
		got, err := clusterOCIEndpoint(c.ociURL, c.apiURL)
		if err != nil {
			t.Errorf("%s/%s: %v", c.ociURL, c.apiURL, err)
			continue
		}
		if got != c.want {
			t.Errorf("clusterOCIEndpoint(%q, %q) = %q, want %q", c.ociURL, c.apiURL, got, c.want)
		}
	}
}

func TestClusterOCIInsecure(t *testing.T) {
	cases := []struct {
		ociURL, apiURL string
		want           bool
	}{
		// The recorded registry states its own transport.
		{"http://oci.localhost:9092", "http://localhost:9090", true},
		{"https://oci.hub.confighub.com", "https://hub.confighub.com", false},
		// A TLS registry in front of a plain-HTTP API, and the reverse: only
		// the recorded URL can tell these apart.
		{"https://oci.example.com", "http://hub.example.com", false},
		{"http://oci.example.com", "https://hub.example.com", true},
		// Nothing recorded: the API URL's scheme decides.
		{"", "http://localhost:9090", true},
		{"", "http://127.0.0.1:9090", true},
		{"", "http://my-internal-cub:9090", true},
		{"", "HTTP://localhost:9090", true},
		{"", "https://hub.confighub.com", false},
		{"", "https://hub.confighub.com:443", false},
	}
	for _, c := range cases {
		got, err := clusterOCIInsecure(c.ociURL, c.apiURL)
		if err != nil {
			t.Errorf("%s/%s: %v", c.ociURL, c.apiURL, err)
			continue
		}
		if got != c.want {
			t.Errorf("clusterOCIInsecure(%q, %q) = %v, want %v", c.ociURL, c.apiURL, got, c.want)
		}
	}
}

func TestClusterOCIEndpointFromContainer(t *testing.T) {
	// Which address a container reaches this host by depends on the docker
	// runtime the test happens to run on (host.docker.internal on Docker
	// Desktop, the kind bridge gateway on Linux). What is under test here is
	// that a local registry gets rewritten to it with its port intact, so ask
	// for the expected host rather than pinning one runtime's answer.
	local := "oci://" + clusterJoinHostPort(clusterHostFromContainer(ctx), "9092")

	cases := []struct {
		ociURL, apiURL, want string
	}{
		{"", "http://localhost:9090", local},
		{"", "http://127.0.0.1:9090", local},
		{"", "https://hub.confighub.com", "oci://oci.hub.confighub.com:443"},
		// A local server reports "localhost", but one predating the carve-out in
		// ociExternalHost — or one pointed elsewhere by OCI_EXTERNAL_HOST —
		// reports "oci.localhost", which does not look like a loopback host and
		// is just as unreachable from inside the cluster. The rewrite follows
		// the API URL rather than the registry hostname so both land right.
		{"http://localhost:9092", "http://localhost:9090", local},
		{"http://oci.localhost:9092", "http://localhost:9090", local},
		// A remote registry is reachable by the same name from inside the
		// cluster, so it passes through.
		{"https://oci.hub.confighub.com", "https://hub.confighub.com", "oci://oci.hub.confighub.com:443"},
	}
	for _, c := range cases {
		got, err := clusterOCIEndpointFromContainer(c.ociURL, c.apiURL)
		if err != nil {
			t.Errorf("%s/%s: %v", c.ociURL, c.apiURL, err)
			continue
		}
		if got != c.want {
			t.Errorf("clusterOCIEndpointFromContainer(%q, %q) = %q, want %q", c.ociURL, c.apiURL, got, c.want)
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
