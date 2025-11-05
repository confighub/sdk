// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/spf13/afero"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/workerapi"
)

type OpenTofuAWSWorker struct{}

var _ api.BridgeWorker = (*OpenTofuAWSWorker)(nil)

const (
	opentofuVersion    = "1.9.0"
	awsProviderVersion = "~> 5.0"
)

type OpenTofuAWSParams struct {
	Profile string `json:",omitempty"`
	Region  string `json:",omitempty"`
}

func (p *OpenTofuAWSParams) ToMap() map[string]interface{} {
	var result map[string]interface{}
	data, _ := json.Marshal(p)
	_ = json.Unmarshal(data, &result)
	return result
}

func (w *OpenTofuAWSWorker) Info(_ api.InfoOptions) api.BridgeWorkerInfo {
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = "default"
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.ConfigType{
			{
				ToolchainType: workerapi.ToolchainOpenTofuHCL,
				ProviderType:  api.ProviderAWS,
				AvailableTargets: []api.Target{
					{
						Name: "opentofu-aws",
						Params: (&OpenTofuAWSParams{
							Profile: profile,
							Region:  region,
						}).ToMap(),
					},
				},
			},
		},
	}
}

const providerTemplateBase = `
terraform {
  required_version = "{{.TFVersion}}"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "{{.AWSVersion}}"
    }
  }
}

provider "aws" {
  profile = "{{.Profile}}"
  region  = "{{.Region}}"
}
`

type providerTemplateData struct {
	TFVersion   string
	AWSVersion  string
	Target      string
	Profile     string
	Region      string
	AccessKey   string
	SecretKey   string
	RoleARN     string
	SessionName string
	ExternalID  string
}

// TODO write tests
func prepareWorkspace(fs afero.Fs, payload api.BridgeWorkerPayload) (string, error) {
	// Create temporary directory
	workspaceDir, err := afero.TempDir(fs, os.TempDir(), "opentofu-aws-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Write main.tf
	mainTFPath := filepath.Join(workspaceDir, "main.tf")
	if err := afero.WriteFile(fs, mainTFPath, payload.Data, 0644); err != nil {
		fs.RemoveAll(workspaceDir)
		return "", fmt.Errorf("failed to write main.tf: %w", err)
	}

	params := OpenTofuAWSParams{}
	if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
		return "", err
	}

	// Create template data based on target type
	tmplData := providerTemplateData{
		TFVersion:  opentofuVersion,
		AWSVersion: awsProviderVersion,
		Profile:    params.Profile,
		Region:     params.Region,
	}

	// Parse and execute the template
	tmpl, err := template.New("provider").Parse(providerTemplateBase)
	if err != nil {
		return "", fmt.Errorf("failed to parse provider template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tmplData); err != nil {
		return "", fmt.Errorf("failed to execute provider template: %w", err)
	}

	// Write provider.tf
	providerTFPath := filepath.Join(workspaceDir, "provider.tf")
	if err := afero.WriteFile(fs, providerTFPath, buf.Bytes(), 0644); err != nil {
		fs.RemoveAll(workspaceDir)
		return "", fmt.Errorf("failed to write provider.tf: %w", err)
	}

	return workspaceDir, nil
}

// convertTofuOutputs converts tfexec.OutputMeta values to their corresponding Go types
func convertTofuOutputs(outputs map[string]*tfexec.OutputMeta) ([]byte, error) {
	simpleOutputs := make(map[string]interface{})
	for k, v := range outputs {
		// The Value field is json.RawMessage, so we need to unmarshal it
		var value interface{}
		if err := json.Unmarshal(v.Value, &value); err != nil {
			return nil, fmt.Errorf("failed to unmarshal output value: %w", err)
		}
		simpleOutputs[k] = value
	}

	// Convert outputs to JSON
	outputsJSON, err := json.Marshal(simpleOutputs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal outputs: %w", err)
	}

	return outputsJSON, nil
}

func (w *OpenTofuAWSWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	fs := afero.NewOsFs()
	workspaceDir, err := prepareWorkspace(fs, payload)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		))
		return err
	}
	defer fs.RemoveAll(workspaceDir)

	// Initialize and run OpenTofu
	tf, err := tfexec.NewTerraform(workspaceDir, "tofu")
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to create terraform executor: %v", err),
		))
		return fmt.Errorf("failed to create terraform executor: %w", err)
	}

	tfstateFilepath := filepath.Join(tf.WorkingDir(), "terraform.tfstate")

	// If we have LiveState data, we write it as the tfstate file
	if len(payload.LiveState) > 0 {
		if err := afero.WriteFile(fs, tfstateFilepath, payload.LiveState, 0644); err != nil {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("failed to write tfstate file: %v", err),
			))
			return err
		}
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to initialize OpenTofu...",
	)); err != nil {
		return err
	}

	sw := lib.NewStatusWriter(wctx, api.ActionApply)
	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	// Initialize
	if err := tf.Init(wctx.Context(), tfexec.Upgrade(false)); err != nil {
		// Ensure any remaining output is flushed before error
		if flushErr := sw.Flush(); flushErr != nil {
			return flushErr
		}
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to init terraform: %v", err),
		))
		return fmt.Errorf("failed to init terraform: %w", err)
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to apply resources...",
	)); err != nil {
		return err
	}

	// Apply
	if err := tf.Apply(wctx.Context()); err != nil {
		// Ensure any remaining output is flushed before error
		if flushErr := sw.Flush(); flushErr != nil {
			return flushErr
		}
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to apply terraform: %v", err),
		))
		return fmt.Errorf("failed to apply terraform: %w", err)
	}

	// Get outputs after successful apply
	outputs, err := tf.Output(wctx.Context())
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to get outputs: %v", err),
		))
		return fmt.Errorf("failed to get outputs: %w", err)
	}

	// Convert the map to use pointers
	ptrOutputs := make(map[string]*tfexec.OutputMeta, len(outputs))
	for k, v := range outputs {
		v := v // Create a new variable to avoid taking address of range variable
		ptrOutputs[k] = &v
	}

	outputsJSON, err := convertTofuOutputs(ptrOutputs)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to convert outputs: %v", err),
		))
		return fmt.Errorf("failed to convert outputs: %w", err)
	}

	tfstate, err := afero.ReadFile(fs, tfstateFilepath)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to read tfstate file: %v", err),
		))
		return fmt.Errorf("failed to read tfstate file: %w", err)
	}

	// Ensure final output is flushed before success
	if err := sw.Flush(); err != nil {
		return err
	}

	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Applied successfully at %s", time.Now().Format(time.RFC3339)),
	)
	status.LiveState = tfstate
	status.Outputs = outputsJSON
	return wctx.SendStatus(status)
}

