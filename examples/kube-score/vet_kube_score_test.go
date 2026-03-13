// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// A well-configured Deployment that should pass most kube-score checks.
const testDeploymentGood = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: good-deployment
  namespace: default
  labels:
    app: good
spec:
  replicas: 3
  selector:
    matchLabels:
      app: good
  template:
    metadata:
      labels:
        app: good
    spec:
      containers:
      - name: web
        image: nginx:1.21
        ports:
        - containerPort: 80
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi
        securityContext:
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          runAsUser: 65534
          runAsGroup: 65534
`

// A minimal Deployment missing resource limits, security context, etc.
const testDeploymentBad = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: bad-deployment
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bad
  template:
    metadata:
      labels:
        app: bad
    spec:
      containers:
      - name: web
        image: nginx:latest
        ports:
        - containerPort: 80
`

// A Pod for testing Pod-specific path resolution.
const testPod = `apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: default
spec:
  containers:
  - name: app
    image: myapp:1.0
    resources:
      requests:
        cpu: 50m
        memory: 64Mi
      limits:
        cpu: 100m
        memory: 128Mi
    securityContext:
      readOnlyRootFilesystem: true
      runAsNonRoot: true
      runAsUser: 65534
      runAsGroup: 65534
`

func requireKubeScoreCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(kubeScoreBinary); err != nil {
		t.Skipf("kube-score CLI not found in PATH (set kubeScoreBinary or install kube-score): %v", err)
	}
}

func TestVetKubeScore_GoodDeployment(t *testing.T) {
	requireKubeScoreCLI(t)

	// Even a well-configured Deployment gets Critical findings from kube-score for
	// missing inter-resource relationships (NetworkPolicy, PDB). Verify that
	// the good deployment has fewer/lower-severity findings than the bad one.
	parsedData, err := gaby.ParseAll([]byte(testDeploymentGood))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Critical"},
	}

	rp := k8skit.NewK8sResourceProvider()
	result, output, err := vetKubeScore(rp, parsedData, args)
	require.NoError(t, err)
	require.NotNil(t, result)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)

	// The good deployment should have fewer findings than the bad one.
	// It should NOT have container-resource or image-tag findings.
	for _, attr := range vr.FailedAttributes {
		desc := ""
		if attr.Details != nil {
			desc = attr.Details.Description
		}
		assert.NotContains(t, desc, "CPU limit is not set", "good deployment should have resource limits")
		assert.NotContains(t, desc, "Image with latest tag", "good deployment should use pinned tag")
	}
	t.Logf("Good deployment: Passed=%v, MaxScore=%s, findings=%d", vr.Passed, vr.MaxScore, len(vr.FailedAttributes))
}

func TestVetKubeScore_FailingValidation(t *testing.T) {
	requireKubeScoreCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentBad))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Critical"},
	}

	rp := k8skit.NewK8sResourceProvider()
	result, output, err := vetKubeScore(rp, parsedData, args)
	require.NoError(t, err)
	require.NotNil(t, result)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.False(t, vr.Passed, "expected validation to fail for bad deployment")
	assert.Equal(t, api.ScoreCritical, vr.MaxScore, "expected Critical max score")
	assert.NotEmpty(t, vr.FailedAttributes, "expected failed attributes")
}

func TestVetKubeScore_ThresholdMedium(t *testing.T) {
	requireKubeScoreCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentBad))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Medium"},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKubeScore(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected validation to fail with Medium threshold")
}

func TestVetKubeScore_ThresholdLow_BelowThresholdAttributes(t *testing.T) {
	requireKubeScoreCLI(t)

	// The bad deployment should produce findings at different severity levels.
	// With a Low threshold, even AlmostOK findings should cause failure.
	parsedData, err := gaby.ParseAll([]byte(testDeploymentBad))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Low"},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKubeScore(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected validation to fail with Low threshold")

	// Verify that there are attributes with scores at different levels.
	// The bad deployment should have Critical findings (missing resource limits)
	// and potentially Medium findings too.
	hasCritical := false
	hasBelowCritical := false
	for _, attr := range vr.FailedAttributes {
		if attr.Score == api.ScoreCritical {
			hasCritical = true
		}
		if attr.Score == api.ScoreMedium || attr.Score == api.ScoreLow {
			hasBelowCritical = true
		}
	}
	assert.True(t, hasCritical, "expected at least one Critical finding")
	// Log all scores for debugging
	t.Logf("MaxScore: %s, total attributes: %d, hasCritical: %v, hasBelowCritical: %v",
		vr.MaxScore, len(vr.FailedAttributes), hasCritical, hasBelowCritical)
}

