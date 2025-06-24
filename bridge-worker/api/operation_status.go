// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"time"

	"github.com/google/uuid"
)

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
)

type ActionResultType string

// Drift values
const (
	ActionResultNone              ActionResultType = "None"
	ActionResultApplyCompleted    ActionResultType = "ApplyCompleted"
	ActionResultApplyFailed       ActionResultType = "ApplyFailed"
	ActionResultApplyWaitFailed   ActionResultType = "ApplyWaitFailed"
	ActionResultDestroyCompleted  ActionResultType = "DestroyCompleted"
	ActionResultDestroyFailed     ActionResultType = "DestroyFailed"
	ActionResultDestroyWaitFailed ActionResultType = "DestroyWaitFailed"
	ActionResultRefreshFailed     ActionResultType = "RefreshFailed"
	ActionResultRefreshAndDrifted ActionResultType = "RefreshAndDrifted"
	ActionResultRefreshAndNoDrift ActionResultType = "RefreshAndNoDrift"
	ActionResultImportCompleted   ActionResultType = "ImportCompleted"
	ActionResultImportFailed      ActionResultType = "ImportFailed"

	ActionResultFunctionInvocationCompleted ActionResultType = "FunctionInvocationCompleted"
	ActionResultFunctionInvocationFailed    ActionResultType = "FunctionInvocationFailed"
)

type ActionType string

// Action values
const (
	ActionNA        ActionType = "N/A"
	ActionApply     ActionType = "Apply"
	ActionDestroy   ActionType = "Destroy"
	ActionRefresh   ActionType = "Refresh"
	ActionImport    ActionType = "Import"
	ActionFinalize  ActionType = "Finalize"
	ActionHeartbeat ActionType = "Heartbeat"

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

// ActionResult is a result of action from the Bridgeworker
type ActionResult struct {
	UnitID  uuid.UUID `description:"UUID of the Unit on which the action is performed"`
	SpaceID uuid.UUID `description:"UUID of the Space of the Unit on which the action is performed"`
	// OrganizationID comes from the worker
	// QueuedOperationID links this result back to the original operation request.
	QueuedOperationID uuid.UUID `description:"UUID of the operation corresponding to the action request"`
	ActionResultBaseMeta
	Data      []byte `json:",omitempty" swaggertype:"string" format:"byte" description:"Configuration data of the Unit"`
	LiveState []byte `json:",omitempty" swaggertype:"string" format:"byte" description:"Live state corresponding to the Unit"`
	Outputs   []byte `json:",omitempty" swaggertype:"string" format:"byte" description:"Outputs resulting from applying the configuration data of the Unit"`
}
