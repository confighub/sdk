// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestClusterParseBoundPorts(t *testing.T) {
	in := `0.0.0.0:30000-30009->30000-30009/tcp, 127.0.0.1:53840->6443/tcp, 0.0.0.0:8080->30080/tcp, 0.0.0.0:8443->30443/tcp
0.0.0.0:8081->80/tcp
[::]:9999->9999/udp`
	got := clusterParseBoundPorts(in)

	wantPresent := []int{30000, 30005, 30009, 53840, 8080, 8443, 8081}
	for _, p := range wantPresent {
		if !got[p] {
			t.Errorf("expected port %d to be bound", p)
		}
	}
	if got[80] {
		t.Errorf("80 is container-side, not host; should not be bound")
	}
	if got[9999] {
		t.Errorf("9999 is UDP; expected to be filtered out (separate port namespace)")
	}
}

func TestClusterPickFreePortWindow(t *testing.T) {
	bound := map[int]bool{
		30000: true, 30001: true, 30002: true, 30003: true, 30004: true,
		// only 5 free between 30005-30009 — too narrow for a 10-window
		30010: true,
		// 30011-30099 free; first window is 30011-30020
	}
	got, err := clusterPickFreePortWindow(bound, 30000, 30099, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != 30011 {
		t.Errorf("expected 30011 (first 10-port window past 30010), got %d", got)
	}
}

func TestClusterPickFreePortWindowFirstSlot(t *testing.T) {
	got, err := clusterPickFreePortWindow(map[int]bool{}, 30000, 30099, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != 30000 {
		t.Errorf("expected 30000 on a clean host, got %d", got)
	}
}

func TestClusterPickFreePortWindowExhausted(t *testing.T) {
	bound := map[int]bool{}
	for p := 30000; p <= 30099; p++ {
		bound[p] = true
	}
	if _, err := clusterPickFreePortWindow(bound, 30000, 30099, 10); err == nil {
		t.Error("expected error when no window fits")
	}
}
