// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/confighub/sdk/function/api"
)

// Test fixture constants
const (
	testNamespace = "test-ns"
	testAppName   = "test-app"
	testChartName = "test-chart"
)

// Common resource type constants for tests
var (
	// Workload resource types
	testResourceTypeDeployment  = api.ResourceType("apps/v1/Deployment")
	testResourceTypeStatefulSet = api.ResourceType("apps/v1/StatefulSet")
	testResourceTypeDaemonSet   = api.ResourceType("apps/v1/DaemonSet")
	testResourceTypeReplicaSet  = api.ResourceType("apps/v1/ReplicaSet")
	testResourceTypeJob         = api.ResourceType("batch/v1/Job")
	testResourceTypeCronJob     = api.ResourceType("batch/v1/CronJob")

	// Core resource types
	testResourceTypeNamespace      = api.ResourceType("v1/Namespace")
	testResourceTypeService        = api.ResourceType("v1/Service")
	testResourceTypeConfigMap      = api.ResourceType("v1/ConfigMap")
	testResourceTypeSecret         = api.ResourceType("v1/Secret")
	testResourceTypePod            = api.ResourceType("v1/Pod")
	testResourceTypeServiceAccount = api.ResourceType("v1/ServiceAccount")
	testResourceTypeResourceQuota  = api.ResourceType("v1/ResourceQuota")
	testResourceTypeLimitRange     = api.ResourceType("v1/LimitRange")

	// RBAC resource types
	testResourceTypeRole               = api.ResourceType("rbac.authorization.k8s.io/v1/Role")
	testResourceTypeRoleBinding        = api.ResourceType("rbac.authorization.k8s.io/v1/RoleBinding")
	testResourceTypeClusterRole        = api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRole")
	testResourceTypeClusterRoleBinding = api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRoleBinding")

	// Other resource types
	testResourceTypeCRD           = api.ResourceType("apiextensions.k8s.io/v1/CustomResourceDefinition")
	testResourceTypeIngress       = api.ResourceType("networking.k8s.io/v1/Ingress")
	testResourceTypeNetworkPolicy = api.ResourceType("networking.k8s.io/v1/NetworkPolicy")
	testResourceTypeStorageClass  = api.ResourceType("storage.k8s.io/v1/StorageClass")
)

// Common test data structures

// ResourceTypeTestCase represents a test case for resource type validation
type ResourceTypeTestCase struct {
	ResourceType api.ResourceType
	Expected     bool
	Description  string
}

// Common test data sets for resource type validation
var (
	// Workload resource test cases
	workloadResourceTestCases = []ResourceTypeTestCase{
		{testResourceTypeDeployment, true, "Deployment should be workload"},
		{testResourceTypeStatefulSet, true, "StatefulSet should be workload"},
		{testResourceTypeDaemonSet, true, "DaemonSet should be workload"},
		{testResourceTypeReplicaSet, true, "ReplicaSet should be workload"},
		{testResourceTypeJob, true, "Job should be workload"},
		{testResourceTypeCronJob, true, "CronJob should be workload"},
		{testResourceTypeService, false, "Service should not be workload"},
		{testResourceTypeConfigMap, false, "ConfigMap should not be workload"},
		{testResourceTypeSecret, false, "Secret should not be workload"},
	}

	// Cluster-scoped resource test cases
	clusterScopedResourceTestCases = []ResourceTypeTestCase{
		{testResourceTypeNamespace, true, "Namespace should be cluster-scoped"},
		{testResourceTypeClusterRole, true, "ClusterRole should be cluster-scoped"},
		{testResourceTypeClusterRoleBinding, true, "ClusterRoleBinding should be cluster-scoped"},
		{testResourceTypeCRD, true, "CRD should be cluster-scoped"},
		{testResourceTypeStorageClass, true, "StorageClass should be cluster-scoped"},
		{testResourceTypeService, false, "Service should not be cluster-scoped"},
		{testResourceTypeDeployment, false, "Deployment should not be cluster-scoped"},
		{testResourceTypeConfigMap, false, "ConfigMap should not be cluster-scoped"},
	}
)

