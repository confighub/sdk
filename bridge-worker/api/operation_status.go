// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
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
	ActionStatusAborted     ActionStatusType = "Aborted" // Operation superseded by a newer one
)

var ValidActionStatus = map[ActionStatusType]bool{
	ActionStatusNone:        true,
	ActionStatusPending:     true,
	ActionStatusSubmitted:   true,
	ActionStatusProgressing: true,
	ActionStatusCompleted:   true,
	ActionStatusFailed:      true,
	ActionStatusCanceled:    true,
	ActionStatusAborted:     true,
}

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

var ValidActionResult = map[ActionResultType]bool{
	ActionResultNone:                        true,
	ActionResultApplyCompleted:              true,
	ActionResultApplyFailed:                 true,
	ActionResultApplyWaitFailed:             true,
	ActionResultDestroyCompleted:            true,
	ActionResultDestroyFailed:               true,
	ActionResultDestroyWaitFailed:           true,
	ActionResultRefreshFailed:               true,
	ActionResultRefreshAndDrifted:           true,
	ActionResultRefreshAndNoDrift:           true,
	ActionResultImportCompleted:             true,
	ActionResultImportFailed:                true,
	ActionResultFunctionInvocationCompleted: true,
	ActionResultFunctionInvocationFailed:    true,
}

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
	ActionCancel    ActionType = "Cancel"

	ActionInvokeFunctions ActionType = "InvokeFunctions"
	ActionListFunctions   ActionType = "ListFunctions"
)

var ValidAction = map[ActionType]bool{
	ActionNA:              true,
	ActionApply:           true,
	ActionDestroy:         true,
	ActionRefresh:         true,
	ActionImport:          true,
	ActionFinalize:        true,
	ActionHeartbeat:       true,
	ActionCancel:          true,
	ActionInvokeFunctions: true,
	ActionListFunctions:   true,
}

type ActionResultBaseMeta struct {
	RevisionNum  int64
	Action       ActionType       `bun:",notnull" swaggertype:"string"`
	Result       ActionResultType `bun:",notnull,default:'None'" swaggertype:"string"`
	Status       ActionStatusType `bun:",notnull,default:'None'" swaggertype:"string"`
	Message      string           `bun:"type:text"`
	StartedAt    time.Time        `json:",omitempty" bun:"type:timestamptz"`
	TerminatedAt *time.Time       `json:",omitempty" bun:"type:timestamptz"`
}

const MaxActionResultMessageLength = 1024

func ValidateActionResultBaseMeta(arbm *ActionResultBaseMeta) error {
	if arbm.RevisionNum < 0 {
		return fmt.Errorf("RevisionNum %d invalid; must be non-negative", arbm.RevisionNum)
	}
	if !ValidAction[arbm.Action] {
		return fmt.Errorf("invalid Action %s", string(arbm.Action))
	}
	if !ValidActionResult[arbm.Result] {
		return fmt.Errorf("invalid Result %s", string(arbm.Result))
	}
	if !ValidActionStatus[arbm.Status] {
		return fmt.Errorf("invalid Status %s", string(arbm.Status))
	}
	if len(arbm.Message) > MaxActionResultMessageLength {
		return fmt.Errorf("Message length %d exceeds max length %d", len(arbm.Message), MaxActionResultMessageLength)
	}
	return nil
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

const MaxConfigDataLength = 64 * 1024 * 1024 // 64MB

func ValidateActionResultMeta(ar *ActionResult) error {
	err := ValidateActionResultBaseMeta(&ar.ActionResultBaseMeta)
	if err != nil {
		return err
	}
	if ar.UnitID == uuid.Nil {
		return errors.New("UnitID must be provided")
	}
	if ar.SpaceID == uuid.Nil {
		return errors.New("SpaceID must be provided")
	}
	if ar.QueuedOperationID == uuid.Nil {
		return errors.New("QueuedOperationID must be provided")
	}
	return nil
}

func ValidateActionResultData(ar *ActionResult) error {
	if len(ar.Data) > MaxConfigDataLength {
		return errors.Errorf("Data length %d exceeds max length %d", len(ar.Data), MaxConfigDataLength)
	}
	if len(ar.LiveState) > MaxConfigDataLength {
		return errors.Errorf("LiveState length %d exceeds max length %d", len(ar.LiveState), MaxConfigDataLength)
	}
	if len(ar.Outputs) > MaxConfigDataLength {
		return errors.Errorf("Outputs length %d exceeds max length %d", len(ar.Outputs), MaxConfigDataLength)
	}
	return nil
}
