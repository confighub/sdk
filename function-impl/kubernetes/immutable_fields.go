// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import "github.com/confighub/sdk/function/api"

// resourceTypeToImmutablePaths maps Kubernetes resource types to their fields
// that are immutable after creation. Changing these fields requires deleting
// and recreating the resource rather than an in-place update.
//
// Derived from Kubernetes validation source code:
//   - pkg/apis/core/validation/validation.go
//   - pkg/apis/apps/validation/validation.go
//   - pkg/apis/batch/validation/validation.go
//   - pkg/apis/storage/validation/validation.go
//   - pkg/apis/networking/validation/validation.go
//   - pkg/apis/discovery/validation/validation.go
//   - pkg/apis/rbac/validation/validation.go
//   - pkg/apis/scheduling/validation/validation.go
//
// This list covers commonly managed resources and is not exhaustive.
var resourceTypeToImmutablePaths = map[api.ResourceType][]string{
	// Pod: nearly everything in spec is immutable except image,
	// activeDeadlineSeconds, tolerations, terminationGracePeriodSeconds,
	// and schedulingGates.
	api.ResourceType("v1/Pod"): {
		"spec.volumes",
		"spec.initContainers",
		"spec.containers",
		"spec.ephemeralContainers",
		"spec.restartPolicy",
		"spec.dnsPolicy",
		"spec.dnsConfig",
		"spec.nodeSelector",
		"spec.nodeName",
		"spec.serviceAccountName",
		"spec.automountServiceAccountToken",
		"spec.hostNetwork",
		"spec.hostPID",
		"spec.hostIPC",
		"spec.hostUsers",
		"spec.shareProcessNamespace",
		"spec.securityContext",
		"spec.imagePullSecrets",
		"spec.hostname",
		"spec.subdomain",
		"spec.setHostnameAsFQDN",
		"spec.affinity",
		"spec.schedulerName",
		"spec.hostAliases",
		"spec.priorityClassName",
		"spec.priority",
		"spec.preemptionPolicy",
		"spec.readinessGates",
		"spec.runtimeClassName",
		"spec.enableServiceLinks",
		"spec.overhead",
		"spec.topologySpreadConstraints",
		"spec.os",
		"spec.resourceClaims",
	},

	api.ResourceType("v1/Service"): {
		"spec.clusterIP",
		"spec.clusterIPs",
		"spec.ipFamilies",
		"spec.ipFamilyPolicy",
		"spec.healthCheckNodePort",
	},

	api.ResourceType("v1/PersistentVolume"): {
		"spec.persistentVolumeSource",
		"spec.volumeMode",
		"spec.storageClassName",
		"spec.nodeAffinity",
	},

	api.ResourceType("v1/PersistentVolumeClaim"): {
		"spec.accessModes",
		"spec.selector",
		"spec.storageClassName",
		"spec.volumeMode",
		"spec.volumeName",
		"spec.dataSource",
		"spec.dataSourceRef",
	},

	api.ResourceType("v1/Secret"): {
		"type",
	},

	api.ResourceType("v1/ResourceQuota"): {
		"spec.scopes",
	},

	api.ResourceType("apps/v1/DaemonSet"): {
		"spec.selector",
	},

	api.ResourceType("apps/v1/Deployment"): {
		"spec.selector",
	},

	api.ResourceType("apps/v1/ReplicaSet"): {
		"spec.selector",
	},

	api.ResourceType("apps/v1/StatefulSet"): {
		"spec.selector",
		"spec.serviceName",
		"spec.podManagementPolicy",
		"spec.volumeClaimTemplates",
	},

	api.ResourceType("batch/v1/Job"): {
		"spec.selector",
		"spec.completionMode",
		"spec.completions",
		"spec.template",
		"spec.podFailurePolicy",
		"spec.backoffLimitPerIndex",
		"spec.managedBy",
		"spec.successPolicy",
	},

	api.ResourceType("storage.k8s.io/v1/StorageClass"): {
		"parameters",
		"provisioner",
		"reclaimPolicy",
		"volumeBindingMode",
	},

	api.ResourceType("networking.k8s.io/v1/IngressClass"): {
		"spec.controller",
	},

	api.ResourceType("discovery.k8s.io/v1/EndpointSlice"): {
		"addressType",
	},

	api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRoleBinding"): {
		"roleRef",
	},

	api.ResourceType("rbac.authorization.k8s.io/v1/RoleBinding"): {
		"roleRef",
	},

	api.ResourceType("scheduling.k8s.io/v1/PriorityClass"): {
		"value",
		"preemptionPolicy",
	},

	// -------------------------------------------------------------------------
	// AWS ACK (AWS Controllers for Kubernetes)
	// Derived from generator.yaml is_immutable markers and CRD x-kubernetes-validations.
	// -------------------------------------------------------------------------

	// EKS
	api.ResourceType("eks.services.k8s.aws/v1alpha1/Nodegroup"): {
		"spec.amiType",
		"spec.clusterName",
		"spec.diskSize",
		"spec.instanceTypes",
		"spec.name",
		"spec.nodeRole",
		"spec.remoteAccess",
		"spec.subnets",
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/IdentityProviderConfig"): {
		"spec.clusterName",
		"spec.oidc",
		"spec.tags",
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/Capability"): {
		"spec.clusterName",
		"spec.name",
		"spec.type",
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/Cluster"): {
		"spec.encryptionConfig",
	},

	// EC2
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/VPCEndpoint"): {
		"spec.serviceNetworkARN",
		"spec.serviceRegion",
	},

	// RDS
	api.ResourceType("rds.services.k8s.aws/v1alpha1/DBInstance"): {
		"spec.availabilityZone",
	},

	// S3
	api.ResourceType("s3.services.k8s.aws/v1alpha1/Bucket"): {
		"spec.name",
	},

	// IAM
	api.ResourceType("iam.services.k8s.aws/v1alpha1/InstanceProfile"): {
		"spec.path",
	},
	api.ResourceType("iam.services.k8s.aws/v1alpha1/OpenIDConnectProvider"): {
		"spec.url",
	},
	api.ResourceType("iam.services.k8s.aws/v1alpha1/ServiceLinkedRole"): {
		"spec.awsServiceName",
		"spec.customSuffix",
	},
	api.ResourceType("iam.services.k8s.aws/v1alpha1/Policy"): {
		"spec.description",
	},

	// TODO: Determine whether this would be useful. Changing the name and/or namespace effectively
	// changes the identity of the resource. As does changing the kind. apiVersion is something people
	// need to be able to change.
	// api.ResourceType("*"): {
	// 	"metadata.name",
	// 	"metadata.namespace",
	// },
}
