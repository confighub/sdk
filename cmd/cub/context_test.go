// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// TestOCIServerURLFromApiInfo covers what a login records as the context's OCI
// endpoint, and what `cub cluster attach` writes into Argo CD's repo creds, for
// the shapes /api/info actually reports: the local http server, a hosted https
// one, a server with no registry to advertise, and a server that says which
// scheme its registry speaks. Cases without OCITLS are servers that predate it.
func TestOCIServerURLFromApiInfo(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		apiInfo   *goclientnew.ApiInfo
		want      string
	}{{
		name:      "local server keeps http and its non-default port",
		serverURL: "http://localhost:9090",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.localhost", OCIPort: "9092"},
		want:      "http://oci.localhost:9092",
	}, {
		name:      "hosted server drops the default https port",
		serverURL: "https://hub.confighub.com",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.hub.confighub.com", OCIPort: "443"},
		want:      "https://oci.hub.confighub.com",
	}, {
		name:      "https with a non-default port keeps it",
		serverURL: "https://hub.example.com",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.hub.example.com", OCIPort: "8443"},
		want:      "https://oci.hub.example.com:8443",
	}, {
		name:      "no port advertised",
		serverURL: "https://hub.example.com",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.hub.example.com"},
		want:      "https://oci.hub.example.com",
	}, {
		name:      "no registry advertised records nothing",
		serverURL: "https://hub.example.com",
		apiInfo:   &goclientnew.ApiInfo{OCIPort: "9092"},
		want:      "",
	}, {
		// A bare host:port parses as an opaque URL whose scheme is the host, so
		// the scheme must not be taken verbatim from whatever url.Parse returns.
		name:      "server URL without a scheme falls back to https",
		serverURL: "hub.example.com:9090",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.hub.example.com", OCIPort: "9092"},
		want:      "https://oci.hub.example.com:9092",
	}, {
		name:      "IPv6 host is bracketed",
		serverURL: "http://[::1]:9090",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "::1", OCIPort: "9092"},
		want:      "http://[::1]:9092",
	}, {
		// The API reached over plain http inside the cluster, the registry over
		// https outside it, which the API's scheme alone would get wrong.
		name:      "OCITLS true over an http API, default port dropped",
		serverURL: "http://confighub.confighub.svc:9090",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "confighub-oci.example.com", OCIPort: "443", OCITLS: new(true)},
		want:      "https://confighub-oci.example.com",
	}, {
		name:      "OCITLS true keeps a non-default port",
		serverURL: "http://localhost:9090",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.example.com", OCIPort: "8443", OCITLS: new(true)},
		want:      "https://oci.example.com:8443",
	}, {
		// The API reached over https, the registry over plain http.
		name:      "OCITLS false over an https API",
		serverURL: "https://hub.example.com",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.internal", OCIPort: "9092", OCITLS: new(false)},
		want:      "http://oci.internal:9092",
	}, {
		name:      "OCITLS false over an http API, default port dropped",
		serverURL: "http://hub.internal:9090",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.internal", OCIPort: "80", OCITLS: new(false)},
		want:      "http://oci.internal",
	}, {
		// 443 is only the default for https, so it stays on an http URL.
		name:      "OCITLS false keeps 443",
		serverURL: "https://hub.example.com",
		apiInfo:   &goclientnew.ApiInfo{OCIHost: "oci.example.com", OCIPort: "443", OCITLS: new(false)},
		want:      "http://oci.example.com:443",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ociServerURLFromApiInfo(tt.serverURL, tt.apiInfo); got != tt.want {
				t.Errorf("ociServerURLFromApiInfo(%q, %+v) = %q, want %q", tt.serverURL, tt.apiInfo, got, tt.want)
			}
		})
	}
}