func (w *OpenTofuAWSWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	fs := afero.NewOsFs()
	tmpDir, err := prepareWorkspace(fs, payload)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		))
		return err
	}
	defer fs.RemoveAll(tmpDir)

	// Initialize and run OpenTofu
	tf, err := tfexec.NewTerraform(tmpDir, "tofu")
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			fmt.Sprintf("failed to create terraform executor: %v", err),
		))
		return fmt.Errorf("failed to create terraform executor: %w", err)
	}

	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	// TODO what should be done if payload.LiveState is empty
	tfstateFilepath := filepath.Join(tf.WorkingDir(), "terraform.tfstate")

	// If we have LiveState data, we write it as the tfstate file
	if len(payload.LiveState) > 0 {
		if err := afero.WriteFile(fs, tfstateFilepath, payload.LiveState, 0644); err != nil {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultDestroyFailed,
				fmt.Sprintf("failed to write tfstate file: %v", err),
			))
			return err
		}
	} else {
		fmt.Printf("*****************************************************************\n")
		fmt.Printf("*** WARNING: Terraform state file is empty. Skipping destroy. ***\n")
		fmt.Printf("*****************************************************************\n")
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			"cannot destroy resources: no state file available",
		))
		return fmt.Errorf("cannot destroy resources without state information")
	}

	// Initialize
	if err := tf.Init(wctx.Context(), tfexec.Upgrade(false)); err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			fmt.Sprintf("failed to init terraform: %v", err),
		))
		return fmt.Errorf("failed to init terraform: %w", err)
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to destroy resources...",
	)); err != nil {
		return err
	}

	sw := lib.NewStatusWriter(wctx, api.ActionDestroy)

	// Destroy
	if err := tf.Destroy(wctx.Context()); err != nil {
		// Ensure any remaining output is flushed before error
		if flushErr := sw.Flush(); flushErr != nil {
			return flushErr
		}
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			fmt.Sprintf("failed to destroy terraform: %v", err),
		))
		return fmt.Errorf("failed to destroy terraform: %w", err)
	}

	// Ensure final output is flushed before success
	if err := sw.Flush(); err != nil {
		return err
	}

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Destroyed successfully at %s", time.Now().Format(time.RFC3339)),
	)
	result.LiveState = []byte{}
	return wctx.SendStatus(result)
}

func (w *OpenTofuAWSWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(
		api.ActionStatusNone,
		api.ActionResultNone,
		fmt.Sprintf("Refresh hasn't been implemented yet: %s", time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *OpenTofuAWSWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(
		api.ActionStatusNone,
		api.ActionResultNone,
		fmt.Sprintf("Import hasn't been implemented yet: %s", time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *OpenTofuAWSWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}
