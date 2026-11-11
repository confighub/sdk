// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const vapDeployments = `apiVersion: v1
kind: Namespace
metadata:
  name: prod
  labels:
    environment: prod
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: big
  namespace: prod
spec:
  replicas: 10
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: small
  namespace: dev
spec:
  replicas: 2
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  type: LoadBalancer
`

// replicaLimitPolicy limits the replicas of Deployments, as the Kubernetes documentation's example
// does, with a message expression naming the limit.
const replicaLimitPolicy = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: replica-limit
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
    - apiGroups: ["apps"]
      apiVersions: ["v1"]
      operations: ["CREATE", "UPDATE"]
      resources: ["deployments"]
  variables:
  - name: replicas
    expression: object.spec.replicas
  validations:
  - expression: variables.replicas <= 5
    messageExpression: "'replicas must be at most 5, not ' + string(variables.replicas)"
`

func vetVAP(t *testing.T, policy string) api.ValidationResult {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(vapDeployments))
	require.NoError(t, err)
	policyDocs, err := gaby.ParseAll([]byte(policy))
	require.NoError(t, err)
	_, output, err := VetValidatingAdmissionPolicy(testResourceProvider, api.NewFunctionOptions(), docs, policyDocs)
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return result
}

func TestVAPWithoutBindingDenies(t *testing.T) {
	result := vetVAP(t, replicaLimitPolicy)
	assert.False(t, result.Passed)
	assert.Equal(t, []string{"apps/v1/Deployment prod/big"}, failedResources(result))
	require.Len(t, result.FailedAttributes, 1)
	issue := result.FailedAttributes[0].Issues[0]
	assert.Equal(t, "replica-limit", issue.Identifier)
	assert.Equal(t, "replicas must be at most 5, not 10", issue.Message)
}

func TestVAPOnlyMatchingResources(t *testing.T) {
	policy := strings.Replace(replicaLimitPolicy, `resources: ["deployments"]`, `resources: ["statefulsets"]`, 1)
	result := vetVAP(t, policy)
	assert.True(t, result.Passed, "%v", result.Details)

	// A policy that matches no operation the creation of a resource is is not applied.
	policy = strings.Replace(replicaLimitPolicy, `operations: ["CREATE", "UPDATE"]`, `operations: ["DELETE"]`, 1)
	result = vetVAP(t, policy)
	assert.True(t, result.Passed, "%v", result.Details)
}

func TestVAPBindingNamespaceSelectorAndActions(t *testing.T) {
	binding := `---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: replica-limit-dev
spec:
  policyName: replica-limit
  validationActions: [%s]
  matchResources:
    namespaceSelector:
      matchLabels:
        kubernetes.io/metadata.name: %s
`
	// The binding applies the policy in dev, where the Deployment is within the limit.
	result := vetVAP(t, replicaLimitPolicy+fmt.Sprintf(binding, "Deny", "dev"))
	assert.True(t, result.Passed, "%v", result.Details)

	// In prod, which the Unit's Namespace labels, it is not.
	result = vetVAP(t, replicaLimitPolicy+fmt.Sprintf(binding, "Deny", "prod"))
	assert.False(t, result.Passed)
	assert.Equal(t, []string{"apps/v1/Deployment prod/big"}, failedResources(result))
	assert.Equal(t, "replica-limit/replica-limit-dev", result.FailedAttributes[0].Issues[0].Identifier)

	// A binding that only warns reports what it finds without failing.
	result = vetVAP(t, replicaLimitPolicy+fmt.Sprintf(binding, "Warn", "prod"))
	assert.True(t, result.Passed)
	require.Len(t, result.Details, 1)
	assert.Contains(t, result.Details[0], "warning: ")
	assert.Contains(t, result.Details[0], "replicas must be at most 5, not 10")
}

func TestVAPParams(t *testing.T) {
	policy := `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: no-load-balancers
spec:
  paramKind:
    apiVersion: v1
    kind: ConfigMap
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["services"]
  validations:
  - expression: "!(object.spec.type in params.data.forbidden.split(','))"
    message: service type is forbidden
    reason: Forbidden
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: no-load-balancers
spec:
  policyName: no-load-balancers
  validationActions: [Deny]
  paramRef:
    name: service-types
    namespace: policy
    parameterNotFoundAction: Deny
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: service-types
  namespace: policy
data:
  forbidden: %s
`
	result := vetVAP(t, fmt.Sprintf(policy, "LoadBalancer,NodePort"))
	assert.False(t, result.Passed)
	assert.Equal(t, []string{"v1/Service prod/web"}, failedResources(result))
	assert.Equal(t, "service type is forbidden", result.FailedAttributes[0].Issues[0].Message)

	result = vetVAP(t, fmt.Sprintf(policy, "NodePort"))
	assert.True(t, result.Passed, "%v", result.Details)

	// Params the binding names but the Unit does not hold deny, as parameterNotFoundAction says.
	missing := strings.Replace(fmt.Sprintf(policy, "NodePort"), "name: service-types\n    namespace", "name: other-types\n    namespace", 1)
	result = vetVAP(t, missing)
	assert.False(t, result.Passed)
	assert.Contains(t, result.FailedAttributes[0].Issues[0].Message, "no ConfigMap params found")

	result = vetVAP(t, strings.Replace(missing, "parameterNotFoundAction: Deny", "parameterNotFoundAction: Allow", 1))
	assert.True(t, result.Passed, "%v", result.Details)
}

func TestVAPBrokenExpressionFollowsFailurePolicy(t *testing.T) {
	broken := strings.Replace(replicaLimitPolicy, "variables.replicas <= 5", "variables.replicas <= ", 1)
	result := vetVAP(t, broken)
	assert.False(t, result.Passed, "an expression that does not compile fails closed")

	result = vetVAP(t, strings.Replace(broken, "failurePolicy: Fail", "failurePolicy: Ignore", 1))
	assert.True(t, result.Passed, "%v", result.Details)
}

func TestVAPNeedsAPolicy(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vapDeployments))
	require.NoError(t, err)
	_, _, err = VetValidatingAdmissionPolicy(testResourceProvider, api.NewFunctionOptions(), docs, nil)
	assert.ErrorContains(t, err, "pass a Unit holding one as OtherData policy")
}
