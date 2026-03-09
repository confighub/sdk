// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"hash/crc32"

	"github.com/confighub/sdk/configkit/cubkit"
)

const MaxSlugLength = 128

func makeSlug(providedText string) string {
	return cubkit.CubNormalizeName(providedText)
}

// truncateWithHash truncates s to maxLen, replacing excess chars with a hash suffix.
// If s is already within maxLen, it is returned as-is.
func truncateWithHash(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	h := crc32.ChecksumIEEE([]byte(s))
	suffix := fmt.Sprintf("-%08x", h)
	return s[:maxLen-len(suffix)] + suffix
}
