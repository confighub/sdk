// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubbyname

import (
	"fmt"
	"math/rand"
	"slices"

	"github.com/google/uuid"
)

// Get a random cubby name
func Random() string {
	return cubbyNames[rand.Intn(len(cubbyNames))]
}

// Unused name (takes a list of used names)
func Unused(usedNames []string) string {
	// subtract used names from cubby names and then pick a random name
	availableNames := slices.DeleteFunc(cubbyNames, func(name string) bool {
		return slices.Contains(usedNames, name)
	})
	if len(availableNames) > 0 {
		return availableNames[rand.Intn(len(availableNames))]
	}
	// no names are available. Continue looking for an unused name by appending a suffix sequentially
	for i := range 100 {
		name := fmt.Sprintf("%s-%02d", Random(), i)
		if !slices.Contains(usedNames, name) {
			return name
		}
	}
	// Last resort: return a string consisting of "cubby-" and a uuid
	return fmt.Sprintf("cubby-%s", uuid.New().String())
}

func List() []string {
	return cubbyNames
}

var cubbyNames = []string{
	"fuzzy-paws",
	"tiny-snout",
	"sleepy-cub",
	"chubby-bear",
	"cuddly-tail",
	"snuggly-ears",
	"playful-muzzle",
	"gentle-paw",
	"little-whiskers",
	"happy-claws",
	"fluffy-nose",
	"sweet-growl",
	"merry-paws",
	"soft-den",
	"warm-fur",
	"rolypoly-cub",
	"golden-paw",
	"curious-snout",
	"jolly-bear",
	"baby-tracks",

	"paw-print",
	"honey-paw",
	"den-cub",
	"fur-ball",
	"moss-nest",
	"berry-bear",
	"moon-cub",
	"tree-paw",
	"snow-cub",
	"cave-den",
	"forest-paw",
	"tail-tracks",
	"hug-paw",
	"nest-cub",
	"sun-bear",
	"stump-den",
	"whisker-paw",
	"growl-cub",
	"meadow-bear",
	"acorn-den",

	"cubby-cubby",
	"paw-paw",
	"snuggle-snuggle",
	"bear-bear",
	"fuzzy-wuzzy",
	"fluffy-puffy",
	"roly-roly",
	"tiny-tiny",
	"snout-snout",
	"honey-honey",
	"purr-purr",
	"muzzle-muzzle",
	"hug-hug",
	"den-den",
	"fur-fur",
	"whisker-whisker",
	"tail-tail",
	"cub-cub",
	"cubbie-cubbie",
	"paws-paws",

	"forest-cub",
	"honey-moon",
	"berry-sun",
	"snow-den",
	"tree-paw",
	"meadow-cub",
	"river-bear",
	"mountain-cub",
	"bamboo-cub",
	"cloud-bear",
	"fern-den",
	"pine-cub",
	"star-paw",
	"sunrise-cub",
	"frosty-paw",
	"acorn-bear",
	"moss-cub",
	"shadow-bear",
	"moon-paw",
	"salmon-cub",
	"tumble-cub",
	"snuggle-bear",
	"pounce-paws",
	"snooze-den",
	"hug-cub",
	"nuzzle-paw",
	"shuffle-bear",
	"munch-cub",
	"clamber-paws",
	"scoot-den",
	"wiggle-tail",
	"doze-cub",
	"paddle-paws",
	"scamper-bear",
	"stretch-cub",
	"peek-paw",
	"roll-cub",
	"stumble-bear",
	"curl-cub",
	"cuddly-cub",
	"berry-bear",
	"tiny-tail",
	"sleepy-snout",
	"mossy-muzzle",
	"fuzzy-fur",
	"playful-paws",
	"snuggly-snout",
	"happy-hug",
	"cubby-claws",
	"fluffy-fur",
	"gentle-growl",
	"baby-bear",
	"puddle-paws",
	"cubby-cuddle",
	"fuzzy-face",
	"soft-snout",
	"sweet-snuggle",
	"little-lair",
	"mellow-muzzle",

	"honey-cub",
	"snuggle-den",
	"cozy-cub",
	"chubby-paws",
	"fluffy-cub",
	"gentle-bear",
	"tiny-cub",
	"sleepy-den",
	"snuggly-cub",
	"curious-cub",
	"happy-cub",
	"soft-paws",
	"warm-cub",
	"sweet-cub",
	"playful-cub",
	"cuddle-cub",
	"fuzzy-cub",
	"merry-cub",
	"baby-cub",
	"golden-cub",
}
