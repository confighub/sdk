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
// multipleFunctions indicates whether more than one function was invoked; when
// false, per-output function labels are suppressed to reduce noise in the
// common single-function case.
func renderFunctionResponse(resp *[]goclientnew.FunctionInvocationsResponse, multipleFunctions bool) bool {
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
		renderFunctionOutputs(resp, spec, multipleFunctions)
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
// multipleFunctions is threaded through to renderOutputByType.
func renderFunctionOutputs(resp *[]goclientnew.FunctionInvocationsResponse, spec OutputSpec, multipleFunctions bool) {
	for i := range *resp {
		r := &(*resp)[i]
		// If invoked on just one unit, then don't print the unit in order to make it easier to process the output.
		if spec.Kind == OutputDefault && len(*resp) > 1 {
			yamlOutput := false
			if len(r.Outputs) == 1 {
				for outputType := range r.Outputs {
					if outputType == string(api.OutputTypeYAML) {
						yamlOutput = true
					}
				}
			}
			if !yamlOutput {
				tprint("Unit %s:", unitDisplayName(r))
			}
			if len(r.Outputs) == 0 {
				tprintRaw("  No output")
				continue
			}
		}
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
				envBytes, err := json.Marshal(buildFunctionOutputEnvelope(r, outputType, outputBytes))
				failOnError(err)
				displayJQForBytes(envBytes, spec.Arg)
			case OutputJSON:
				displayJSON(buildFunctionOutputEnvelope(r, outputType, outputBytes))
			case OutputYAML:
				displayYAML(buildFunctionOutputEnvelope(r, outputType, outputBytes))
			case OutputYQ:
				displayYQWith(buildFunctionOutputEnvelope(r, outputType, outputBytes), spec.Arg)
			default:
				if len(r.Outputs) > 1 {
					tprintRaw(fmt.Sprintf("%s:\n", outputType))
				}
				renderOutputByType(outputType, outputBytes, false, i, multipleFunctions)
			}
		}
	}
}

// buildFunctionOutputEnvelope wraps a single function output with the
// identity of the unit it came from, so structured formatters (-o jq/json/
// yaml/yq) can correlate the result back to its Space and Unit. Output holds
// the parsed JSON value when outputBytes parse as JSON; otherwise it holds
// the raw string so downstream formatters still see a well-formed document.
func buildFunctionOutputEnvelope(r *goclientnew.FunctionInvocationsResponse, outputType string, outputBytes []byte) map[string]any {
	var output any
	if err := json.Unmarshal(outputBytes, &output); err != nil {
		output = string(outputBytes)
	}
	return map[string]any{
		"SpaceID":    r.SpaceID,
		"UnitID":     r.UnitID,
		"SpaceSlug":  r.SpaceSlug,
		"UnitSlug":   r.UnitSlug,
		"OutputType": outputType,
		"Output":     output,
	}
}

// renderOutputByType mirrors the per-output-type rendering in
// outputFunctionInvocationResponse so --show output can emit the same
// human-readable blocks without the surrounding summary.
// When multipleFunctions is false, per-attribute function labels are
// suppressed since the function identity is unambiguous.
func renderOutputByType(outputType string, outputBytes []byte, outputRawFlag bool, i int, multipleFunctions bool) {
	switch outputType {
	case string(api.OutputTypeYAML):
		var payload api.YAMLPayload
		if err := json.Unmarshal(outputBytes, &payload); err != nil || outputRawFlag {
			tprintRaw(string(outputBytes))
		} else {
			if i > 0 {
				tprintRaw("---")
			}
			tprintRaw(payload.Payload)
		}
	case string(api.OutputTypeAttributeValueList):
		var payload api.AttributeValueList
		if err := json.Unmarshal(outputBytes, &payload); err != nil {
			tprintRaw(string(outputBytes))
		} else {
			for i := range payload {
				if multipleFunctions {
					funcDisplay := functionDisplayName(payload[i].FunctionName, payload[i].Index)
					tprint("  Function: %s", funcDisplay)
				}
				displayAttributeValue(&payload[i], "")
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

// renderFunctionData emits the configuration each response describes. A response carries
// ConfigData only when the invocation changed it, so an unchanged Unit's data is fetched
// from its data endpoint rather than silently skipped -- the command was asked for data,
// not for changes.
// When -O/--output-file is set, each unit's data is written to a path derived
// from the template (supports {space}, {unit}, {section}). Otherwise, stdout
// receives the raw bytes; when multiple units are present, each block is
// prefixed with "# unit: <space>/<slug>" for disambiguation.
func renderFunctionData(resp *[]goclientnew.FunctionInvocationsResponse) {
	multi := resp != nil && len(*resp) > 1
	for i := range *resp {
		r := &(*resp)[i]
		resolved, err := responseConfigData(r)
		failOnError(err)
		if len(resolved) == 0 {
			continue
		}
		data := []byte(resolved)
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
