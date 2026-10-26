// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"regexp"
	"testing"
)

func TestGenerateCodeVerifier(t *testing.T) {
	unreserved := regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
	seen := map[string]bool{}
	for range 100 {
		v := generateCodeVerifier()
		if !unreserved.MatchString(v) {
			t.Fatalf("verifier %q is not 43-128 unreserved characters", v)
		}
		if seen[v] {
			t.Fatalf("verifier %q generated twice", v)
		}
		seen[v] = true
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	// RFC 7636 Appendix B.
	got := generateCodeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
}
