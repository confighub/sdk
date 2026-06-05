// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
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