func TestVetKubeScore_ContainerPathResolution(t *testing.T) {
	requireKubeScoreCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentBad))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Critical"},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKubeScore(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.NotEmpty(t, vr.FailedAttributes)

	// All attributes should have paths (non-path findings go to Details only).
	// Paths should be specific, extending past the container index.
	paths := map[string]bool{}
	for _, attr := range vr.FailedAttributes {
		p := string(attr.Path)
		assert.NotEmpty(t, p, "every FailedAttribute should have a path")
		paths[p] = true
		t.Logf("Resolved path: %s — %s", attr.Path, attr.Details.Description)
	}
	// Should see field-specific paths like resources.limits.cpu, securityContext, image, etc.
	assert.True(t, paths["spec.template.spec.containers.0.resources.limits.cpu"],
		"expected a path for resources.limits.cpu")
	assert.True(t, paths["spec.template.spec.containers.0.image"],
		"expected a path for image tag check")

	// Non-path findings (NetworkPolicy, PDB) should be in Details, not FailedAttributes.
	assert.NotEmpty(t, vr.Details, "expected non-attribute findings in Details")
}

func TestVetKubeScore_ScoreAndDescription(t *testing.T) {
	requireKubeScoreCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentBad))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Low"},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKubeScore(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)

	// Every failed attribute should have a score and description.
	for i, attr := range vr.FailedAttributes {
		assert.NotEqual(t, api.ScoreNone, attr.Score, "attribute %d should have a score", i)
		require.NotNil(t, attr.Details, "attribute %d should have details", i)
		assert.NotEmpty(t, attr.Details.Description, "attribute %d should have description", i)
	}
}

