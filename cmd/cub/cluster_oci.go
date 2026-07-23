// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/url"
	"strings"
)

// OCI registry endpoint derivation for `cub cluster`. cub's OCI registry
// shares the API server's host/transport, so the OCI endpoint and its
// plain-HTTP-vs-TLS behavior are derived from the API server URL.

// clusterOCIEndpoint returns the OCI registry URL for the given ConfigHub API URL.
//
//   - Loopback hosts (localhost, 127.0.0.1, 0.0.0.0, ::1) map to
//     oci://localhost:9092 (the dev convention; the cub server's OCI
//     endpoint is on a separate port from the JSON API).
//   - Anything else prepends "oci." to the API hostname and forces port 443
//     (e.g. https://hub.confighub.com → oci://oci.hub.confighub.com:443).
func clusterOCIEndpoint(apiURL string) (string, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", apiURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in %q", apiURL)
	}
	if clusterIsLoopback(host) {
		return "oci://localhost:9092", nil
	}
	return "oci://oci." + host + ":443", nil
}

// clusterOCIEndpointFromContainer returns the OCI URL as seen from inside a
// docker container on the same host (e.g. a kind node, or Argo running in
// kind). For loopback endpoints this rewrites to host.docker.internal; for
// remote endpoints it's identical to clusterOCIEndpoint.
func clusterOCIEndpointFromContainer(apiURL string) (string, error) {
	ext, err := clusterOCIEndpoint(apiURL)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(ext)
	if err != nil {
		return "", err
	}
	if clusterIsLoopback(u.Hostname()) {
		port := u.Port()
		if port != "" {
			u.Host = "host.docker.internal:" + port
		} else {
			u.Host = "host.docker.internal"
		}
		return u.String(), nil
	}
	return ext, nil
}

// clusterAPIEndpointFromContainer returns the ConfigHub API URL as seen from
// inside a container running on the same host (e.g. argobot in a kind node).
// A loopback host is rewritten to host.docker.internal (preserving scheme,
// port, and path); a non-loopback host is returned unchanged.
func clusterAPIEndpointFromContainer(apiURL string) (string, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", apiURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in %q", apiURL)
	}
	if clusterIsLoopback(host) {
		if port := u.Port(); port != "" {
			u.Host = "host.docker.internal:" + port
		} else {
			u.Host = "host.docker.internal"
		}
		return strings.TrimRight(u.String(), "/"), nil
	}
	return strings.TrimRight(apiURL, "/"), nil
}

// clusterOCIInsecure reports whether the OCI registry derived from apiURL
// should be accessed over plain HTTP. An http:// API URL implies an http://
// OCI endpoint (the local-dev case); anything else (https://) uses TLS.
func clusterOCIInsecure(apiURL string) (bool, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return false, fmt.Errorf("parse %q: %w", apiURL, err)
	}
	return strings.EqualFold(u.Scheme, "http"), nil
}

func clusterIsLoopback(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return true
	}
	return false
}
