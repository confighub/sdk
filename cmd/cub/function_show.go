// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// ShowSection selects which sub-payload of a FunctionInvocationsResponse
// is the subject of display. --show is valid only on function commands.
// Orthogonal to -o/--output, which formats the selected sub-payload.
type ShowSection int

const (
	ShowDefault ShowSection = iota // command-default human summary
	ShowOutput                     // Outputs section
	ShowValues                     // AttributeValueList Value fields only
	ShowData                       // modified ConfigData
)

// showSection is the raw value of --show; parsed by effectiveShow.
var showSection string

func parseShowSection(s string) (ShowSection, error) {
	switch strings.TrimSpace(s) {
	case "":
		return ShowDefault, nil
	case "output":
		return ShowOutput, nil
	case "values":
		return ShowValues, nil
	case "data":
		return ShowData, nil
	default:
		return ShowDefault, fmt.Errorf("unknown --show value %q; valid: output, values, data", s)
	}
}

// effectiveShow returns the ShowSection to display. --show wins if set;
// otherwise the deprecated flags --data-only / --output-only / --output-json /
// --output-jq / --output-values-only are considered.
func effectiveShow() ShowSection {
	if showSection != "" {
		s, err := parseShowSection(showSection)
		failOnError(err)
		return s
	}
	if dataOnly {
		return ShowData
	}
	if outputValuesOnly {
		return ShowValues
	}
	if outputOnly || outputRaw || outputJQ != "" {
		return ShowOutput
	}
	return ShowDefault
}

// enableShowFlag registers --show on a function command.
func enableShowFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&showSection, "show", "",
		"Select which part of the function response to display. One of: output, values, data")
}

// renderFunctionResponse displays a FunctionInvocationsResponse list honoring
// both --show and -o. Returns true if an alternative path was taken and the
// caller should skip the default human-summary renderer.
func renderFunctionResponse(resp *[]goclientnew.FunctionInvocationsResponse) bool {
	show := effectiveShow()
	spec := effectiveOutput()

	// If -o is set without --show, it formats the whole response.
	if show == ShowDefault && spec.Kind != OutputDefault {
		return renderPayload(resp)
	}

	switch show {
	case ShowDefault:
		return false

	case ShowOutput:
		renderFunctionOutputs(resp, spec)
		return true

	case ShowValues:
		renderFunctionValues(resp)
		return true

	case ShowData:
		renderFunctionData(resp)
		return true
	}
	return false
}

// renderFunctionOutputs emits the Outputs section of each response.
// With -o jq=<expr>, applies jq to each output blob. With -o json, emits
// decoded JSON per output. Default: pretty-prints using existing formatters.
func renderFunctionOutputs(resp *[]goclientnew.FunctionInvocationsResponse, spec OutputSpec) {
	for i := range *resp {
		r := &(*resp)[i]
		for outputType, outputData := range r.Outputs {
			if len(outputData) == 0 {
				continue
			}
			outputBytes, err := base64.StdEncoding.DecodeString(outputData)
			if err != nil {
				tprintRaw(outputData)
				failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
			}
			if strings.TrimSpace(string(outputBytes)) == "null" {
				continue
			}
			switch spec.Kind {
			case OutputJQ:
				displayJQForBytes(outputBytes, spec.Arg)
			case OutputJSON:
				// Pretty-print decoded bytes as JSON.
				var pretty any
				if err := json.Unmarshal(outputBytes, &pretty); err == nil {
					displayJSON(pretty)
				} else {
					tprintRaw(string(outputBytes))
				}
			case OutputYAML:
				var parsed any
				if err := json.Unmarshal(outputBytes, &parsed); err == nil {
					displayYAML(parsed)
				} else {
					tprintRaw(string(outputBytes))
				}
			case OutputYQ:
				// YAML-first path: re-render as YAML then yq.
				var parsed any
				if err := json.Unmarshal(outputBytes, &parsed); err == nil {
					displayYQWith(parsed, spec.Arg)
				} else {
					tprintRaw(string(outputBytes))
				}
			default:
				if len(r.Outputs) > 1 {
					tprintRaw(fmt.Sprintf("%s:\n", outputType))
				}
				renderOutputByType(outputType, outputBytes, false)
			}
		}
	}
}