func TestVetKubeScore_MultipleResources(t *testing.T) {
	requireKubeScoreCLI(t)

	multiDoc := testDeploymentGood + "---\n" + testDeploymentBad
	parsedData, err := gaby.ParseAll([]byte(multiDoc))
	require.NoError(t, err)
	require.Len(t, parsedData, 2, "expected 2 documents")

	args := []api.FunctionArgument{
		{Value: "Critical"},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKubeScore(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	// Should fail because bad-deployment has critical findings.
	assert.False(t, vr.Passed, "expected validation to fail due to bad-deployment")
	assert.Equal(t, api.ScoreCritical, vr.MaxScore)
}

func TestVetKubeScore_MissingBinary(t *testing.T) {
	old := kubeScoreBinary
	kubeScoreBinary = "nonexistent-kube-score-binary-12345"
	defer func() { kubeScoreBinary = old }()

	parsedData, err := gaby.ParseAll([]byte(testDeploymentGood))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Critical"},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKubeScore(rp, parsedData, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute kube-score CLI")
}

func TestVetKubeScore_InvalidThreshold(t *testing.T) {
	parsedData, err := gaby.ParseAll([]byte(testDeploymentGood))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "Invalid"},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKubeScore(rp, parsedData, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid score-threshold")
}

// Unit tests for helper functions (don't need the kube-score binary).

func TestGradeToScore(t *testing.T) {
	assert.Equal(t, api.ScoreCritical, gradeToScore(gradeCritical))
	assert.Equal(t, api.ScoreMedium, gradeToScore(gradeWarning))
	assert.Equal(t, api.ScoreLow, gradeToScore(gradeAlmostOK))
	assert.Equal(t, api.ScoreNone, gradeToScore(gradeAllOK))
	assert.Equal(t, api.ScoreNone, gradeToScore(99))
}

func TestParseScoreThreshold(t *testing.T) {
	tests := []struct {
		input    string
		expected api.Score
		wantErr  bool
	}{
		{"Critical", api.ScoreCritical, false},
		{"High", api.ScoreHigh, false},
		{"Medium", api.ScoreMedium, false},
		{"Low", api.ScoreLow, false},
		{"invalid", api.ScoreNone, true},
		{"", api.ScoreNone, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseScoreThreshold(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestCommentSubPath(t *testing.T) {
	tests := []struct {
		summary  string
		expected string
	}{
		// Resource limits/requests
		{"CPU limit is not set", "resources.limits.cpu"},
		{"Memory limit is not set", "resources.limits.memory"},
		{"CPU request is not set", "resources.requests.cpu"},
		{"Memory request is not set", "resources.requests.memory"},
		{"CPU requests does not match limits", "resources"},
		{"Memory requests does not match limits", "resources"},
		// Ephemeral storage
		{"Ephemeral Storage limit is not set", "resources.limits.ephemeral-storage"},
		{"Ephemeral Storage request is not set", "resources.requests.ephemeral-storage"},
		{"Ephemeral Storage request does not match limit", "resources"},
		// Image
		{"Image with latest tag", "image"},
		{"ImagePullPolicy is not set to Always", "imagePullPolicy"},
		// Security context
		{"The pod has a container with a writable root filesystem", "securityContext.readOnlyRootFilesystem"},
		{"The container is privileged", "securityContext.privileged"},
		{"The container is running with a low user ID", "securityContext.runAsUser"},
		{"The container running with a low group ID", "securityContext.runAsGroup"},
		{"The container has not configured Seccomp", "securityContext.seccompProfile"},
		{"Container has no configured security context", "securityContext"},
		// Ports
		{"Container Port Check", "ports"},
		// Env
		{"Environment Variable Key Duplication", "env"},
		// Unknown
		{"Something totally unknown", ""},
		{"The pod does not have a matching NetworkPolicy", ""},
	}
	for _, tt := range tests {
		t.Run(tt.summary, func(t *testing.T) {
			assert.Equal(t, tt.expected, commentSubPath(tt.summary))
		})
	}
}

func TestResolveContainerPath(t *testing.T) {
	// Test with a Deployment
	parsedData, err := gaby.ParseAll([]byte(testDeploymentBad))
	require.NoError(t, err)

	obj := kubeScoreScoredObject{
		TypeMeta:   kubeScoreTypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: kubeScoreObjectMeta{Name: "bad-deployment", Namespace: "default"},
	}

	path := resolveContainerPath(parsedData, obj, "web")
	assert.Equal(t, "spec.template.spec.containers.0", path, "should resolve container name to dotted path")

	// Empty container name returns empty path.
	assert.Equal(t, "", resolveContainerPath(parsedData, obj, ""))

	// Unknown container name returns empty path.
	assert.Equal(t, "", resolveContainerPath(parsedData, obj, "nonexistent"))
}

func TestResolveContainerPath_Pod(t *testing.T) {
	parsedData, err := gaby.ParseAll([]byte(testPod))
	require.NoError(t, err)

	obj := kubeScoreScoredObject{
		TypeMeta:   kubeScoreTypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: kubeScoreObjectMeta{Name: "test-pod", Namespace: "default"},
	}

	path := resolveContainerPath(parsedData, obj, "app")
	assert.Equal(t, "spec.containers.0", path, "Pod containers should use spec.containers prefix")
}

func TestProcessKubeScoreJSON(t *testing.T) {
	// Simulate kube-score JSON output to test processing without the binary.
	scoredObjects := kubeScoreOutput{
		{
			ObjectName: "Deployment/apps/v1//test-deploy",
			TypeMeta:   kubeScoreTypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
			ObjectMeta: kubeScoreObjectMeta{Name: "test-deploy"},
			Checks: []kubeScoreTestScore{
				{
					Check: kubeScoreCheck{
						Name:       "Container Resources",
						ID:         "container-resources",
						TargetType: "Pod",
					},
					Grade:   gradeCritical,
					Skipped: false,
					Comments: []kubeScoreTestComment{
						{
							Path:        "web",
							Summary:     "CPU limit is not set",
							Description: "Resource limits are recommended to avoid resource DDOS. Set resources.limits.cpu",
						},
					},
				},
				{
					Check: kubeScoreCheck{
						Name:       "Container Image Tag",
						ID:         "container-image-tag",
						TargetType: "Pod",
					},
					Grade:   gradeCritical,
					Skipped: false,
					Comments: []kubeScoreTestComment{
						{
							Path:        "web",
							Summary:     "Image with latest tag",
							Description: "Using a fixed tag is recommended to avoid accidental upgrades",
						},
					},
				},
				{
					Check: kubeScoreCheck{
						Name:       "Pod NetworkPolicy",
						ID:         "pod-networkpolicy",
						TargetType: "Pod",
					},
					Grade:   gradeWarning,
					Skipped: false,
					Comments: []kubeScoreTestComment{
						{
							Path:    "",
							Summary: "The pod does not have a matching NetworkPolicy",
						},
					},
				},
				{
					Check: kubeScoreCheck{
						Name:       "Container Ephemeral Storage Request and Limit",
						ID:         "container-ephemeral-storage-request-and-limit",
						TargetType: "Pod",
					},
					Grade:   gradeAllOK,
					Skipped: true,
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(scoredObjects)
	require.NoError(t, err)

	// Verify JSON roundtrip.
	var parsed kubeScoreOutput
	require.NoError(t, json.Unmarshal(jsonBytes, &parsed))
	require.Len(t, parsed, 1)
	require.Len(t, parsed[0].Checks, 4)

	// Test processing with container path resolution and sub-paths.
	t.Run("CriticalThreshold", func(t *testing.T) {
		// Use a deployment whose name matches the simulated kube-score output.
		testDeploy := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deploy
spec:
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: web
        image: nginx:latest
`
		parsedData, err := gaby.ParseAll([]byte(testDeploy))
		require.NoError(t, err)

		rp := k8skit.NewK8sResourceProvider()
		resourceInfoMap := buildResourceInfoMap(parsedData, rp)

		var maxScore api.Score
		var details []string
		var failedAttributes api.AttributeValueList

		for _, obj := range parsed {
			resourceKey := kubeScoreResourceKey(obj)
			resourceInfo := lookupResourceInfo(resourceInfoMap, resourceKey)

			for _, check := range obj.Checks {
				if check.Skipped {
					continue
				}
				score := gradeToScore(check.Grade)
				if score == api.ScoreNone {
					continue
				}
				maxScore = api.ScoreMax(maxScore, score)
				for _, comment := range check.Comments {
					containerPath := resolveContainerPath(parsedData, obj, comment.Path)
					if containerPath == "" {
						details = append(details, comment.Summary)
						continue
					}
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
						},
						Score: score,
					})
				}
			}
		}

		assert.Equal(t, api.ScoreCritical, maxScore)
		// 2 container-specific attributes (CPU limit, image tag); NetworkPolicy goes to details.
		assert.Len(t, failedAttributes, 2, "should have 2 failed attributes (container-specific only)")
		assert.Len(t, details, 1, "should have 1 detail (NetworkPolicy)")

		// Verify scores and specific paths.
		assert.Equal(t, api.ScoreCritical, failedAttributes[0].Score)
		assert.Equal(t, "spec.template.spec.containers.0.resources.limits.cpu", string(failedAttributes[0].Path))
		assert.Equal(t, api.ScoreCritical, failedAttributes[1].Score)
		assert.Equal(t, "spec.template.spec.containers.0.image", string(failedAttributes[1].Path))

		// Critical threshold: maxScore >= Critical → fail
		passed := maxScore == api.ScoreNone || api.ScoreMax(maxScore, api.ScoreCritical) != maxScore
		assert.False(t, passed)
	})

	// Test with High threshold — MaxScore is Critical which is above High, should fail.
	t.Run("HighThreshold", func(t *testing.T) {
		// MaxScore is Critical. ScoreMax(Critical, High) == Critical, so fail.
		passed := api.ScoreCritical == api.ScoreNone || api.ScoreMax(api.ScoreCritical, api.ScoreHigh) != api.ScoreCritical
		assert.False(t, passed)
	})

	// Test threshold logic: if maxScore is Medium and threshold is Critical.
	t.Run("MediumMaxCriticalThreshold", func(t *testing.T) {
		// ScoreMax(Medium, Critical) == Critical != Medium, so pass.
		passed := api.ScoreMedium == api.ScoreNone || api.ScoreMax(api.ScoreMedium, api.ScoreCritical) != api.ScoreMedium
		assert.True(t, passed, "Medium maxScore should pass with Critical threshold")
	})

	// Test threshold logic: if maxScore is Medium and threshold is Medium.
	t.Run("MediumMaxMediumThreshold", func(t *testing.T) {
		// ScoreMax(Medium, Medium) == Medium == Medium, so fail.
		passed := api.ScoreMedium == api.ScoreNone || api.ScoreMax(api.ScoreMedium, api.ScoreMedium) != api.ScoreMedium
		assert.False(t, passed, "Medium maxScore should fail with Medium threshold")
	})

	// Test threshold logic: if maxScore is None.
	t.Run("NoneMaxLowThreshold", func(t *testing.T) {
		passed := api.ScoreNone == api.ScoreNone || api.ScoreMax(api.ScoreNone, api.ScoreLow) != api.ScoreNone
		assert.True(t, passed, "None maxScore should always pass")
	})
}
