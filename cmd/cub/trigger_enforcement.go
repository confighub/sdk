// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"time"

	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// The annotations that track the enforcement of a validating Trigger's policy: --priority and
// --enforced-by set them, and creating a validating Trigger blocking, --unwarn and --warn keep
// EnforcedAt.
var (
	triggerPriority      string
	triggerEnforcedBy    string
	triggerPrioritySet   bool
	triggerEnforcedBySet bool
)

func addTriggerEnforcementFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&triggerPriority, "priority", "",
		"priority of the policy a validating trigger checks, recorded in its "+constants.TriggerPriorityAnnotation+
			" annotation: High, Medium or Low; - (or Unspecified) removes it")
	cmd.Flags().StringVar(&triggerEnforcedBy, "enforced-by", "",
		"when a validating trigger is due to be enforced (made blocking with --unwarn), recorded in its "+
			constants.TriggerEnforcedByAnnotation+" annotation: RFC 3339, or a date such as 2026-12-31; - removes it")
}

// readTriggerEnforcementFlags records which of the enforcement flags were given. An empty value is
// a flag not given, as for every flag that sets a field; - removes the annotation.
func readTriggerEnforcementFlags(cmd *cobra.Command) {
	triggerPrioritySet = cmd.Flags().Changed("priority") && triggerPriority != ""
	triggerEnforcedBySet = cmd.Flags().Changed("enforced-by") && triggerEnforcedBy != ""
}

// triggerEnforcementAnnotations returns the annotations the enforcement flags set: a value to set, or nil
// to remove. An annotation whose flag was not given is absent.
//
// Enforcing, by --unwarn, records when in EnforcedAt, and going back to warnings removes it, so
// that it always says since when the Trigger has been blocking. wasWarn says whether the Trigger
// warned before; an --unwarn of a Trigger already blocking keeps the time it was enforced.
func triggerEnforcementAnnotations(wasWarn bool) (map[string]*string, error) {
	annotations := map[string]*string{}
	set := func(key, value string) {
		if value == "" {
			annotations[key] = nil
			return
		}
		annotations[key] = &value
	}
	if triggerPrioritySet {
		priority, err := cubapi.ParseTriggerPriority(triggerPriority)
		if err != nil {
			return nil, err
		}
		set(constants.TriggerPriorityAnnotation, priority)
	}
	if triggerEnforcedBySet {
		enforcedBy, err := cubapi.ParseEnforcementTime(triggerEnforcedBy)
		if err != nil {
			return nil, err
		}
		set(constants.TriggerEnforcedByAnnotation, enforcedBy)
	}
	if unwarnTrigger && wasWarn {
		set(constants.TriggerEnforcedAtAnnotation, cubapi.FormatEnforcementTime(time.Now()))
	}
	if warnTrigger {
		set(constants.TriggerEnforcedAtAnnotation, "")
	}
	return annotations, nil
}

// stampEnforcedAtOnCreate records in EnforcedAt that a validating Trigger created blocking is
// enforced from its creation, as --unwarn records it for one that warned first. Whether the
// Trigger validates is up to its function, or its Invocation's, so a dry run of the create asks the
// server. An EnforcedAt given with --annotation is kept, and a dry run of the command writes
// nothing to stamp.
func stampEnforcedAtOnCreate(spaceID uuid.UUID, params *goclientnew.CreateTriggerParams, body *goclientnew.Trigger) error {
	if body.Warn || params.DryRun != nil {
		return nil
	}
	if _, given := body.Annotations[constants.TriggerEnforcedAtAnnotation]; given {
		return nil
	}
	probe := true
	dryRunParams := *params
	dryRunParams.DryRun = &probe
	res, err := cubClientNew.CreateTriggerWithResponse(ctx, spaceID, &dryRunParams, *body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if !res.JSON200.Validating {
		return nil
	}
	enforcedAt := cubapi.FormatEnforcementTime(time.Now())
	applyEnforcementAnnotations(&body.Annotations, map[string]*string{constants.TriggerEnforcedAtAnnotation: &enforcedAt})
	return nil
}

// applyEnforcementAnnotations writes annotations into a Trigger's annotation map.
func applyEnforcementAnnotations(target *map[string]string, annotations map[string]*string) {
	if len(annotations) == 0 {
		return
	}
	if *target == nil {
		*target = map[string]string{}
	}
	for key, value := range annotations {
		if value == nil {
			delete(*target, key)
		} else {
			(*target)[key] = *value
		}
	}
}

// patchEnforcementAnnotations adds annotations to a merge patch, where null removes one.
func patchEnforcementAnnotations(patchMap map[string]interface{}, annotations map[string]*string) {
	if len(annotations) == 0 {
		return
	}
	patch, _ := patchMap["Annotations"].(map[string]interface{})
	if patch == nil {
		patch = map[string]interface{}{}
	}
	for key, value := range annotations {
		if value == nil {
			patch[key] = nil
		} else {
			patch[key] = *value
		}
	}
	patchMap["Annotations"] = patch
}
