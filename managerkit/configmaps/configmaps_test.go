// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package configmaps

import (
	"testing"

	api "github.com/confighub/sdk/core/function/api"
)

// Each Trigger is cleared for the guards it states, or it could not rewrite what it wrote: SetHash
// writes the hash again whenever the data changes.
func TestTriggersAreClearedForTheirOwnGuards(t *testing.T) {
	for _, trigger := range Triggers() {
		if err := api.CheckGuardStampCleared(trigger.Guards, trigger.Clearance); err != nil {
			t.Errorf("%s: %v", trigger.Slug, err)
		}
	}
	if SetHash.Guards[api.GuardKeyTrigger] != "set-hash" {
		t.Errorf("SetHash guards the hash with %v, not Trigger=set-hash", SetHash.Guards)
	}
}

// Prune removes renderings the Upsert Link that rendered them guards with Link=Upsert.
func TestPruneIsClearedForTheRenderingsLinkGuard(t *testing.T) {
	if admitted, _ := Prune.Clearance.Admits(map[string]string{api.GuardKeyLink: "Upsert"}); !admitted {
		t.Errorf("Prune's clearance %v does not admit Link=Upsert", Prune.Clearance)
	}
}
