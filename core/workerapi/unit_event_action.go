// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package workerapi

import "time"

// The action types below describe what a UnitEvent recorded. Nothing records new UnitEvents;
// they remain readable as history.

type ActionStatusType string

// Status values
const (
	ActionStatusNone        ActionStatusType = "None"
	ActionStatusPending     ActionStatusType = "Pending"
	ActionStatusSubmitted   ActionStatusType = "Submitted"
	ActionStatusProgressing ActionStatusType = "Progressing"
	ActionStatusCompleted   ActionStatusType = "Completed"
	ActionStatusFailed      ActionStatusType = "Failed"
	ActionStatusCanceled    ActionStatusType = "Canceled"
	ActionStatusAborted     ActionStatusType = "Aborted" // Operation superseded by a newer one
)

type ActionResultType string

// Drift values
const (
	ActionResultNone ActionResultType = "None"

	ActionResultFunctionInvocationCompleted ActionResultType = "FunctionInvocationCompleted"
	ActionResultFunctionInvocationFailed    ActionResultType = "FunctionInvocationFailed"
)

type ActionType string

// Action values
const (
	ActionNA     ActionType = "N/A"
	ActionCancel ActionType = "Cancel"

	// ActionApply is no longer performed. Historical UnitEvents carry it.
	ActionApply ActionType = "Apply"

	ActionInvokeFunctions ActionType = "InvokeFunctions"
	ActionListFunctions   ActionType = "ListFunctions"
)

type ActionResultBaseMeta struct {
	RevisionNum  int64
	Action       ActionType       `bun:",notnull" swaggertype:"string"`
	Result       ActionResultType `bun:",notnull,default:'None'" swaggertype:"string"`
	Status       ActionStatusType `bun:",notnull,default:'None'" swaggertype:"string"`
	Message      string           `bun:"type:text"`
	StartedAt    time.Time        `json:",omitempty" bun:"type:timestamptz"`
	TerminatedAt *time.Time       `json:",omitempty" bun:"type:timestamptz"`
}
