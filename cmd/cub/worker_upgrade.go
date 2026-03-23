// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/spf13/cobra"
)

var workerUpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade worker image to match server version",
	Long: getCommandHelp(`Upgrade the worker container image to the latest release.

This command updates the worker container image reference to the latest release. 
You can upgrade either a local configuration file or a unit stored in ConfigHub.

Exactly one of --filename or --unit must be specified.

Examples:

  # Upgrade worker in a local file
  cub worker upgrade --filename worker.yaml

  # Upgrade worker in a unit
  cub worker upgrade --space my-space --unit my-worker-unit

The command uses the "get-image-reference" function to check the current image
and "set-image-reference" function to update it if needed. The container name
used is "worker".
	`, ""),
	RunE:          workerUpgradeCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var workerUpgradeArgs struct {
	filename  string
	unitSlug  string
	reference string
	dryRun    bool
}

func init() {
	workerUpgradeCmd.Flags().StringVar(&workerUpgradeArgs.filename, "filename", "", "local file containing worker configuration")
	workerUpgradeCmd.Flags().StringVar(&workerUpgradeArgs.unitSlug, "unit", "", "unit slug containing worker configuration")
	workerUpgradeCmd.Flags().StringVar(&workerUpgradeArgs.reference, "reference", "", "target image reference (e.g., :v1.0); overrides server version")
	workerUpgradeCmd.Flags().BoolVar(&workerUpgradeArgs.dryRun, "dry-run", false, "preview changes without applying them")
	addSpaceFlags(workerUpgradeCmd)
	enableWaitFlag(workerUpgradeCmd)
	enableDisplayMutationsFlag(workerUpgradeCmd)

	workerCmd.AddCommand(workerUpgradeCmd)
}

func workerUpgradeCmdRun(cmd *cobra.Command, args []string) error {
	// Validate that exactly one of filename or unit is specified
	if (workerUpgradeArgs.filename == "" && workerUpgradeArgs.unitSlug == "") ||
		(workerUpgradeArgs.filename != "" && workerUpgradeArgs.unitSlug != "") {
		return fmt.Errorf("exactly one of --filename or --unit must be specified")
	}

	// Get the target image reference: --reference flag overrides server version
	var targetImageReference string
	if workerUpgradeArgs.reference != "" {
		targetImageReference = workerUpgradeArgs.reference
	} else {
		targetImageReference = getWorkerReference()
	}

	if workerUpgradeArgs.filename != "" {
		return upgradeWorkerInFile(workerUpgradeArgs.filename, targetImageReference)
	}

	return upgradeWorkerInUnit(workerUpgradeArgs.unitSlug, targetImageReference)
}

func upgradeWorkerInFile(filename string, targetImageReference string) error {
	// Read the file content
	configData, err := fetchContent(filename)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Get the current image reference using get-image-reference function
	getCurrentArgs := []string{"worker"}
	getCurrentResp, err := invokeLocalFunction(configData, "get-image-reference", getCurrentArgs, string(workerapi.ToolchainKubernetesYAML))
	if err != nil {
		return fmt.Errorf("failed to get current image reference: %w", err)
	}

	if !getCurrentResp.Success {
		return fmt.Errorf("get-image-reference failed: %v", getCurrentResp.ErrorMessages)
	}

	// Extract the current image reference from the output
	currentImageReference, err := extractImageReferenceFromOutput(getCurrentResp)
	if err != nil {
		return fmt.Errorf("failed to extract current image reference: %w", err)
	}

	// Check if update is needed
	if currentImageReference == targetImageReference {
		fmt.Printf("Worker image is already at version %s - no update needed\n", targetImageReference)
		return nil
	}

	// Set the new image reference using set-image-reference function
	setImageArgs := []string{"worker", targetImageReference}
	setImageResp, err := invokeLocalFunction(configData, "set-image-reference", setImageArgs, string(workerapi.ToolchainKubernetesYAML))
	if err != nil {
		return fmt.Errorf("failed to set image reference: %w", err)
	}

	if !setImageResp.Success {
		return fmt.Errorf("set-image-reference failed: %v", setImageResp.ErrorMessages)
	}

	// Write the updated configuration back to the file
	err = os.WriteFile(filename, setImageResp.ConfigData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write updated file: %w", err)
	}

	fmt.Printf("Successfully upgraded worker image from %s to %s in %s\n", currentImageReference, targetImageReference, filename)
	return nil
}

