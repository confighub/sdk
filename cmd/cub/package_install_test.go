// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestKeyValueFlags(t *testing.T) {
	got, err := keyValueFlags("--input", []string{"a=1", "b=x=y", "c="})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "x=y" || got["c"] != "" || len(got) != 3 {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"novalue", "=1"} {
		if _, err := keyValueFlags("--input", []string{bad}); err == nil || !strings.Contains(err.Error(), "--input") {
			t.Errorf("%q: err = %v, want a refusal naming the flag", bad, err)
		}
	}
}
