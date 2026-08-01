// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// OCI registry endpoint derivation for `cub cluster`. The server names its own
// registry at /api/info (see clusterOCIServerURL), so that is what these use.
// Only when it advertises none do they fall back to deriving the endpoint from
// the API server URL, on the assumption that the registry shares the API
// server's host and transport.

// clusterOCIEndpoint returns the OCI registry URL for the given ConfigHub API
// URL, preferring ociURL — the registry the server advertises.
//
// ociURL wins whenever it is set: only the server knows where its registry
// actually lives, and a deployment that puts it somewhere other than
// oci.<api-host> is invisible to the derivation below. It is empty against a
// server too old to advertise one or running without a registry, and then the
// original derivation applies:
//
//   - Loopback hosts (localhost, 127.0.0.1, 0.0.0.0, ::1) map to
//     oci://localhost:9092 (the dev convention; the cub server's OCI
//     endpoint is on a separate port from the JSON API).
//   - Anything else prepends "oci." to the API hostname and forces port 443
//     (e.g. https://hub.confighub.com → oci://oci.hub.confighub.com:443).
func clusterOCIEndpoint(ociURL, apiURL string) (string, error) {
	if ociURL != "" {
		return clusterOCIEndpointFromAdvertised(ociURL)
	}
	host, err := clusterHost(apiURL)
	if err != nil {
		return "", err
	}
	if clusterIsLoopback(host) {
		return "oci://localhost:9092", nil
	}
	return "oci://oci." + host + ":443", nil
}

// clusterOCIEndpointFromAdvertised converts the http(s) registry URL the server
// advertises to the oci:// form. The port the scheme implies is spelled out when
// the URL omits it, so the result always carries one, as the derived form does.
func clusterOCIEndpointFromAdvertised(ociURL string) (string, error) {
	u, err := url.Parse(ociURL)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", ociURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in %q", ociURL)
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if strings.EqualFold(u.Scheme, "http") {
			port = "80"
		}
	}
	return "oci://" + clusterJoinHostPort(host, port), nil
}

// clusterHost returns the hostname of a URL cub was configured with.
func clusterHost(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", rawURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in %q", rawURL)
	}
	return host, nil
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
func clusterOCIEndpointFromContainer(ociURL, apiURL string) (string, error) {
	ext, err := clusterOCIEndpoint(ociURL, apiURL)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(ext)
	if err != nil {
		return "", err
	}
	// Whether ConfigHub runs on this machine is a property of the server cub
	// talks to, not of the registry hostname that server reports: a local
	// server names its registry "oci.localhost", which is no more reachable
	// from inside the cluster than "localhost" is even though it does not look
	// like a loopback host.
	apiHost, err := clusterHost(apiURL)
	if err != nil {
		return "", err
	}
	if !clusterIsLoopback(apiHost) {
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

// clusterOCIInsecure reports whether the OCI registry should be accessed over
// plain HTTP. The advertised registry URL states its own transport, so it
// answers for itself when set; otherwise an http:// API URL implies an http://
// OCI endpoint (the local-dev case) and anything else (https://) uses TLS.
func clusterOCIInsecure(ociURL, apiURL string) (bool, error) {
	target := ociURL
	if target == "" {
		target = apiURL
	}
	u, err := url.Parse(target)
	if err != nil {
		return false, fmt.Errorf("parse %q: %w", target, err)
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
