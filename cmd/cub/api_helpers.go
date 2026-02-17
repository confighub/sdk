// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/configkit/cubkit"
)

func makeSlug(providedText string) string {
	return cubkit.ConfigHubResourceProvider.NormalizeName(providedText)
}
