// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package changeworkflow

import (
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

const (
	APIVersion = "confighub.com/v1"
	Kind       = "ChangeWorkflow"
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
	Stages []ChangeWorkflowStage    `json:"stages" yaml:"stages"`
	Final  ChangeWorkflowFinalStage `json:"final" yaml:"final"`
}

type ChangeWorkflowStage struct {
	Name          string   `json:"name" yaml:"name"`
	WhereSpace    string   `json:"whereSpace" yaml:"whereSpace"`
	Prerequisites []string `json:"prerequisites,omitempty" yaml:"prerequisites,omitempty"`
}

type ChangeWorkflowFinalStage struct {
	Prerequisites []string `json:"prerequisites,omitempty" yaml:"prerequisites,omitempty"`
}