// YAML templates for common Kubernetes resources
const (
	namespaceTemplate = `apiVersion: v1
kind: Namespace
metadata:
  name: %s`

	serviceAccountTemplate = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: %s`

	roleTemplate = `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: %s
  namespace: %s
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]`

	roleBindingTemplate = `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: %s
  namespace: %s
subjects:
- kind: ServiceAccount
  name: %s
  namespace: %s
roleRef:
  kind: Role
  name: %s
  apiGroup: rbac.authorization.k8s.io`

	resourceQuotaTemplate = `apiVersion: v1
kind: ResourceQuota
metadata:
  name: %s
  namespace: %s
spec:
  hard:
    requests.cpu: "2"
    requests.memory: 2G`

	crdTemplate = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: %s
spec:
  group: example.com
  versions:
  - name: v1
    served: true
    storage: true
  scope: Namespaced
  names:
    plural: %s
    singular: %s
    kind: %s`

	deploymentTemplate = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: %s
        image: nginx:latest`

	serviceTemplate = `apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  selector:
    app: %s
  ports:
  - port: 80
    targetPort: 8080`

	configMapTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  config.yaml: |
    %s`

	clusterRoleTemplate = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: %s
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]`

	clusterRoleBindingTemplate = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %s
subjects:
- kind: ServiceAccount
  name: %s
  namespace: %s
roleRef:
  kind: ClusterRole
  name: %s
  apiGroup: rbac.authorization.k8s.io`

	ingressTemplate = `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  rules:
  - host: %s
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: %s
            port:
              number: 80`

	podTemplate = `apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: main
    image: nginx:latest`

	helmReleaseTemplate = `apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: %s
  namespace: %s
spec:
  chart:
    spec:
      chart: %s
      version: "%s"
      sourceRef:
        kind: HelmRepository
        name: %s`
)

// Helper functions for creating individual resources

func createNamespace(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf(namespaceTemplate, name)
}

func createServiceAccount(t *testing.T, name, namespace string) string {
	t.Helper()
	return fmt.Sprintf(serviceAccountTemplate, name, namespace)
}

func createRole(t *testing.T, name, namespace string) string {
	t.Helper()
	return fmt.Sprintf(roleTemplate, name, namespace)
}

func createRoleBinding(t *testing.T, name, namespace, saName, saNamespace, roleName string) string {
	t.Helper()
	return fmt.Sprintf(roleBindingTemplate, name, namespace, saName, saNamespace, roleName)
}

func createResourceQuota(t *testing.T, name, namespace string) string {
	t.Helper()
	return fmt.Sprintf(resourceQuotaTemplate, name, namespace)
}

func createCRD(t *testing.T, name, plural, singular, kind string) string {
	t.Helper()
	return fmt.Sprintf(crdTemplate, name, plural, singular, kind)
}

func createDeployment(t *testing.T, name, namespace, app string) string {
	t.Helper()
	return fmt.Sprintf(deploymentTemplate, name, namespace, app, app, app, name)
}

func createService(t *testing.T, name, namespace, app string) string {
	t.Helper()
	if namespace == "" {
		// For cluster-scoped or without namespace, omit the namespace field
		return fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  labels:
    app: %s
spec:
  selector:
    app: %s
  ports:
  - port: 80
    targetPort: 8080`, name, app, app)
	}
	return fmt.Sprintf(serviceTemplate, name, namespace, app, app)
}

func createConfigMap(t *testing.T, name, namespace, data string) string {
	t.Helper()
	return fmt.Sprintf(configMapTemplate, name, namespace, data)
}

func createClusterRole(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf(clusterRoleTemplate, name)
}

func createClusterRoleBinding(t *testing.T, name, saName, saNamespace, roleName string) string {
	t.Helper()
	return fmt.Sprintf(clusterRoleBindingTemplate, name, saName, saNamespace, roleName)
}

func createIngress(t *testing.T, name, namespace, app, host, serviceName string) string {
	t.Helper()
	return fmt.Sprintf(ingressTemplate, name, namespace, app, host, serviceName)
}

func createPod(t *testing.T, name, namespace string) string {
	t.Helper()
	if namespace == "" {
		// For cluster-scoped or namespaced without namespace, we need to omit the namespace field
		return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
spec:
  containers:
  - name: main
    image: nginx:latest`, name)
	}
	return fmt.Sprintf(podTemplate, name, namespace)
}

