// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package configmaps defines the configmaps pack: the Mutation Triggers that keep the hashes
// that roll a workload when a ConfigMap it uses changes, and that bound how many immutable
// ConfigMaps render-configmap leaves behind.
//
// A pod template carries one annotation per ConfigMap it uses, hash.confighub.com/<configmap>,
// whose value is the named placeholder for the ConfigMap (set-configmap-hash-annotations adds
// them). Needs/Provides fills each with the confighub.com/Hash of the ConfigMap it names, which
// the SetHash Trigger keeps for ConfigMaps authored as Kubernetes/YAML. A ConfigMap rendered
// immutable by render-configmap changes its name with its content instead, and the Prune
// Trigger removes old renderings.
//
// `cub variant upload` creates the Prune Trigger in a Space that runs its own Triggers and needs
// it. It does not create SetHash: it fills the annotation for a ConfigMap authored as it is
// uploaded with a TransformPaths Link that hashes the ConfigMap with get-hash. A Space that
// selects its Triggers from a policy Space needs them installed there, where they can be
// selected by their Pack label.
package configmaps

import (
	"github.com/confighub/sdk/core/constants"
	api "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
)

// Group is the constants.PackLabel value of this pack's Triggers, by which a Space whose Triggers
// come from a policy Space selects them.
const Group = "configmaps"

// Trigger is one of the pack's Triggers.
type Trigger struct {
	Slug          string
	Description   string
	Event         string
	ToolchainType workerapi.ToolchainType
	Function      string
	Arguments     []api.FunctionArgument
	WhereResource string
	// Guards are the guards the Trigger states on the paths it writes.
	Guards api.GuardStamp
	// Clearance is what the Trigger is cleared for: its own Guards, and the guards of the
	// writers whose paths it has to change.
	Clearance api.Clearance
}

// Labels are the labels each of the pack's Triggers carries.
func Labels() map[string]string {
	return map[string]string{constants.PackLabel: Group}
}

// SetHash keeps a hash of each ConfigMap's data and binaryData in its confighub.com/Hash
// annotation, which Needs/Provides carries to the hash annotations of the pod templates that use
// it. It leaves out the ConfigMaps render-configmap produces, which carry a hash of the rendered
// file already, and which, latest or superseded, carry a RenderRevision.
//
// It guards the annotation with Trigger=set-hash, so that another writer has to be cleared for
// it before overwriting the hash, and is cleared for that guard itself, so that it can write the
// hash again when the data changes. A change that is not cleared for it is withheld at the hash
// and reported as a Guarded conflict, although the Trigger then writes the hash of the new data in
// the same write: an upgrade bringing the upstream's hash, or an update from a file without the
// annotation, which proposes removing it. The conflict says that the hash is the Trigger's, and
// is dismissed or applied as any other is.
var SetHash = Trigger{
	Slug:          "set-configmap-hash",
	Description:   "Keeps a hash of each ConfigMap's data and binaryData in its confighub.com/Hash annotation, so that the workloads using it roll when it changes.",
	Event:         "Mutation",
	ToolchainType: workerapi.ToolchainKubernetesYAML,
	Function:      "set-hash",
	Arguments:     []api.FunctionArgument{{ParameterName: "path", Value: "data"}, {ParameterName: "path", Value: "binaryData"}},
	WhereResource: "ConfigHub.ResourceType = 'v1/ConfigMap' AND metadata.annotations.confighub~1com/RenderRevision IS NULL",
	Guards:        api.GuardStamp{api.GuardKeyTrigger: "set-hash"},
	Clearance:     api.Clearance{{Key: api.GuardKeyTrigger, Operator: api.ClearanceOperatorIn, Values: []string{"set-hash"}}},
}

// Prune bounds how many immutable ConfigMaps rendered from one AppConfig Unit accumulate, since
// each change to the AppConfig Unit adds a rendering.
//
// The Upsert Link that renders a ConfigMap guards it with Link=Upsert, so that nothing else
// rewrites what it rendered. Removing an old rendering is a write to it, so Prune is cleared for
// that guard.
var Prune = Trigger{
	Slug:          "prune-configmaps",
	Description:   "Keeps the 10 newest immutable ConfigMaps rendered from each AppConfig Unit and removes the rest.",
	Event:         "Mutation",
	ToolchainType: workerapi.ToolchainKubernetesYAML,
	Function:      "prune-configmaps",
	Arguments:     []api.FunctionArgument{{ParameterName: "revision-history-limit", Value: 10}},
	WhereResource: "ConfigHub.ResourceType = 'v1/ConfigMap'",
	Clearance:     api.Clearance{{Key: api.GuardKeyLink, Operator: api.ClearanceOperatorIn, Values: []string{"Upsert"}}},
}

// Triggers are the pack's Triggers.
func Triggers() []Trigger {
	return []Trigger{SetHash, Prune}
}
