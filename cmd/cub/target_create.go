// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/cubapi"
	funcapi "github.com/confighub/sdk/function/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetCreateCmd = &cobra.Command{
	Use:   "create <slug> <parameters> [worker-slug]",
	Short: "Create a new target",
	Long: getCommandHelp(`Create a new target with the specified slug and optional parameters and worker slug.
Parameters are optional and can be used to pass additional configuration data to the target.
Parameters are passed as a JSON string.

Example:
`+"```"+`
  "{\"KubeContext\":\"kind-space17005\",\"KubeNamespace\":\"default\",\"WaitTimeout\":\"2m0s\"}"
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(1, 3),
	RunE: targetCreateCmdRun,
}

var fromTarget string
var fromTargetSpace string
var providerType string
var toolchainType string

func init() {
	addStandardCreateFlags(targetCreateCmd)
	targetCreateCmd.Flags().StringVarP(&providerType, "provider", "p", "Kubernetes", "The type of provider for the target.\nDefault is Kubernetes.\n\t(e.g., Kubernetes, Terraform, FluxOCIWriter)")
	targetCreateCmd.Flags().StringVarP(&toolchainType, "toolchain", "t", "Kubernetes/YAML", "The type of toolchain for the target.\nDefault is Kubernetes/YAML.\n\t(e.g., Kubernetes/YAML, Terraform)")
	// TODO: Remove client-side copying now that server-side bulk create exists
	targetCreateCmd.Flags().StringVar(&fromTarget, "from-target", "", "target to copy from another space")
	targetCreateCmd.Flags().StringVar(&fromTargetSpace, "from-target-space", "", "space of target to copy")
	targetCmd.AddCommand(targetCreateCmd)
}

func targetCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}
	// Validate no label removal
	if err := ValidateLabelRemoval(label, false); err != nil {
		return err
	}
	// Validate no delete gate removal
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	newTarget := goclientnew.Target{}

	if fromTarget != "" || fromTargetSpace != "" {
		if fromTarget == "" || fromTargetSpace == "" {
			return errors.New("both of --from-target and --from-target-space must be specified")
		}
		if flagPopulateModelFromStdin || flagFilename != "" {
			return errors.New("only one of --from-target or --from-stdin/--filename may be specified")
		}
		ftSpace, err := apiGetSpaceFromSlug(fromTargetSpace, "*") // get all fields for now
		if err != nil {
			return err
		}
		ftTarget, err := apiGetTargetFromSlug(fromTarget, ftSpace.SpaceID.String(), "*") // get all fields for copy
		if err != nil {
			return err
		}
		newTarget = *ftTarget.Target
	}

	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newTarget); err != nil {
			return err
		}
	}

	// set toolchainType and providerType if not copying from another target or stdin
	if fromTarget == "" && fromTargetSpace == "" && !flagPopulateModelFromStdin && flagFilename == "" {
		newTarget.ToolchainType = toolchainType
		newTarget.ProviderType = providerType
	}

	err := validateToolchainAndProvider(newTarget.ToolchainType, newTarget.ProviderType)
	if err != nil {
		return err
	}

	err = setLabels(&newTarget.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newTarget.DeleteGates)
	if err != nil {
		return err
	}
	newTarget.SpaceID = spaceID
	newTarget.Slug = makeSlug(args[0])
	if newTarget.DisplayName == "" {
		newTarget.DisplayName = args[0]
	}
	if len(args) >= 2 {
		newTarget.Parameters = args[1]
	}
	if len(args) == 3 {
		worker, err := apiGetBridgeWorkerFromSlug(args[2], "*") // get all fields for now
		if err != nil {
			return err
		}
		workerID := worker.BridgeWorkerID
		newTarget.BridgeWorkerID = workerID
	}

	// Create params with AllowExists if needed
	params := &goclientnew.CreateTargetParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	targetRes, err := cubClientNew.CreateTargetWithResponse(ctx, spaceID, params, newTarget)
	if cubapi.IsAPIError(err, targetRes) {
		return cubapi.InterpretErrorGeneric(err, targetRes)
	}

	targetDetails := targetRes.JSON200
	extendedDetails := &goclientnew.ExtendedTarget{Target: targetDetails}
	displayCreateResults(extendedDetails, "target", args[0], targetDetails.TargetID.String(), displayTargetDetails)
	return nil
}

func validateToolchainAndProvider(toolchainType string, providerType string) error {
	// Ensure toolchainType and providerType are set and valid. Should never be empty but just in case.
	if toolchainType == "" || providerType == "" {
		return errors.New("toolchain and provider must be specified")
	}
	if !funcapi.IsSupportedToolchain(workerapi.ToolchainType(toolchainType)) {
		return errors.New("toolchain must be one of: " + funcapi.SupportedToolchainsToString())
	}
	// Technically we need to allow any provider type that a bridge implements
	// if providerType != string(api.ProviderKubernetes) &&
	// 	providerType != string(api.ProviderOpenTofuAWS) &&
	// 	providerType != string(api.ProviderFluxOCIWriter) &&
	// 	providerType != string(api.ProviderConfigMap) {
	// 	// NOTE: FluxOCI deliberately not mentioned
	// 	return errors.New("provider must be one of: Kubernetes, OpenTofu/AWS, ConfigMap")
	// }
	if providerType == string(api.ProviderOpenTofuAWS) && toolchainType != string(workerapi.ToolchainOpenTofuHCL) {
		return errors.New("provider OpenTofu/AWS requires toolchain OpenTofu/HCL")
	}
	if (providerType == string(api.ProviderKubernetes) || providerType == string(api.ProviderFluxOCIWriter)) &&
		toolchainType != string(workerapi.ToolchainKubernetesYAML) {
		return fmt.Errorf("provider %s requires toolchain Kubernetes/YAML", providerType)
	}
	if providerType == string(api.ProviderConfigMap) &&
		toolchainType != string(workerapi.ToolchainAppConfigProperties) &&
		toolchainType != string(workerapi.ToolchainAppConfigYAML) &&
		toolchainType != string(workerapi.ToolchainAppConfigTOML) &&
		toolchainType != string(workerapi.ToolchainAppConfigINI) &&
		toolchainType != string(workerapi.ToolchainAppConfigEnv) {
		// NOTE: Env deliberately not mentioned
		return errors.New("provider ConfigMap requires toolchain AppConfig/Properties, AppConfig/YAML, AppConfig/TOML, or AppConfig/INI")
	}
	return nil
}
