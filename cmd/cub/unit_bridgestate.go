// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

func init() {
	newUnitBlobCmd(
		"bridgestate",
		"BridgeState",
		"Show the BridgeState of a unit",
		`Display the BridgeState of a unit: a bridge-implementation blob whose
contents vary by bridge. The Kubernetes bridge stores an Inventory object
(what resources the Unit owns in the cluster); ArgoCDOCI / FluxOCI store the
Application / HelmRelease / Kustomization they create.`,
		legacyOutputAlias,
	)
}
