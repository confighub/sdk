// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net"
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

// The addresses below are the ones a container in the cluster reaches ConfigHub
// by, which is not the address cub itself uses. That is a property of the
// cluster's networking rather than of the ConfigHub server — the server has no
// idea what cluster exists on this machine — so `cub cluster` answers it from
// the docker runtime it just built the cluster on. See clusterHostFromContainer.
//
// A non-loopback ConfigHub URL means the server is remote and already reachable
// by the same name from inside the cluster, so those pass through untouched.

// clusterOCIEndpointFromContainer returns the OCI URL as seen from inside a
// container in the cluster (Argo's repo-server pulling a Release).
func clusterOCIEndpointFromContainer(apiURL string) (string, error) {
	ext, err := clusterOCIEndpoint(apiURL)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(ext)
	if err != nil {
		return "", err
	}
	if !clusterIsLoopback(u.Hostname()) {
		return ext, nil
	}
	u.Host = clusterJoinHostPort(clusterHostFromContainer(ctx), u.Port())
	return u.String(), nil
}

// clusterAPIEndpointFromContainer returns the ConfigHub API URL as seen from
// inside a container in the cluster (argobot's CONFIGHUB_URL).
func clusterAPIEndpointFromContainer(apiURL string) (string, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", apiURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in %q", apiURL)
	}
	if !clusterIsLoopback(host) {
		return strings.TrimRight(apiURL, "/"), nil
	}
	u.Host = clusterJoinHostPort(clusterHostFromContainer(ctx), u.Port())
	return strings.TrimRight(u.String(), "/"), nil
}

// clusterJoinHostPort keeps the port when there is one.
func clusterJoinHostPort(host, port string) string {
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
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
