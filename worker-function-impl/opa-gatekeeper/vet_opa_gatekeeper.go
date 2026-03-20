// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package opagatekeeper

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	admissionwebhook "github.com/confighub/sdk/worker-function-impl/k8s-admission-webhook"
)

// Environment variable names for OPA Gatekeeper configuration.
const (
	envGatekeeperURL           = "GATEKEEPER_URL"
	envGatekeeperCACertPath    = "GATEKEEPER_CA_CERT_PATH"
	envGatekeeperSkipTLSVerify = "GATEKEEPER_SKIP_TLS_VERIFY"
)

// gatekeeperVWCName is the standard name of the Gatekeeper
// ValidatingWebhookConfiguration.
const gatekeeperVWCName = "gatekeeper-validating-webhook-configuration"

// gatekeeperLabelSelector is the label selector for Gatekeeper-managed VWCs.
const gatekeeperLabelSelector = "gatekeeper.sh/system%3Dyes"

// Package-level state initialized once by InitOPAGatekeeper.
var (
	gatekeeperWebhookClient *admissionwebhook.WebhookClient
	gatekeeperWatcher       *admissionwebhook.WebhookWatcher
)

// InitOPAGatekeeper initializes the webhook client and starts watching for
// Gatekeeper webhook configuration changes. It should be called once at startup.
func InitOPAGatekeeper() error {
	client, err := admissionwebhook.NewWebhookClient(envGatekeeperURL, envGatekeeperCACertPath, envGatekeeperSkipTLSVerify)
	if err != nil {
		return err
	}

	discoveryClient, err := admissionwebhook.NewK8sDiscoveryClient()
	if err != nil {
		return errors.Wrap(err, "failed to create K8s discovery client")
	}

	selector := admissionwebhook.WebhookSelector{
		LabelSelector: gatekeeperLabelSelector,
		ConfigNames:   []string{gatekeeperVWCName},
	}

	watcher := admissionwebhook.NewWebhookWatcher(discoveryClient, selector)
	if err := watcher.Start(context.Background()); err != nil {
		return errors.Wrap(err, "failed to start Gatekeeper webhook watcher")
	}

	gatekeeperWebhookClient = client
	gatekeeperWatcher = watcher

	slog.Info("OPA Gatekeeper initialized", "webhooks", len(watcher.GetEndpoints()))
	return nil
}

// GetVetOPAGatekeeperSignature returns the function signature for vet-opa-gatekeeper.
func GetVetOPAGatekeeperSignature() api.FunctionSignature {
	return api.FunctionSignature{
		FunctionName:       "vet-opa-gatekeeper",
		Parameters:         []api.FunctionParameter{},
		RequiredParameters: 0,
		OutputInfo: &api.FunctionOutput{
			ResultName:  "passed",
			Description: "True if all resources pass OPA Gatekeeper constraint validation, false otherwise",
			OutputType:  api.OutputTypeValidationResult,
		},
		Mutating:              false,
		Validating:            true,
		Hermetic:              false,
		Idempotent:            true,
		Description:           "Validates Kubernetes resources against OPA Gatekeeper constraints by sending AdmissionReview requests to a running Gatekeeper server. Constraint templates and constraints must be deployed in the Gatekeeper cluster.",
		FunctionType:          api.FunctionTypeCustom,
		AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
	}
}

// VetOPAGatekeeperFunction validates Kubernetes resources against a running OPA Gatekeeper.
func VetOPAGatekeeperFunction(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
	if gatekeeperWebhookClient == nil || gatekeeperWatcher == nil {
		return fArgs.ParsedData, nil, errors.New("OPA Gatekeeper not initialized; call InitOPAGatekeeper first")
	}

	webhooks := gatekeeperWatcher.GetEndpoints()

	slog.Info("using Gatekeeper webhooks", "count", len(webhooks))

	return admissionwebhook.ValidateResources(gatekeeperWebhookClient, webhooks, k8skit.NewK8sResourceProvider(), fArgs.ParsedData, gatekeeperResponseConverter)
}

// gatekeeperViolation represents a parsed violation from a Gatekeeper response message.
type gatekeeperViolation struct {
	constraintName string
	message        string
}

// gatekeeperResponseConverter converts a Gatekeeper AdmissionResponse into validation
// details and failed attributes.
func gatekeeperResponseConverter(resp *admissionwebhook.AdmissionResponse, resourceInfo api.ResourceInfo) ([]string, []api.AttributeValue) {
	var details []string
	var failedAttrs []api.AttributeValue

	if resp.Status != nil && resp.Status.Message != "" {
		violations := parseGatekeeperMessage(resp.Status.Message)
		for _, v := range violations {
			detail := fmt.Sprintf("constraint %q: %s", v.constraintName, v.message)
			details = append(details, detail)
			failedAttrs = append(failedAttrs, api.AttributeValue{
				AttributeInfo: api.AttributeInfo{
					AttributeIdentifier: api.AttributeIdentifier{
						ResourceInfo: resourceInfo,
					},
					AttributeMetadata: api.AttributeMetadata{
						AttributeName: api.AttributeNameNone,
					},
				},
				Issues: []api.Issue{
					{
						Identifier: v.constraintName,
						Message:    v.message,
					},
				},
			})
		}
		if len(violations) == 0 {
			// Couldn't parse structured violations, use raw message.
			details = append(details, resp.Status.Message)
		}
	}

	return details, failedAttrs
}

// constraintMessageRE matches Gatekeeper's "[constraint-name] message" format.
var constraintMessageRE = regexp.MustCompile(`^\[(\S+)\]\s+(.+)$`)

// parseGatekeeperMessage parses validation failure messages from OPA Gatekeeper.
// Gatekeeper returns messages in the format:
//
//	[constraint-name] violation message
//
// Multiple violations are separated by newlines.
func parseGatekeeperMessage(msg string) []gatekeeperViolation {
	var violations []gatekeeperViolation

	for _, line := range strings.Split(msg, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if m := constraintMessageRE.FindStringSubmatch(trimmed); m != nil {
			violations = append(violations, gatekeeperViolation{
				constraintName: m[1],
				message:        m[2],
			})
		}
	}
	return violations
}
