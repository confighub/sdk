// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

// kubeScoreBinary is the name or path of the kube-score CLI binary.
// It can be overridden for testing.
var kubeScoreBinary = "kube-score"

// GetVetKubeScoreSignature returns the function signature for vet-kube-score.
func GetVetKubeScoreSignature() api.FunctionSignature {
	return api.FunctionSignature{
		FunctionName: "vet-kube-score",
		Parameters: []api.FunctionParameter{
			{
				ParameterName: "score-threshold",
				Required:      true,
				Description:   `Score threshold for validation failure. If any finding has a score at or above this threshold, validation fails. Possible values: "Critical", "High", "Medium", "Low".`,
				DataType:      api.DataTypeString,
			},
		},
		RequiredParameters: 1,
		OutputInfo: &api.FunctionOutput{
			ResultName:  "passed",
			Description: "True if all resources score below the threshold, false otherwise",
			OutputType:  api.OutputTypeValidationResult,
		},
		Mutating:              false,
		Validating:            true,
		Hermetic:              false, // execs kube-score CLI
		Idempotent:            true,
		Description:           "Validates Kubernetes resources using kube-score best-practice checks. See https://kube-score.com for details.",
		FunctionType:          api.FunctionTypeCustom,
		AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
	}
}

// VetKubeScoreFunction validates Kubernetes resources using the kube-score CLI.
func VetKubeScoreFunction(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
	return vetKubeScore(k8skit.NewK8sResourceProvider(), fArgs.ParsedData, fArgs.Arguments)
}

// kube-score Grade constants (from scorecard.Grade).
const (
	gradeCritical = 1
	gradeWarning  = 5
	gradeAlmostOK = 7
	gradeAllOK    = 10
)

// gradeToScore maps a kube-score grade to a ConfigHub Score.
func gradeToScore(grade int) api.Score {
	switch grade {
	case gradeCritical:
		return api.ScoreCritical
	case gradeWarning:
		return api.ScoreMedium
	case gradeAlmostOK:
		return api.ScoreLow
	case gradeAllOK:
		return api.ScoreNone
	default:
		return api.ScoreNone
	}
}

// parseScoreThreshold converts a threshold string to an api.Score.
func parseScoreThreshold(s string) (api.Score, error) {
	switch s {
	case "Critical":
		return api.ScoreCritical, nil
	case "High":
		return api.ScoreHigh, nil
	case "Medium":
		return api.ScoreMedium, nil
	case "Low":
		return api.ScoreLow, nil
	default:
		return api.ScoreNone, fmt.Errorf("invalid score-threshold %q: must be Critical, High, Medium, or Low", s)
	}
}

// JSON types matching kube-score json_v2 output.
type kubeScoreOutput []kubeScoreScoredObject

type kubeScoreScoredObject struct {
	ObjectName string               `json:"object_name"`
	TypeMeta   kubeScoreTypeMeta    `json:"type_meta"`
	ObjectMeta kubeScoreObjectMeta  `json:"object_meta"`
	Checks     []kubeScoreTestScore `json:"checks"`
}

type kubeScoreTypeMeta struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
}

type kubeScoreObjectMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type kubeScoreTestScore struct {
	Check    kubeScoreCheck         `json:"check"`
	Grade    int                    `json:"grade"`
	Skipped  bool                   `json:"skipped"`
	Comments []kubeScoreTestComment `json:"comments"`
}

type kubeScoreCheck struct {
	Name       string `json:"name"`
	ID         string `json:"id"`
	TargetType string `json:"target_type"`
	Comment    string `json:"comment"`
	Optional   bool   `json:"optional"`
}

