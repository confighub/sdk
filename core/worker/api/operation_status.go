// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	funcApi "github.com/confighub/sdk/core/function/api"
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
	// ResourceStatuses contains per-resource sync and readiness status.
	// Key format: "apiVersion/kind#namespace/name" (e.g., "apps/v1/Deployment#default/my-app")
	ResourceStatuses ResourceStatusMap `json:",omitempty" description:"Per-resource sync and readiness status"`
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

// ResourceSyncStatusType represents the sync status of an individual resource.
// Sync status indicates whether the configuration has been pushed to the target.
type ResourceSyncStatusType string

const (
	// ResourceSyncStatusSynced indicates the config was successfully pushed to the target
	ResourceSyncStatusSynced ResourceSyncStatusType = "Synced"
	// ResourceSyncStatusPending indicates the resource is waiting to be synced
	ResourceSyncStatusPending ResourceSyncStatusType = "Pending"
	// ResourceSyncStatusFailed indicates the sync operation failed
	ResourceSyncStatusFailed ResourceSyncStatusType = "Failed"
)

// ResourceReadinessType represents the readiness status of an individual resource.
// Readiness status is derived from kstatus polling and indicates whether the resource
// has reached a ready/healthy state in the target system.
type ResourceReadinessType string

const (
	// ResourceReadinessReady indicates the resource is ready/healthy
	ResourceReadinessReady ResourceReadinessType = "Ready"
	// ResourceReadinessInProgress indicates the resource is progressing towards ready state
	ResourceReadinessInProgress ResourceReadinessType = "InProgress"
	// ResourceReadinessStuck indicates the resource has been InProgress without
	// observed progress for longer than the staleness threshold. Its controller
	// may be suspended, missing, broken, or backpressured. The resource is not
	// failed; if progress resumes it transitions back to InProgress.
	ResourceReadinessStuck ResourceReadinessType = "Stuck"
	// ResourceReadinessFailed indicates the resource failed to reach ready state
	ResourceReadinessFailed ResourceReadinessType = "Failed"
	// ResourceReadinessTerminating indicates the resource is being deleted
	ResourceReadinessTerminating ResourceReadinessType = "Terminating"
	// ResourceReadinessUnknown indicates the resource readiness cannot be determined
	ResourceReadinessUnknown ResourceReadinessType = "Unknown"
)

// ResourceStatus represents the sync and readiness status of a single resource.
// It tracks both whether configuration was pushed (SyncStatus) and whether the
// resource has become healthy/ready (Readiness), along with a timestamp for
// tracking progress duration.
type ResourceStatus struct {
	// SyncStatus indicates whether config was pushed to the target
	SyncStatus ResourceSyncStatusType `json:",omitempty" description:"Whether config was pushed to the target (Synced or NotSynced)"`
	// Readiness indicates the health/ready state from kstatus
	Readiness ResourceReadinessType `json:",omitempty" description:"Health state from kstatus (Ready, InProgress, Failed, Unknown)"`
	// Message provides human-readable status details
	Message string `json:",omitempty" description:"Human-readable status details or error message"`
	// UpdatedAt is the timestamp when this resource status was last updated
	UpdatedAt time.Time `json:",omitempty" description:"Timestamp when this resource status was last updated"`
}

// ResourceStatusMap maps ResourceTypeAndName to ResourceStatus.
// Key format: "apiVersion/kind#namespace/name" following K8sResourceProviderType conventions
// Examples: "apps/v1/Deployment#default/my-app", "v1/ConfigMap#/my-config"
// This format includes namespace to distinguish resources with the same name in different namespaces.
type ResourceStatusMap map[funcApi.ResourceTypeAndName]ResourceStatus
