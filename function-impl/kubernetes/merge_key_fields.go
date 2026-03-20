// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
)

// MergeKeyField is an alias for k8skit.MergeKeyField for backward compatibility.
type MergeKeyField = k8skit.MergeKeyField

// StrategicMergeKeyFields, PodSpecMergeKeyFields, and WorkloadMergeKeyFields are
// defined in k8skit. These variables are retained as aliases for any code that
// references them from this package.
var StrategicMergeKeyFields = k8skit.StrategicMergeKeyFields
var PodSpecMergeKeyFields = k8skit.PodSpecMergeKeyFields
var WorkloadMergeKeyFields map[api.ResourceType]string = k8skit.WorkloadMergeKeyFields
