// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// testDisruptionRP registers a small EKS-shaped disruption table: replacing the cluster is
// critical, replacing the node group is high, and a rolling node replacement is medium.
func testDisruptionRP() *k8skit.K8sResourceProviderType {
	rp := k8skit.NewK8sResourceProvider()
	register := func(attr api.AttributeName, resourceType api.ResourceType, paths ...string) {
		pathInfos := make(api.PathToVisitorInfoType, len(paths))
		for _, p := range paths {
			up := api.UnresolvedPath(p)
			pathInfos[up] = &api.PathVisitorInfo{
				Path: up, AttributeName: attr, DataType: api.DataTypeYAML,
			}
		}
		yamlkit.RegisterPathsByAttributeName(rp, attr, resourceType, pathInfos, nil, false, false)
	}
	const cluster = api.ResourceType("eks.aws.upbound.io/v1beta2/Cluster")
	const nodeGroup = api.ResourceType("eks.aws.upbound.io/v1beta2/NodeGroup")

	register("disruption-critical", cluster, "spec.forProvider.kubernetesNetworkConfig.serviceIpv4Cidr")
	register("disruption-high", nodeGroup, "spec.forProvider.instanceTypes", "spec.forProvider.amiType")
	register("disruption-medium", nodeGroup, "spec.forProvider.version")
	return rp
}

const ngBaseline = `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.large
    amiType: AL2023_x86_64_STANDARD
    version: "1.34"
    scalingConfig:
      minSize: 2
      maxSize: 6
`

func parse(t *testing.T, s string) gaby.Container {
	t.Helper()
	c, err := gaby.ParseAll([]byte(s))
	require.NoError(t, err)
	return c
}

func otherData(t *testing.T, source api.OtherDataSource, s string) map[api.OtherDataSource]gaby.Container {
	t.Helper()
	return map[api.OtherDataSource]gaby.Container{source: parse(t, s)}
}