func upgradeWorkerInUnit(unitSlug string, targetImageReference string) error {
	// Initialize function invocations request for get-image-reference
	whereClause := "Slug='" + unitSlug + "'"
	getCurrentArgs := []string{"get-image-reference", "worker"}

	getCurrentReq, err := initializeFunctionInvocationsRequest(getCurrentArgs)
	if err != nil {
		return fmt.Errorf("failed to initialize get-image-reference request: %w", err)
	}

	// Invoke get-image-reference on the unit
	invokeGetArgs := &invokeArgs{
		Where:  whereClause,
		Body:   getCurrentReq,
		DryRun: workerUpgradeArgs.dryRun,
	}

	getCurrentResp, err := invokeFunctionsOnUnits(invokeGetArgs)
	if err != nil {
		return fmt.Errorf("failed to get current image reference: %w", err)
	}

	if getCurrentResp == nil || len(*getCurrentResp) == 0 {
		// Check if the unit exists to provide a better error message
		_, unitErr := apiGetUnitFromSlug(unitSlug, "UnitID")
		if unitErr != nil {
			return fmt.Errorf("unit '%s' not found in space", unitSlug)
		}
		return fmt.Errorf("no response from get-image-reference on unit '%s'", unitSlug)
	}

	// Check the first response
	firstResp := (*getCurrentResp)[0]
	if !firstResp.Success {
		errorMsg := "unknown error"
		if firstResp.Error != nil {
			errorMsg = firstResp.Error.Message
		}
		return fmt.Errorf("get-image-reference failed: %s", errorMsg)
	}

	// Extract the current image reference from the output
	currentImageReference, err := extractImageReferenceFromUnitOutput(&firstResp)
	if err != nil {
		return fmt.Errorf("failed to extract current image reference: %w", err)
	}

	// Check if update is needed
	if currentImageReference == targetImageReference {
		fmt.Printf("Worker image is already at version %s - no update needed\n", targetImageReference)
		return nil
	}

	// Save prior HeadMutationNums if displaying mutations
	var priorHeadMutationNums map[string]priorUnitInfo
	if displayMutations {
		priorHeadMutationNums = savePriorUnitInfoFromWhere(whereClause, "")
	}

	// Set the new image reference using set-image-reference function
	setImageArgs := []string{"set-image-reference", "worker", targetImageReference}
	setImageReq, err := initializeFunctionInvocationsRequest(setImageArgs)
	if err != nil {
		return fmt.Errorf("failed to initialize set-image-reference request: %w", err)
	}

	// Invoke set-image-reference on the unit
	invokeSetArgs := &invokeArgs{
		Where:  whereClause,
		Body:   setImageReq,
		DryRun: workerUpgradeArgs.dryRun,
	}

	setImageResp, err := invokeFunctionsOnUnits(invokeSetArgs)
	if err != nil {
		return fmt.Errorf("failed to set image reference: %w", err)
	}

	if setImageResp == nil || len(*setImageResp) == 0 {
		return fmt.Errorf("no response from set-image-reference")
	}

	// Check the first response
	firstSetResp := (*setImageResp)[0]
	if !firstSetResp.Success {
		errorMsg := "unknown error"
		if firstSetResp.Error != nil {
			errorMsg = firstSetResp.Error.Message
		}
		return fmt.Errorf("set-image-reference failed: %s", errorMsg)
	}

	if workerUpgradeArgs.dryRun {
		if displayMutations {
			displayMutationsFromFunctionResponse(setImageResp, true, priorHeadMutationNums, "set-image-reference")
		} else {
			dataOnly = true
			outputFunctionInvocationResponse(setImageResp)
		}
		return nil
	}

	// Wait for triggers if requested
	if wait {
		unitDetails, err := apiGetUnitInSpace(firstSetResp.UnitID.String(), firstSetResp.SpaceID.String(), "*")
		if err != nil {
			return fmt.Errorf("failed to get unit details: %w", err)
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}

	if displayMutations {
		displayMutationsFromFunctionResponse(setImageResp, false, priorHeadMutationNums, "set-image-reference")
	}

	fmt.Printf("Successfully upgraded worker image from %s to %s in unit %s\n", currentImageReference, targetImageReference, unitSlug)
	return nil
}

// extractImageReferenceFromAttributeList extracts the image reference from an AttributeValueList
// The list should contain exactly one element with a string value that is either ":tag" or ""
func extractImageReferenceFromAttributeList(attrList api.AttributeValueList) (string, error) {
	if len(attrList) != 1 {
		return "", fmt.Errorf("expected exactly one attribute in list, got %d", len(attrList))
	}

	strValue, ok := attrList[0].Value.(string)
	if !ok {
		return "", fmt.Errorf("expected string value, got %T", attrList[0].Value)
	}

	return strValue, nil
}

// extractImageReferenceFromOutput extracts the image reference from local function output
func extractImageReferenceFromOutput(resp *api.FunctionInvocationResponse) (string, error) {
	outputData, exists := resp.Outputs[api.OutputTypeAttributeValueList]
	if !exists {
		return "", fmt.Errorf("AttributeValueList output not found")
	}

	var attrList api.AttributeValueList
	if err := json.Unmarshal(outputData, &attrList); err != nil {
		return "", fmt.Errorf("failed to unmarshal attribute value list: %w", err)
	}

	return extractImageReferenceFromAttributeList(attrList)
}

// extractImageReferenceFromUnitOutput extracts the image reference from unit function output
func extractImageReferenceFromUnitOutput(resp *goclientnew.FunctionInvocationsResponse) (string, error) {
	outputData, exists := resp.Outputs[string(api.OutputTypeAttributeValueList)]
	if !exists {
		return "", fmt.Errorf("AttributeValueList output not found")
	}

	// Decode the base64 output
	decodedData, err := base64.StdEncoding.DecodeString(outputData)
	if err != nil {
		return "", fmt.Errorf("failed to decode output data: %w", err)
	}

	var attrList api.AttributeValueList
	if err := json.Unmarshal(decodedData, &attrList); err != nil {
		return "", fmt.Errorf("failed to unmarshal attribute value list: %w", err)
	}

	return extractImageReferenceFromAttributeList(attrList)
}
