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
to a file instead of stdout.

With --where, reads every unit the clause selects in one request rather than one
per unit, and prints the endpoint's rows -- identity, DataHash, DataSize and the
document -- as JSON, because a stream of concatenated documents cannot say which
unit each came from. Scoped to --space unless that is "*". Examples:

  cub unit data --space my-space my-unit
  cub unit data --space my-space --where "Slug LIKE 'app-%'"
  cub unit data --space "*" --where "Labels.Tier = 'backend'" -o jq='.[].Slug'`,
		legacyFilenameAlias,
	)
}