func TestVetDisruption_NoChangePasses(t *testing.T) {
	rp := testDisruptionRP()
	res, err := GenericVetDisruption(rp, parse(t, ngBaseline),
		otherData(t, "LastAppliedRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.NoError(t, err)
	assert.True(t, res.Passed)
	assert.Equal(t, api.ScoreNone, res.MaxScore)
	assert.Empty(t, res.FailedAttributes)
}

// A change to a field registered under no tier is not disruption.
func TestVetDisruption_UnregisteredFieldIgnored(t *testing.T) {
	current := `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.large
    amiType: AL2023_x86_64_STANDARD
    version: "1.34"
    scalingConfig:
      minSize: 2
      maxSize: 20
`
	rp := testDisruptionRP()
	res, err := GenericVetDisruption(rp, parse(t, current),
		otherData(t, "LastAppliedRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.NoError(t, err)
	assert.True(t, res.Passed, "a scaling change is not disruption")
	assert.Equal(t, api.ScoreNone, res.MaxScore)
}

// The threshold decides which severities fail; the grading itself does not change.
func TestVetDisruption_Threshold(t *testing.T) {
	rolling := `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.large
    amiType: AL2023_x86_64_STANDARD
    version: "1.35"
`
	rp := testDisruptionRP()
	for _, tc := range []struct {
		threshold  api.Score
		wantPassed bool
	}{
		// The change is Medium (a node-group version bump rolls the nodes).
		{api.ScoreCritical, true},
		{api.ScoreHigh, true},
		{api.ScoreMedium, false},
		{api.ScoreLow, false},
	} {
		res, err := GenericVetDisruption(rp, parse(t, rolling),
			otherData(t, "LastAppliedRevisionNum", ngBaseline),
			DefaultDisruptionAttributePrefix, tc.threshold, nil)
		require.NoError(t, err)
		assert.Equal(t, tc.wantPassed, res.Passed,
			"threshold %s against a Medium change", tc.threshold)
		// Grading is independent of the threshold.
		assert.Equal(t, api.ScoreMedium, res.MaxScore)
		require.Len(t, res.FailedAttributes, 1)
		assert.Equal(t, api.ScoreMedium, res.FailedAttributes[0].Score)
	}
}

func TestVetDisruption_HighSeverityReplacement(t *testing.T) {
	replaced := `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.2xlarge
    amiType: AL2023_x86_64_STANDARD
    version: "1.34"
`
	rp := testDisruptionRP()
	res, err := GenericVetDisruption(rp, parse(t, replaced),
		otherData(t, "LastAppliedRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreHigh, nil)
	require.NoError(t, err)
	assert.False(t, res.Passed)
	assert.Equal(t, api.ScoreHigh, res.MaxScore)
	require.Len(t, res.FailedAttributes, 1)
	assert.Equal(t, "disruption-high", res.FailedAttributes[0].Issues[0].Identifier)
	assert.Contains(t, res.FailedAttributes[0].Issues[0].Message, "cannot be reconciled in place")
}

// The worst tier wins when several severities change at once.
func TestVetDisruption_WorstTierWins(t *testing.T) {
	clusterBaseline := `apiVersion: eks.aws.upbound.io/v1beta2
kind: Cluster
metadata:
  name: prod
spec:
  forProvider:
    kubernetesNetworkConfig:
      serviceIpv4Cidr: 10.100.0.0/16
`
	clusterChanged := `apiVersion: eks.aws.upbound.io/v1beta2
kind: Cluster
metadata:
  name: prod
spec:
  forProvider:
    kubernetesNetworkConfig:
      serviceIpv4Cidr: 10.200.0.0/16
`
	rp := testDisruptionRP()
	current := parse(t, clusterChanged+"---\n"+`apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.large
    amiType: AL2023_x86_64_STANDARD
    version: "1.35"
`)
	base := parse(t, clusterBaseline+"---\n"+ngBaseline)

	res, err := GenericVetDisruption(rp, current,
		map[api.OtherDataSource]gaby.Container{"LastAppliedRevisionNum": base},
		DefaultDisruptionAttributePrefix, api.ScoreCritical, nil)
	require.NoError(t, err)
	assert.False(t, res.Passed)
	assert.Equal(t, api.ScoreCritical, res.MaxScore, "the cluster replacement must dominate")
	assert.Len(t, res.FailedAttributes, 2, "both findings are reported, not just the worst")
}

// No baseline means the unit has never been applied. This must pass: failing would deadlock the
// unit, since it could never be applied to acquire the baseline that would let it pass. It is also
// correct on the merits — creating a resource is not replacing one.
func TestVetDisruption_NoBaselinePasses(t *testing.T) {
	rp := testDisruptionRP()
	res, err := GenericVetDisruption(rp, parse(t, ngBaseline), nil,
		DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.NoError(t, err)
	assert.True(t, res.Passed)

	res, err = GenericVetDisruption(rp, parse(t, ngBaseline),
		map[api.OtherDataSource]gaby.Container{},
		DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.NoError(t, err)
	assert.True(t, res.Passed)
}

// A baseline supplied under an unusable source is a misconfiguration, NOT a clean slate. Passing
// there would credit a validator that never ran — the failure mode this function exists to prevent.
func TestVetDisruption_UnusableBaselineErrors(t *testing.T) {
	rp := testDisruptionRP()
	_, err := GenericVetDisruption(rp, parse(t, ngBaseline),
		otherData(t, "HeadRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.Error(t, err, "an uninterpretable baseline must not silently pass")
	assert.Contains(t, err.Error(), "LastAppliedRevisionNum")
}

// Either supported baseline source works, so a Trigger configured with LiveRevisionNum is not
// silently inert.
func TestVetDisruption_AcceptsLiveRevisionBaseline(t *testing.T) {
	replaced := `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.2xlarge
    amiType: AL2023_x86_64_STANDARD
    version: "1.34"
`
	rp := testDisruptionRP()
	res, err := GenericVetDisruption(rp, parse(t, replaced),
		otherData(t, "LiveRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreHigh, nil)
	require.NoError(t, err)
	assert.False(t, res.Passed)
}

// LastAppliedRevisionNum is preferred when both are present.
func TestVetDisruption_PrefersLastApplied(t *testing.T) {
	rp := testDisruptionRP()
	// The unit currently matches LastApplied, but differs from Live. Preferring LastApplied means
	// no disruption is reported.
	live := `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.4xlarge
    amiType: AL2023_x86_64_STANDARD
    version: "1.34"
`
	res, err := GenericVetDisruption(rp, parse(t, ngBaseline),
		map[api.OtherDataSource]gaby.Container{
			"LastAppliedRevisionNum": parse(t, ngBaseline),
			"LiveRevisionNum":        parse(t, live),
		}, DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.NoError(t, err)
	assert.True(t, res.Passed, "LastAppliedRevisionNum should have been used as the baseline")
}

// A resource absent from the baseline is being created, not changed.
func TestVetDisruption_NewResourceIgnored(t *testing.T) {
	rp := testDisruptionRP()
	added := ngBaseline + "---\n" + `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: batch
spec:
  forProvider:
    instanceTypes:
      - m6i.8xlarge
    amiType: AL2023_ARM_64_STANDARD
    version: "1.34"
`
	res, err := GenericVetDisruption(rp, parse(t, added),
		otherData(t, "LastAppliedRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.NoError(t, err)
	assert.True(t, res.Passed, "adding a node group is a create, not a replacement")
}

// A validator with nothing registered would pass everything while appearing to run. That is worse
// than no validator, so it errors instead.
func TestVetDisruption_NoRegisteredPathsErrors(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider() // nothing registered
	_, err := GenericVetDisruption(rp, parse(t, ngBaseline),
		otherData(t, "LastAppliedRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreLow, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no paths registered")
}

// The prefix is configurable, so a second, differently-tiered table can coexist.
func TestVetDisruption_CustomAttributePrefix(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	up := api.UnresolvedPath("spec.forProvider.instanceTypes")
	yamlkit.RegisterPathsByAttributeName(rp, "blast-critical",
		api.ResourceType("eks.aws.upbound.io/v1beta2/NodeGroup"),
		api.PathToVisitorInfoType{up: &api.PathVisitorInfo{
			Path: up, AttributeName: "blast-critical", DataType: api.DataTypeYAML,
		}}, nil, false, false)

	changed := `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.2xlarge
`
	res, err := GenericVetDisruption(rp, parse(t, changed),
		otherData(t, "LastAppliedRevisionNum", ngBaseline), "blast", api.ScoreCritical, nil)
	require.NoError(t, err)
	assert.False(t, res.Passed)
	assert.Equal(t, api.ScoreCritical, res.MaxScore)
}

// Paths registered through an Attribute must declare DataType string, because the server requires
// each path's type to match the Attribute's and an Attribute's is restricted to string/int/bool.
// The values themselves are frequently lists or maps, so verify a string-typed registration still
// grades a change to a list-valued path — otherwise the server-side gate would silently miss
// exactly the field (instanceTypes) that motivates the whole check.
func TestVetDisruption_StringTypedRegistrationGradesListValues(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	up := api.UnresolvedPath("spec.forProvider.instanceTypes")
	yamlkit.RegisterPathsByAttributeName(rp, "disruption-high",
		api.ResourceType("eks.aws.upbound.io/v1beta2/NodeGroup"),
		api.PathToVisitorInfoType{up: &api.PathVisitorInfo{
			Path: up, AttributeName: "disruption-high", DataType: api.DataTypeString,
		}}, nil, false, false)

	changed := `apiVersion: eks.aws.upbound.io/v1beta2
kind: NodeGroup
metadata:
  name: system
spec:
  forProvider:
    instanceTypes:
      - m6i.2xlarge
    amiType: AL2023_x86_64_STANDARD
    version: "1.34"
`
	res, err := GenericVetDisruption(rp, parse(t, changed),
		otherData(t, "LastAppliedRevisionNum", ngBaseline),
		DefaultDisruptionAttributePrefix, api.ScoreHigh, nil)
	require.NoError(t, err)
	assert.False(t, res.Passed, "a list-valued path registered as string must still be graded")
	assert.Equal(t, api.ScoreHigh, res.MaxScore)
}