// renderOutputByType mirrors the per-output-type rendering in
// outputFunctionInvocationResponse so --show output can emit the same
// human-readable blocks without the surrounding summary.
func renderOutputByType(outputType string, outputBytes []byte, outputRawFlag bool) {
	switch outputType {
	case string(api.OutputTypeYAML):
		var payload api.YAMLPayload
		if err := json.Unmarshal(outputBytes, &payload); err != nil || outputRawFlag {
			tprintRaw(string(outputBytes))
		} else {
			tprintRaw(payload.Payload)
		}
	case string(api.OutputTypeAttributeValueList):
		var payload api.AttributeValueList
		if err := json.Unmarshal(outputBytes, &payload); err != nil {
			tprintRaw(string(outputBytes))
		} else {
			for i := range payload {
				funcDisplay := functionDisplayName(payload[i].FunctionName, payload[i].Index)
				tprint("Function: %s", funcDisplay)
				displayAttributeValue(&payload[i])
			}
		}
	case string(api.OutputTypeValidationResultList), string(api.OutputTypeValidationResult):
		var payload api.ValidationResultList
		if err := json.Unmarshal(outputBytes, &payload); err != nil {
			tprintRaw(string(outputBytes))
		} else {
			for i := range payload {
				displayValidationResult(&payload[i])
			}
		}
	case string(api.OutputTypeResourceInfoList):
		var payload api.ResourceInfoList
		if err := json.Unmarshal(outputBytes, &payload); err != nil {
			tprintRaw(string(outputBytes))
		} else {
			for i := range payload {
				displayResourceInfo(&payload[i])
			}
		}
	case string(api.OutputTypeResourceList):
		var payload api.ResourceList
		if err := json.Unmarshal(outputBytes, &payload); err != nil {
			tprintRaw(string(outputBytes))
		} else {
			for i := range payload {
				displayResource(&payload[i])
			}
		}
	default:
		tprintRaw(string(outputBytes))
	}
}

// renderFunctionValues emits only the Value field from AttributeValueList
// outputs. Mirrors --output-values-only.
func renderFunctionValues(resp *[]goclientnew.FunctionInvocationsResponse) {
	for i := range *resp {
		r := &(*resp)[i]
		attributeOutput, exists := r.Outputs[string(api.OutputTypeAttributeValueList)]
		if !exists || len(attributeOutput) == 0 {
			continue
		}
		outputBytes, err := base64.StdEncoding.DecodeString(attributeOutput)
		if err != nil {
			tprintRaw(attributeOutput)
			failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
		}
		if strings.TrimSpace(string(outputBytes)) == "null" {
			continue
		}
		var attrValueList api.AttributeValueList
		if err := json.Unmarshal(outputBytes, &attrValueList); err != nil {
			tprintRaw(string(outputBytes))
			failOnError(fmt.Errorf("%s: Failed to decode output as AttributeValueList", err.Error()))
		}
		for j := range attrValueList {
			tprint("%v", attrValueList[j].Value)
		}
	}
}

// renderFunctionData emits the modified ConfigData field of each response.
// When -O/--output-file is set, each unit's data is written to a path derived
// from the template (supports {space}, {unit}, {section}). Otherwise, stdout
// receives the raw bytes; when multiple units are present, each block is
// prefixed with "# unit: <space>/<slug>" for disambiguation.
func renderFunctionData(resp *[]goclientnew.FunctionInvocationsResponse) {
	multi := resp != nil && len(*resp) > 1
	for i := range *resp {
		r := &(*resp)[i]
		if len(r.ConfigData) == 0 {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(r.ConfigData)
		if err != nil {
			failOnError(fmt.Errorf("%s: Failed to decode config data", err.Error()))
		}
		if outputFile != "" {
			space := r.SpaceSlug
			if space == "" {
				space = r.SpaceID.String()
			}
			unit := r.UnitSlug
			if unit == "" {
				unit = r.UnitID.String()
			}
			failOnError(writeOrPrint(data, space, unit, "data"))
			continue
		}
		if multi && !quiet {
			tprintRaw(fmt.Sprintf("# unit: %s", unitDisplayName(r)))
		}
		tprintRaw(string(data))
	}
}
