// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// This file defines a set of experimental aliases for the purpose of getting a feel for
// the best naming conventions for the SDK.
package api

import "github.com/confighub/sdk/workerapi"

// Core interface aliases
type Bridge = BridgeWorker
type BridgeContext = BridgeWorkerContext
type BridgePayload = BridgeWorkerPayload

// Configuration type aliases
type Toolchain = workerapi.ToolchainType

// Information and options aliases
type BridgeInfo = BridgeWorkerInfo

// Action and result aliases
type Action = ActionType
type ActionResultMeta = ActionResultBaseMeta
type ActionStatus = ActionStatusType
