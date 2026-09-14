// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ocibundle

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"oras.land/oras-go/v2/registry/remote/retry"
)

// NewPublicHTTPClient returns a client for pulling on someone else's behalf: it
// speaks only HTTPS and connects only to public addresses. A registry reference
// is caller-supplied, so without these checks a pull could reach the puller's own
// network -- a cloud metadata endpoint, a database, an internal service. The
// address is checked when each connection is dialed, after DNS resolution, so a
// name that resolves to a private address is refused too, as is every redirect
// and token request, since they are all dialed the same way.
func NewPublicHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   refuseNonPublicAddress,
	}
	transport := &http.Transport{
		// A proxy would be dialed in place of the registry, so the address the
		// dialer checked would not be the one the request reaches.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &http.Client{
		Transport: retry.NewTransport(httpsOnly{next: transport}),
	}
}

// httpsOnly refuses any request that is not HTTPS, including redirects and token
// requests to another host.
type httpsOnly struct {
	next http.RoundTripper
}

func (t httpsOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("refusing to fetch %s: only https is allowed", req.URL.Redacted())
	}
	return t.next.RoundTrip(req)
}

// refuseNonPublicAddress is a net.Dialer Control function: it runs with the
// resolved address about to be connected to.
func refuseNonPublicAddress(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("refusing to connect to %s: not an IP address", address)
	}
	if !IsPublicAddress(addr) {
		return fmt.Errorf("refusing to connect to %s: not a public address", address)
	}
	return nil
}

// sharedAddressSpace is the carrier-grade NAT range (RFC 6598), which netip does
// not count as private but is not reachable from the internet either.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// IsPublicAddress reports whether addr is a globally routable unicast address:
// not loopback, private, link-local (which holds the 169.254.169.254 metadata
// endpoint), unique-local, multicast, or unspecified.
func IsPublicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() &&
		addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!sharedAddressSpace.Contains(addr)
}
