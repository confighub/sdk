// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

func init() {
	newUnitBlobCmd(
		"livedata",
		"LiveData",
		"Show the LiveData of a unit",
		`Display the LiveData YAML of a unit: the cluster's current resources cleaned
up the same way the Worker cleaned them (status stripped, controller-managed
fields elided per ignoredFieldManagers).

For bridge-owned inventory (e.g., the Kubernetes inventory ConfigMap), use
'cub unit bridgestate' instead.`,
		legacyOutputAlias,
	)
}
