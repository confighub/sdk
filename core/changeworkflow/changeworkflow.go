// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package changeworkflow

import (
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

const (
	changeWorkflowAPIVersion = "confighub.com/v1"
	changeWorkflowKind       = "ChangeWorkflow"
)

// ChangeWorkflow is a KRM object, so it takes its apiVersion, kind and metadata
// from kyaml's ResourceMeta rather than restating them. The embedded fields are
// promoted, which is what flattens them back to the top level of the document:
// encoding/json, which sigs.k8s.io/yaml decodes through, promotes anonymous
// struct fields that carry no name of their own. The ",inline" tag is how kyaml
// spells that intent, but it is gopkg.in/yaml.v3's option, not one encoding/json
// reads.
type ChangeWorkflow struct {
	yaml.ResourceMeta `json:",inline" yaml:",inline"`

	Spec ChangeWorkflowSpec `json:"spec" yaml:"spec"`
}

type ChangeWorkflowSpec struct {
	Source ChangeWorkflowSource  `json:"source" yaml:"source"`
	Stages []ChangeWorkflowStage `json:"stages" yaml:"stages"`
}

type ChangeWorkflowSource struct {
	Space string `json:"space" yaml:"space"`
}

type ChangeWorkflowStage struct {
	Name          string   `json:"name" yaml:"name"`
	WhereSpace    string   `json:"whereSpace" yaml:"whereSpace"`
	Prerequisites []string `json:"prerequisites" yaml:"prerequisites"`
}
