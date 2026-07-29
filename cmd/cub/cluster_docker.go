// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Docker state inspection for `cub cluster` port allocation. We shell out to
// the docker CLI rather than using the Go SDK to avoid pulling in a heavy
// dependency for two read-only calls.

// clusterBoundHostPorts returns the set of TCP host ports currently bound by
// any running docker container. Parses `docker ps --format '{{.Ports}}'`,
// which emits lines like:
//
//	0.0.0.0:30000-30009->30000-30009/tcp, 127.0.0.1:53840->6443/tcp
//
// Single ports and ranges are both expanded. UDP and unbound ports are ignored.
func clusterBoundHostPorts(ctx context.Context) (map[int]bool, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Ports}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	return clusterParseBoundPorts(string(out)), nil
}

// matches "addr:port->cport/proto" or "addr:port-port->cport-cport/proto",
// capturing the host port (or range) and the proto. addr is IPv4 dotted-quad
// or [IPv6]. We filter to /tcp because UDP lives in a separate port namespace
// and doesn't conflict with kind's TCP NodePort exposure.
var clusterPortMappingRE = regexp.MustCompile(`(?:\d+\.\d+\.\d+\.\d+|\[[^\]]+\]):(\d+)(?:-(\d+))?->\d+(?:-\d+)?/(\w+)`)

func clusterParseBoundPorts(dockerPsOutput string) map[int]bool {
	bound := map[int]bool{}
	for _, m := range clusterPortMappingRE.FindAllStringSubmatch(dockerPsOutput, -1) {
		if m[3] != "tcp" {
			continue
		}
		start, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		end := start
		if m[2] != "" {
			if e, err := strconv.Atoi(m[2]); err == nil {
				end = e
			}
		}
		for p := start; p <= end; p++ {
			bound[p] = true
		}
	}
	return bound
}

// clusterPickFreePortWindow returns the first port `start` such that ports
// [start, start+size) are all free in `bound` and start+size-1 <= rangeEnd.
// Returns an error if no window fits.
func clusterPickFreePortWindow(bound map[int]bool, rangeStart, rangeEnd, size int) (int, error) {
	if size <= 0 {
		return 0, fmt.Errorf("window size must be positive")
	}
	for s := rangeStart; s+size-1 <= rangeEnd; s++ {
		clear := true
		for p := s; p < s+size; p++ {
			if bound[p] {
				clear = false
				break
			}
		}
		if clear {
			return s, nil
		}
	}
	return 0, fmt.Errorf("no free %d-port window in %d-%d (try --no-ports or stop conflicting containers)", size, rangeStart, rangeEnd)
}

// clusterHostFromContainer returns the address a container on the local docker
// network reaches this host by — the address to stamp into an Argo Application's
// repoURL, or into argobot's CONFIGHUB_URL.
//
// This is a property of the cluster, not of the ConfigHub server: the server has
// no idea what cluster exists on your machine, and the answer differs by docker
// runtime. `cub cluster up` created the cluster, so it is the one thing in a
// position to answer.
//
// Docker Desktop runs the daemon in a VM and publishes host.docker.internal for
// exactly this; the bridge gateway is inside the VM and unreachable from
// containers. On Linux the daemon shares the host's network stack, the gateway
// is the way in, and host.docker.internal does not resolve from a pod at all —
// kind pods go through CoreDNS rather than docker's resolver.
func clusterHostFromContainer(ctx context.Context) string {
	if clusterIsDockerDesktop(ctx) {
		return dockerInternalHost
	}
	if gw := clusterDockerGatewayIPv4(ctx, clusterKindNetwork); gw != "" {
		return gw
	}
	// Nothing better to offer. On a runtime we did not recognise this may not
	// resolve, which surfaces as an Application that cannot pull.
	return dockerInternalHost
}

const (
	dockerInternalHost = "host.docker.internal"
	// clusterKindNetwork is the docker network kind attaches clusters to.
	clusterKindNetwork = "kind"
)

// clusterIsDockerDesktop reports whether the docker daemon is Docker Desktop,
// which reports itself in `docker info`.
func clusterIsDockerDesktop(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.OperatingSystem}}").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "docker desktop")
}

// clusterDockerGatewayIPv4 returns a docker network's IPv4 gateway, or "" if the
// network does not exist or has no IPv4 subnet.
//
// One gateway is emitted per line and the first IPv4 kept: a dual-stack daemon
// lists both an IPv4 and an IPv6 entry, and the IPv6 one can come first, so
// indexing into the list picks the wrong address.
func clusterDockerGatewayIPv4(ctx context.Context, network string) string {
	out, err := exec.CommandContext(ctx, "docker", "network", "inspect", network,
		"--format", "{{range .IPAM.Config}}{{println .Gateway}}{{end}}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if ip := net.ParseIP(strings.TrimSpace(line)); ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}