type kubeScoreTestComment struct {
	Path        string `json:"path"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

func vetKubeScore(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	thresholdStr := args[0].Value.(string)
	threshold, err := parseScoreThreshold(thresholdStr)
	if err != nil {
		return parsedData, nil, err
	}

	// Write resources to a temp file.
	resourceFile, err := os.CreateTemp("", "kube-score-resource-*.yaml")
	if err != nil {
		return parsedData, nil, errors.Wrap(err, "failed to create temp resource file")
	}
	defer os.Remove(resourceFile.Name())
	for i, doc := range parsedData {
		if i > 0 {
			resourceFile.WriteString("---\n")
		}
		resourceFile.Write(doc.Bytes())
		resourceFile.WriteString("\n")
	}
	resourceFile.Close()

	// Run kube-score score with JSON output.
	cmd := exec.Command(kubeScoreBinary, "score", "--output-format", "json", resourceFile.Name())
	output, err := cmd.CombinedOutput()

	// kube-score exits non-zero when there are critical findings,
	// but still produces valid JSON output. Only treat non-ExitError as real errors.
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return parsedData, nil, errors.Wrapf(err, "failed to execute kube-score CLI (is %q in PATH?)", kubeScoreBinary)
		}
	}

	// Parse the JSON output.
	var scoredObjects kubeScoreOutput
	if err := json.Unmarshal(output, &scoredObjects); err != nil {
		return parsedData, nil, errors.Wrapf(err, "failed to parse kube-score JSON output: %s", string(output))
	}

	// Build resource info map for the parsed YAML data.
	resourceInfoMap := buildResourceInfoMap(parsedData, rp)

	// Process results.
	var maxScore api.Score
	var details []string
	var failedAttributes api.AttributeValueList

	for _, obj := range scoredObjects {
		resourceKey := kubeScoreResourceKey(obj)
		resourceInfo := lookupResourceInfo(resourceInfoMap, resourceKey)

		for _, check := range obj.Checks {
			if check.Skipped {
				continue
			}

			score := gradeToScore(check.Grade)
			if score == api.ScoreNone {
				continue // AllOK, no finding to report
			}

			maxScore = api.ScoreMax(maxScore, score)

			if len(check.Comments) == 0 {
				detail := fmt.Sprintf("[%s] %s: %s", score, check.Check.Name, check.Check.Comment)
				details = append(details, detail)
				continue
			}

			for _, comment := range check.Comments {
				containerPath := resolveContainerPath(parsedData, obj, comment.Path)

				desc := comment.Summary
				if comment.Description != "" {
					desc += ": " + comment.Description
				}
				detail := fmt.Sprintf("[%s] %s: %s", score, check.Check.Name, desc)

				if containerPath == "" {
					// Non-attribute finding (e.g., NetworkPolicy, PDB). Details only.
					details = append(details, detail)
					continue
				}

				// Extend the container path with a field-specific sub-path
				// derived from the comment summary.
				subPath := commentSubPath(comment.Summary)
				path := containerPath
				if subPath != "" {
					path += "." + subPath
				}

				failedAttributes = append(failedAttributes, api.AttributeValue{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: resourceInfo,
							Path:         api.ResolvedPath(path),
						},
						AttributeMetadata: api.AttributeMetadata{
							AttributeName: api.AttributeNameNone,
							Details: &api.AttributeDetails{
								Description: detail,
							},
						},
					},
					Score: score,
				})
			}
		}
	}

	// Determine pass/fail based on threshold.
	// If MaxScore >= threshold then fail. ScoreMax(maxScore, threshold) == maxScore
	// implies maxScore >= threshold.
	passed := maxScore == api.ScoreNone || api.ScoreMax(maxScore, threshold) != maxScore

	result := api.ValidationResult{
		Passed:           passed,
		MaxScore:         maxScore,
		Details:          details,
		FailedAttributes: failedAttributes,
	}

	return parsedData, result, nil
}

// kubeScoreResourceKey produces the kube-score-style resource reference key.
// Format: Kind/APIVersion/Namespace/Name
func kubeScoreResourceKey(obj kubeScoreScoredObject) string {
	return obj.TypeMeta.Kind + "/" + obj.TypeMeta.APIVersion + "/" + obj.ObjectMeta.Namespace + "/" + obj.ObjectMeta.Name
}

// resolveContainerPath attempts to convert a kube-score comment path (typically
// a container name) into a gaby dot-notation path by searching the YAML structure.
func resolveContainerPath(parsedData gaby.Container, obj kubeScoreScoredObject, containerName string) string {
	if containerName == "" {
		return ""
	}

	// Determine possible container path prefixes based on resource kind.
	var prefixes []string
	switch obj.TypeMeta.Kind {
	case "Pod":
		prefixes = []string{"spec.initContainers", "spec.containers"}
	case "CronJob":
		prefixes = []string{
			"spec.jobTemplate.spec.template.spec.initContainers",
			"spec.jobTemplate.spec.template.spec.containers",
		}
	default:
		// Deployment, StatefulSet, DaemonSet, ReplicaSet, Job
		prefixes = []string{
			"spec.template.spec.initContainers",
			"spec.template.spec.containers",
		}
	}

	// Find the matching YAML doc by name/namespace.
	for _, doc := range parsedData {
		name, _ := doc.Path("metadata.name").Data().(string)
		ns, _ := doc.Path("metadata.namespace").Data().(string)
		if name != obj.ObjectMeta.Name {
			continue
		}
		if obj.ObjectMeta.Namespace != "" && ns != obj.ObjectMeta.Namespace {
			continue
		}

		// Search container arrays for the named container.
		for _, prefix := range prefixes {
			containers := doc.Path(prefix)
			if containers == nil || containers.Data() == nil {
				continue
			}
			children := containers.Children()
			for i, c := range children {
				cname, _ := c.Path("name").Data().(string)
				if cname == containerName {
					return fmt.Sprintf("%s.%d", prefix, i)
				}
			}
		}
	}

	return ""
}

// commentSubPath maps a kube-score comment Summary to a field-specific sub-path
// that can be appended to a container path for more precise attribution.
// Returns "" if no specific sub-path can be determined.
func commentSubPath(summary string) string {
	// Exact-match mappings for known kube-score comment summaries.
	switch summary {
	// container resource checks
	case "CPU limit is not set":
		return "resources.limits.cpu"
	case "Memory limit is not set":
		return "resources.limits.memory"
	case "CPU request is not set":
		return "resources.requests.cpu"
	case "Memory request is not set":
		return "resources.requests.memory"
	case "CPU requests does not match limits":
		return "resources"
	case "Memory requests does not match limits":
		return "resources"
	// ephemeral storage checks
	case "Ephemeral Storage limit is not set":
		return "resources.limits.ephemeral-storage"
	case "Ephemeral Storage request is not set":
		return "resources.requests.ephemeral-storage"
	case "Ephemeral Storage request does not match limit":
		return "resources"
	// image checks
	case "Image with latest tag":
		return "image"
	case "ImagePullPolicy is not set to Always":
		return "imagePullPolicy"
	// security context checks
	case "The pod has a container with a writable root filesystem":
		return "securityContext.readOnlyRootFilesystem"
	case "The container is privileged":
		return "securityContext.privileged"
	case "The container is running with a low user ID":
		return "securityContext.runAsUser"
	case "The container running with a low group ID":
		return "securityContext.runAsGroup"
	case "The container has not configured Seccomp":
		return "securityContext.seccompProfile"
	}

	// Prefix-based fallbacks for security-context checks that share summaries
	// across multiple check IDs.
	if strings.HasPrefix(summary, "Container has no configured security context") {
		return "securityContext"
	}

	// Port checks
	if strings.HasPrefix(summary, "Container Port Check") ||
		strings.HasPrefix(summary, "Container ports") {
		return "ports"
	}

	// Env var checks
	if strings.Contains(summary, "Environment Variable") {
		return "env"
	}

	return ""
}

// buildResourceInfoMap creates a map from kube-score resource key
// (Kind/APIVersion/Namespace/Name) to ResourceInfo.
func buildResourceInfoMap(parsedData gaby.Container, rp *k8skit.K8sResourceProviderType) map[string]api.ResourceInfo {
	infoMap := make(map[string]api.ResourceInfo)
	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		kind, _ := doc.Path("kind").Data().(string)
		apiVersion, _ := doc.Path("apiVersion").Data().(string)
		name, _ := doc.Path("metadata.name").Data().(string)
		ns, _ := doc.Path("metadata.namespace").Data().(string)
		key := kind + "/" + apiVersion + "/" + ns + "/" + name
		infoMap[key] = *resourceInfo
		return output, nil
	}
	yamlkit.VisitResources(parsedData, nil, rp, visitor)
	return infoMap
}

func lookupResourceInfo(infoMap map[string]api.ResourceInfo, key string) api.ResourceInfo {
	if info, ok := infoMap[key]; ok {
		return info
	}
	return api.ResourceInfo{}
}
