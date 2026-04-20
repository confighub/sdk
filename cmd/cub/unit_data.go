// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

func init() {
	newUnitBlobCmd(
		"data",
		"Data",
		"Show the decoded config data of a unit",
		`Display the decoded configuration Data of a unit.

Replaces 'cub unit get --data-only'. Use --output-file / -O to write the data
to a file instead of stdout.`,
		legacyFilenameAlias,
	)
}
