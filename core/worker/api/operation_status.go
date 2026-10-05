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
	ActionResultNone ActionResultType = "None"

	ActionResultFunctionInvocationCompleted ActionResultType = "FunctionInvocationCompleted"
	ActionResultFunctionInvocationFailed    ActionResultType = "FunctionInvocationFailed"
)

var ValidActionResult = map[ActionResultType]bool{
	ActionResultNone:                        true,
	ActionResultFunctionInvocationCompleted: true,
	ActionResultFunctionInvocationFailed:    true,
}

type ActionType string

// Action values
const (
	ActionNA     ActionType = "N/A"
	ActionCancel ActionType = "Cancel"

	// ActionApply is no longer performed — nothing applies configuration since
	// the bridge sunset. The constant remains because historical UnitActions and
	// UnitEvents carry it. It is deliberately absent from ValidAction: it can be
	// read, not submitted.
	ActionApply ActionType = "Apply"

	ActionInvokeFunctions ActionType = "InvokeFunctions"
	ActionListFunctions   ActionType = "ListFunctions"
)

var ValidAction = map[ActionType]bool{
	ActionNA:              true,
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

const MaxActionResultMessageLength = 4096

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
	Data []byte `json:",omitempty" swaggertype:"string" format:"byte" description:"Updated configuration Data of the Unit (for refresh and import)"`
	// ErrorMessages contains warning or error messages to surface to the user.
	ErrorMessages []string `json:",omitempty" description:"Warning or error messages to surface to the user"`
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
	return nil
}