// Helper functions for creating complex multi-resource fixtures

func createNamespaceWithPolicyResources(t *testing.T, namespace string) map[string]string {
	t.Helper()
	return map[string]string{
		"namespace.yaml": joinYAMLDocs(
			createNamespace(t, namespace),
			createServiceAccount(t, namespace+"-sa", namespace),
			createRole(t, namespace+"-role", namespace),
			createRoleBinding(t, namespace+"-binding", namespace, namespace+"-sa", namespace, namespace+"-role"),
			createResourceQuota(t, namespace+"-quota", namespace),
		),
	}
}

func createWorkloadResources(t *testing.T, appName, namespace string) map[string]string {
	t.Helper()
	return map[string]string{
		"workload.yaml": joinYAMLDocs(
			createDeployment(t, appName, namespace, appName),
			createService(t, appName, namespace, appName),
			createIngress(t, appName+"-ingress", namespace, appName, appName+".example.com", appName),
		),
	}
}

func createClusterRBACResources(t *testing.T, name string) map[string]string {
	t.Helper()
	return map[string]string{
		"cluster-rbac.yaml": joinYAMLDocs(
			createClusterRole(t, name+"-cluster-role"),
			createClusterRoleBinding(t, name+"-cluster-binding", name+"-sa", "default", name+"-cluster-role"),
		),
	}
}

func createMixedResources(t *testing.T, namespace, appName string) map[string]string {
	t.Helper()
	resources := make(map[string]string)

	// Add namespace resources
	for k, v := range createNamespaceWithPolicyResources(t, namespace) {
		resources[k] = v
	}

	// Add workload resources
	for k, v := range createWorkloadResources(t, appName, namespace) {
		resources[k] = v
	}

	// Add CRD
	resources["crd.yaml"] = createCRD(t, "mycustomresource.example.com", "mycustomresources", "mycustomresource", "MyCustomResource")

	// Add cluster RBAC
	for k, v := range createClusterRBACResources(t, appName) {
		resources[k] = v
	}

	// Add ConfigMap with varying sizes
	resources["configmap-small.yaml"] = createConfigMap(t, appName+"-config-small", namespace, "key: value")
	resources["configmap-large.yaml"] = createConfigMap(t, appName+"-config-large", namespace, strings.Repeat("key: value\n", 100))

	return resources
}

// Helper functions for creating test ResourceDocuments

func createTestResourceDocument(t *testing.T, resourceType api.ResourceType, name, namespace string, labels map[string]string, ownerRefs []OwnerReference) ResourceDocument {
	t.Helper()
	resourceName := api.ResourceName(name)
	if namespace != "" {
		resourceName = api.ResourceName(namespace + "/" + name)
	} else {
		resourceName = api.ResourceName("/" + name)
	}

	return ResourceDocument{
		ResourceType: resourceType,
		ResourceName: resourceName,
		Namespace:    namespace,
		Name:         name,
		Content:      fmt.Sprintf("# Test content for %s", name),
		Source:       fmt.Sprintf("# Source: test/%s", name),
		Labels:       labels,
		OwnerRefs:    ownerRefs,
	}
}

func createTestWorkloadDocument(t *testing.T, name, namespace, app string) ResourceDocument {
	t.Helper()
	labels := map[string]string{"app": app}
	return createTestResourceDocument(t, "apps/v1/Deployment", name, namespace, labels, nil)
}

func createTestServiceDocument(t *testing.T, name, namespace, app string) ResourceDocument {
	t.Helper()
	labels := map[string]string{"app": app}
	return createTestResourceDocument(t, "v1/Service", name, namespace, labels, nil)
}

func createTestNamespaceDocument(t *testing.T, name string) ResourceDocument {
	t.Helper()
	return createTestResourceDocument(t, "v1/Namespace", name, "", nil, nil)
}

func createTestConfigMapDocument(t *testing.T, name, namespace, content string) ResourceDocument {
	t.Helper()
	doc := createTestResourceDocument(t, "v1/ConfigMap", name, namespace, nil, nil)
	doc.Content = content
	return doc
}

// Utility helper functions

func joinYAMLDocs(docs ...string) string {
	return strings.Join(docs, "\n---\n")
}
