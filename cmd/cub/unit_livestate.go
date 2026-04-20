// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

func init() {
	newUnitBlobCmd(
		"livestate",
		"LiveState",
		"Show the LiveState of a unit",
		`Display the LiveState of a unit: the cluster's resources with nothing elided
— full .status, .metadata.managedFields, controller-written fields. Use for
debugging the workload itself; for drift diffs against Data prefer
'cub unit livedata' (same shape as Data).`,
		legacyOutputAlias,
	)
}
