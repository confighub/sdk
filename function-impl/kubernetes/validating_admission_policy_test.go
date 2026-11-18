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
	// The Issue names the policy, which is what a rule selects and an exception names; the
	// details say which binding applied it.
	assert.Equal(t, "replica-limit", result.FailedAttributes[0].Issues[0].Identifier)
	assert.Contains(t, strings.Join(result.Details, "\n"), "replica-limit/replica-limit-dev")

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

func vetVAPWith(t *testing.T, data string, inputs AdmissionPolicyInputs) (api.ValidationResult, error) {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	_, output, err := VetValidatingAdmissionPolicyWith(testResourceProvider, api.NewFunctionOptions(), docs, inputs)
	if err != nil {
		return api.ValidationResult{}, err
	}
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return result, nil
}

func parseDocs(t *testing.T, data string) gaby.Container {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	return docs
}

// noLoadBalancerPolicy denies LoadBalancer Services, a second policy beside replicaLimitPolicy.
const noLoadBalancerPolicy = `---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: no-load-balancer
spec:
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE", "UPDATE"]
      resources: ["services"]
  validations:
  - expression: object.spec.type != 'LoadBalancer'
    message: no LoadBalancer Services
`

func issueIDs(result api.ValidationResult) []string {
	var ids []string
	for _, fa := range result.FailedAttributes {
		for _, issue := range fa.Issues {
			ids = append(ids, issue.Identifier)
		}
	}
	return ids
}

// One Unit of policies can serve several rules, each evaluating some of them.
func TestVAPSelectPolicies(t *testing.T) {
	policies := parseDocs(t, replicaLimitPolicy+noLoadBalancerPolicy)

	result, err := vetVAPWith(t, vapDeployments, AdmissionPolicyInputs{Policy: policies})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"replica-limit", "no-load-balancer"}, issueIDs(result))

	result, err = vetVAPWith(t, vapDeployments, AdmissionPolicyInputs{Policy: policies, Include: []string{"no-load-balancer"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"no-load-balancer"}, issueIDs(result))

	result, err = vetVAPWith(t, vapDeployments, AdmissionPolicyInputs{Policy: policies, Exclude: []string{"no-load-balancer"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"replica-limit"}, issueIDs(result))

	_, err = vetVAPWith(t, vapDeployments, AdmissionPolicyInputs{Policy: policies, Include: []string{"replica-limt"}})
	assert.ErrorContains(t, err, `no ValidatingAdmissionPolicy named "replica-limt"`)

	assert.Equal(t, []string{"a", "b"}, splitNames(" a, ,b,"))
}

// Params kept in a Unit of their own are found when the policy Unit has none.
func TestVAPParamsUnit(t *testing.T) {
	policy := `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: replica-limit
spec:
  paramKind:
    apiVersion: v1
    kind: ConfigMap
  matchConstraints:
    resourceRules:
    - apiGroups: ["apps"]
      apiVersions: ["v1"]
      operations: ["CREATE", "UPDATE"]
      resources: ["deployments"]
  validations:
  - expression: object.spec.replicas <= int(params.data.max)
    messageExpression: "'replicas must be at most ' + params.data.max"
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: replica-limit
spec:
  policyName: replica-limit
  validationActions: [Deny]
  paramRef:
    name: limits
    namespace: policy
    parameterNotFoundAction: Deny
`
	params := `apiVersion: v1
kind: ConfigMap
metadata:
  name: limits
  namespace: policy
data:
  max: "%s"
`
	inputs := AdmissionPolicyInputs{Policy: parseDocs(t, policy), Params: parseDocs(t, fmt.Sprintf(params, "5"))}
	result, err := vetVAPWith(t, vapDeployments, inputs)
	require.NoError(t, err)
	assert.Equal(t, []string{"apps/v1/Deployment prod/big"}, failedResources(result))
	assert.Equal(t, "replicas must be at most 5", result.FailedAttributes[0].Issues[0].Message)

	inputs.Params = parseDocs(t, fmt.Sprintf(params, "20"))
	result, err = vetVAPWith(t, vapDeployments, inputs)
	require.NoError(t, err)
	assert.True(t, result.Passed, "%v", result.Details)

	// Params in the policy Unit come first.
	inputs.Policy = parseDocs(t, policy+"---\n"+fmt.Sprintf(params, "1"))
	result, err = vetVAPWith(t, vapDeployments, inputs)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"apps/v1/Deployment prod/big", "apps/v1/Deployment dev/small"}, failedResources(result))
}

// A resource in the last released revision is evaluated as an update of it, with oldObject, so a
// policy about what may change works as it does at admission.
func TestVAPOldObjectFromReleasedRevision(t *testing.T) {
	policy := `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: replicas-halve
spec:
  matchConstraints:
    resourceRules:
    - apiGroups: ["apps"]
      apiVersions: ["v1"]
      operations: ["UPDATE"]
      resources: ["deployments"]
  validations:
  - expression: object.spec.replicas * 2 >= oldObject.spec.replicas
    messageExpression: "'replicas may not drop from ' + string(oldObject.spec.replicas) + ' to ' + string(object.spec.replicas)"
`
	released := strings.Replace(vapDeployments, "replicas: 2", "replicas: 8", 1)
	inputs := AdmissionPolicyInputs{Policy: parseDocs(t, policy), Released: parseDocs(t, released)}
	result, err := vetVAPWith(t, vapDeployments, inputs)
	require.NoError(t, err)
	assert.Equal(t, []string{"apps/v1/Deployment dev/small"}, failedResources(result))
	assert.Equal(t, "replicas may not drop from 8 to 2", result.FailedAttributes[0].Issues[0].Message)

	// A resource the released revision does not have is created, which an UPDATE policy does
	// not match; nor does it match anything in a Unit never released.
	inputs.Released = parseDocs(t, strings.Replace(released, "name: small", "name: other", 1))
	result, err = vetVAPWith(t, vapDeployments, inputs)
	require.NoError(t, err)
	assert.True(t, result.Passed, "%v", result.Details)

	inputs.Released = nil
	result, err = vetVAPWith(t, vapDeployments, inputs)
	require.NoError(t, err)
	assert.True(t, result.Passed, "%v", result.Details)
}

// A field written as null is unset to the API server, which decodes the object into its type
// before admission, so an expression guarding a field with has() sees no field, as in a cluster.
func TestVAPNullFieldIsUnset(t *testing.T) {
	const deployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
  labels: null
spec:
  template:
    spec:
      containers:
      - name: web
        image: nginx:1.27
        env:
        - name: FROM_SECRET
          value: null
          valueFrom:
            secretKeyRef:
              name: web
              key: password
        - name: MODE
          value: fast
`
	const policy = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: no-literal-secrets
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
    - apiGroups: ["apps"]
      apiVersions: ["v1"]
      operations: ["CREATE", "UPDATE"]
      resources: ["deployments"]
  validations:
  - expression: |
      object.spec.template.spec.containers.all(container, container.env.all(envVariable,
        !envVariable.value.matches("^secret") || !has(envVariable.value)))
  - expression: "!has(object.metadata.labels) || object.metadata.labels.all(key, key != 'secret')"
`
	docs, err := gaby.ParseAll([]byte(deployment))
	require.NoError(t, err)
	policyDocs, err := gaby.ParseAll([]byte(policy))
	require.NoError(t, err)
	_, output, err := VetValidatingAdmissionPolicy(testResourceProvider, api.NewFunctionOptions(), docs, policyDocs)
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, result.Passed, "%v", result.Details)
}
