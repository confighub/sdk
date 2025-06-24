// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"github.com/confighub/sdk/workerapi"
	"github.com/google/uuid"
)

type BridgeWorkerEventRequest struct {
	Action  ActionType `description:"The action requested"`
	Payload BridgeWorkerPayload
}

// BridgeWorkerPayload is an input for the Bridgeworker
type BridgeWorkerPayload struct {
	QueuedOperationID uuid.UUID               `description:"UUID of the operation corresponding to the requested action"`
	ToolchainType     workerapi.ToolchainType `description:"ToolchainType of the Unit on which the action was performed"`
	ProviderType      ProviderType            `description:"ProviderType of the Target attached to the Unit on which the action was performed"`
	UnitSlug          string                  `description:"Slug of the Unit on which the action was performed"`
	UnitID            uuid.UUID               `description:"UUID of the Unit on which the action was performed"`
	SpaceID           uuid.UUID               `description:"UUID of the Space of the Unit on which the action was performed"`
	Data              []byte                  `swaggertype:"string" format:"byte" description:"Configuration data of the Unit on which the action was performed"`
	LiveState         []byte                  `swaggertype:"string" format:"byte" description:"Live state corresponding to the Unit"`
	TargetParams      []byte                  `swaggertype:"string" format:"byte" description:"Parameters of the Target attached to the Unit on which the action was performed"`
	ExtraParams       []byte                  `swaggertype:"string" format:"byte" description:"Additional parameters associated with the action sent to the worker"`
	RevisionNum       int64                   `description:"Sequence number of the revision of the Unit on which the action was performed"`
}
