// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

func init() {
	newUnitBlobCmd(
		"data",
		"Data",
		"Show the config data of a unit",
		`Display the configuration data of a unit, as text.

Replaces 'cub unit get --data-only'. Use --output-file / -O to write the data
to a file instead of stdout.`,
		legacyFilenameAlias,
	)
}
