// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"github.com/confighub/sdk/core/function/api"
)

// PathReference describes a field path that references another resource, and the
// ResourceType of the resource it refers to.
type PathReference struct {
	Path   string
	Target api.ResourceType
}

// CRDReferenceFields maps resource types to their cross-resource reference fields.
// These extend the kustomize NameReferenceFieldSpecs with CRD-specific references.
//
// Paths use gaby dot syntax: dot-separated, * for array wildcard.
// ResourceTypes use group/version/kind format.
//
// Only spec fields are included (not status). Deeply nested pod-spec references
// (env, envFrom, volumes, etc.) that follow the same pattern as built-in workloads
// are omitted since they are handled by the workload pod-spec traversal.
var CRDReferenceFields = map[api.ResourceType][]PathReference{
	// -------------------------------------------------------------------------
	// Argo CD
	// -------------------------------------------------------------------------
	api.ResourceType("argoproj.io/v1alpha1/Application"): {
		{Path: "spec.project", Target: "argoproj.io/v1alpha1/AppProject"},
	},

	// -------------------------------------------------------------------------
	// Argo Rollouts
	// -------------------------------------------------------------------------
	api.ResourceType("argoproj.io/v1alpha1/Rollout"): {
		{Path: "spec.strategy.blueGreen.activeService", Target: "v1/Service"},
		{Path: "spec.strategy.blueGreen.previewService", Target: "v1/Service"},
		{Path: "spec.strategy.canary.canaryService", Target: "v1/Service"},
		{Path: "spec.strategy.canary.stableService", Target: "v1/Service"},
		{Path: "spec.strategy.canary.pingPongService", Target: "v1/Service"},
		{Path: "spec.strategy.canary.trafficRouting.istio.virtualService.name", Target: "networking.istio.io/v1/VirtualService"},
		{Path: "spec.strategy.canary.trafficRouting.istio.destinationRule.name", Target: "networking.istio.io/v1/DestinationRule"},
	},

	// -------------------------------------------------------------------------
	// cert-manager
	// -------------------------------------------------------------------------
	api.ResourceType("cert-manager.io/v1/Certificate"): {
		// issuerRef.name refers to an Issuer or ClusterIssuer depending on issuerRef.kind
		{Path: "spec.issuerRef.name", Target: "cert-manager.io/v1/Issuer"},
		{Path: "spec.secretName", Target: "v1/Secret"},
	},
	api.ResourceType("cert-manager.io/v1/Issuer"): {
		{Path: "spec.ca.secretName", Target: "v1/Secret"},
		{Path: "spec.acme.solvers.*.http01.ingress.podTemplate.spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.vault.auth.kubernetes.serviceAccountRef.name", Target: "v1/ServiceAccount"},
		{Path: "spec.vault.auth.clientCertificate.secretName", Target: "v1/Secret"},
	},
	api.ResourceType("cert-manager.io/v1/ClusterIssuer"): {
		{Path: "spec.ca.secretName", Target: "v1/Secret"},
		{Path: "spec.acme.solvers.*.http01.ingress.podTemplate.spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.vault.auth.kubernetes.serviceAccountRef.name", Target: "v1/ServiceAccount"},
		{Path: "spec.vault.auth.clientCertificate.secretName", Target: "v1/Secret"},
	},

	// -------------------------------------------------------------------------
	// Flux CD - Kustomize Controller
	// -------------------------------------------------------------------------
	api.ResourceType("kustomize.toolkit.fluxcd.io/v1/Kustomization"): {
		{Path: "spec.sourceRef.name", Target: "source.toolkit.fluxcd.io/v1/GitRepository"},
		{Path: "spec.decryption.secretRef.name", Target: "v1/Secret"},
		{Path: "spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.kubeConfig.secretRef.name", Target: "v1/Secret"},
	},

	// -------------------------------------------------------------------------
	// Flux CD - Helm Controller
	// -------------------------------------------------------------------------
	api.ResourceType("helm.toolkit.fluxcd.io/v2/HelmRelease"): {
		// chart.spec.sourceRef.name refers to a HelmRepository, GitRepository, or Bucket
		{Path: "spec.chart.spec.sourceRef.name", Target: "source.toolkit.fluxcd.io/v1/HelmRepository"},
		// chartRef.name refers to an OCIRepository or HelmChart
		{Path: "spec.chartRef.name", Target: "source.toolkit.fluxcd.io/v1/HelmChart"},
		{Path: "spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.kubeConfig.secretRef.name", Target: "v1/Secret"},
		{Path: "spec.valuesFrom.*.name", Target: "v1/ConfigMap"},
	},

	// -------------------------------------------------------------------------
	// Flux CD - Source Controller
	// -------------------------------------------------------------------------
	api.ResourceType("source.toolkit.fluxcd.io/v1/GitRepository"): {
		{Path: "spec.secretRef.name", Target: "v1/Secret"},
	},
	api.ResourceType("source.toolkit.fluxcd.io/v1/HelmRepository"): {
		{Path: "spec.secretRef.name", Target: "v1/Secret"},
	},
	api.ResourceType("source.toolkit.fluxcd.io/v1/HelmChart"): {
		{Path: "spec.sourceRef.name", Target: "source.toolkit.fluxcd.io/v1/HelmRepository"},
		{Path: "spec.valuesFiles.*.name", Target: "v1/ConfigMap"},
	},
	api.ResourceType("source.toolkit.fluxcd.io/v1beta2/OCIRepository"): {
		{Path: "spec.secretRef.name", Target: "v1/Secret"},
		{Path: "spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.certSecretRef.name", Target: "v1/Secret"},
	},
	api.ResourceType("source.toolkit.fluxcd.io/v1beta2/Bucket"): {
		{Path: "spec.secretRef.name", Target: "v1/Secret"},
	},

	// -------------------------------------------------------------------------
	// Flux CD - Notification Controller
	// -------------------------------------------------------------------------
	api.ResourceType("notification.toolkit.fluxcd.io/v1beta3/Provider"): {
		{Path: "spec.secretRef.name", Target: "v1/Secret"},
		{Path: "spec.certSecretRef.name", Target: "v1/Secret"},
	},
	api.ResourceType("notification.toolkit.fluxcd.io/v1beta3/Alert"): {
		{Path: "spec.providerRef.name", Target: "notification.toolkit.fluxcd.io/v1beta3/Provider"},
	},

	// -------------------------------------------------------------------------
	// Flux CD - Image Automation
	// -------------------------------------------------------------------------
	api.ResourceType("image.toolkit.fluxcd.io/v1beta2/ImageRepository"): {
		{Path: "spec.secretRef.name", Target: "v1/Secret"},
		{Path: "spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.certSecretRef.name", Target: "v1/Secret"},
	},
	api.ResourceType("image.toolkit.fluxcd.io/v1beta2/ImagePolicy"): {
		{Path: "spec.imageRepositoryRef.name", Target: "image.toolkit.fluxcd.io/v1beta2/ImageRepository"},
	},
	api.ResourceType("image.toolkit.fluxcd.io/v1beta2/ImageUpdateAutomation"): {
		{Path: "spec.sourceRef.name", Target: "source.toolkit.fluxcd.io/v1/GitRepository"},
	},

	// -------------------------------------------------------------------------
	// External Secrets Operator
	// -------------------------------------------------------------------------
	api.ResourceType("external-secrets.io/v1beta1/ExternalSecret"): {
		// secretStoreRef.name refers to SecretStore or ClusterSecretStore depending on kind
		{Path: "spec.secretStoreRef.name", Target: "external-secrets.io/v1beta1/SecretStore"},
		{Path: "spec.target.name", Target: "v1/Secret"},
		{Path: "spec.data.*.sourceRef.storeRef.name", Target: "external-secrets.io/v1beta1/SecretStore"},
		{Path: "spec.dataFrom.*.sourceRef.storeRef.name", Target: "external-secrets.io/v1beta1/SecretStore"},
		{Path: "spec.target.template.templateFrom.*.configMap.name", Target: "v1/ConfigMap"},
		{Path: "spec.target.template.templateFrom.*.secret.name", Target: "v1/Secret"},
	},
	api.ResourceType("external-secrets.io/v1beta1/SecretStore"): {
		{Path: "spec.provider.kubernetes.auth.serviceAccount.name", Target: "v1/ServiceAccount"},
	},
	api.ResourceType("external-secrets.io/v1beta1/ClusterSecretStore"): {
		{Path: "spec.provider.kubernetes.auth.serviceAccount.name", Target: "v1/ServiceAccount"},
	},
	// v1 is the current stable API; same reference paths as v1beta1.
	api.ResourceType("external-secrets.io/v1/ExternalSecret"): {
		{Path: "spec.secretStoreRef.name", Target: "external-secrets.io/v1/SecretStore"},
		{Path: "spec.target.name", Target: "v1/Secret"},
		{Path: "spec.data.*.sourceRef.storeRef.name", Target: "external-secrets.io/v1/SecretStore"},
		{Path: "spec.dataFrom.*.sourceRef.storeRef.name", Target: "external-secrets.io/v1/SecretStore"},
		{Path: "spec.target.template.templateFrom.*.configMap.name", Target: "v1/ConfigMap"},
		{Path: "spec.target.template.templateFrom.*.secret.name", Target: "v1/Secret"},
	},
	api.ResourceType("external-secrets.io/v1/SecretStore"): {
		{Path: "spec.provider.kubernetes.auth.serviceAccount.name", Target: "v1/ServiceAccount"},
	},
	api.ResourceType("external-secrets.io/v1/ClusterSecretStore"): {
		{Path: "spec.provider.kubernetes.auth.serviceAccount.name", Target: "v1/ServiceAccount"},
	},

	// -------------------------------------------------------------------------
	// Istio - Networking
	// -------------------------------------------------------------------------
	api.ResourceType("networking.istio.io/v1/VirtualService"): {
		{Path: "spec.http.*.route.*.destination.host", Target: "v1/Service"},
		{Path: "spec.http.*.mirror.host", Target: "v1/Service"},
		{Path: "spec.http.*.mirrors.*.destination.host", Target: "v1/Service"},
		{Path: "spec.tcp.*.route.*.destination.host", Target: "v1/Service"},
		{Path: "spec.tls.*.route.*.destination.host", Target: "v1/Service"},
	},
	api.ResourceType("networking.istio.io/v1/DestinationRule"): {
		{Path: "spec.host", Target: "v1/Service"},
	},
	api.ResourceType("networking.istio.io/v1/Gateway"): {
		{Path: "spec.servers.*.tls.credentialName", Target: "v1/Secret"},
	},

	// -------------------------------------------------------------------------
	// Istio - Security
	// -------------------------------------------------------------------------
	// -------------------------------------------------------------------------
	// Gateway API
	// -------------------------------------------------------------------------
	api.ResourceType("gateway.networking.k8s.io/v1/Gateway"): {
		{Path: "spec.gatewayClassName", Target: "gateway.networking.k8s.io/v1/GatewayClass"},
		{Path: "spec.listeners.*.tls.certificateRefs.*.name", Target: "v1/Secret"},
	},
	api.ResourceType("gateway.networking.k8s.io/v1/HTTPRoute"): {
		{Path: "spec.rules.*.backendRefs.*.name", Target: "v1/Service"},
		{Path: "spec.rules.*.filters.*.requestMirror.backendRef.name", Target: "v1/Service"},
	},
	api.ResourceType("gateway.networking.k8s.io/v1/GRPCRoute"): {
		{Path: "spec.rules.*.backendRefs.*.name", Target: "v1/Service"},
	},
	api.ResourceType("gateway.networking.k8s.io/v1alpha2/TCPRoute"): {
		{Path: "spec.rules.*.backendRefs.*.name", Target: "v1/Service"},
	},
	api.ResourceType("gateway.networking.k8s.io/v1alpha2/UDPRoute"): {
		{Path: "spec.rules.*.backendRefs.*.name", Target: "v1/Service"},
	},
	// -------------------------------------------------------------------------
	// Traefik
	// -------------------------------------------------------------------------
	api.ResourceType("traefik.io/v1alpha1/IngressRoute"): {
		{Path: "spec.tls.secretName", Target: "v1/Secret"},
		{Path: "spec.routes.*.services.*.name", Target: "v1/Service"},
		{Path: "spec.routes.*.middlewares.*.name", Target: "traefik.io/v1alpha1/Middleware"},
	},
	api.ResourceType("traefik.io/v1alpha1/IngressRouteTCP"): {
		{Path: "spec.tls.secretName", Target: "v1/Secret"},
		{Path: "spec.routes.*.services.*.name", Target: "v1/Service"},
	},
	api.ResourceType("traefik.io/v1alpha1/IngressRouteUDP"): {
		{Path: "spec.routes.*.services.*.name", Target: "v1/Service"},
	},

	// -------------------------------------------------------------------------
	// Contour
	// -------------------------------------------------------------------------
	api.ResourceType("projectcontour.io/v1/HTTPProxy"): {
		{Path: "spec.virtualhost.tls.secretName", Target: "v1/Secret"},
		{Path: "spec.routes.*.services.*.name", Target: "v1/Service"},
		{Path: "spec.includes.*.name", Target: "projectcontour.io/v1/HTTPProxy"},
	},
	api.ResourceType("projectcontour.io/v1/TLSCertificateDelegation"): {
		{Path: "spec.delegations.*.secretName", Target: "v1/Secret"},
	},

	// -------------------------------------------------------------------------
	// Prometheus Operator
	// -------------------------------------------------------------------------
	api.ResourceType("monitoring.coreos.com/v1/Prometheus"): {
		{Path: "spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.serviceName", Target: "v1/Service"},
		{Path: "spec.alerting.alertmanagers.*.name", Target: "v1/Service"},
	},
	api.ResourceType("monitoring.coreos.com/v1/Alertmanager"): {
		{Path: "spec.serviceAccountName", Target: "v1/ServiceAccount"},
		{Path: "spec.configSecret", Target: "v1/Secret"},
	},

	// -------------------------------------------------------------------------
	// Crossplane
	// -------------------------------------------------------------------------
	api.ResourceType("apiextensions.crossplane.io/v1/Composition"): {
		{Path: "spec.compositeTypeRef.kind", Target: "apiextensions.crossplane.io/v1/CompositeResourceDefinition"},
	},
	api.ResourceType("pkg.crossplane.io/v1/Provider"): {
		{Path: "spec.runtimeConfigRef.name", Target: "pkg.crossplane.io/v1beta1/DeploymentRuntimeConfig"},
	},

	// -------------------------------------------------------------------------
	// Argo Workflows
	// -------------------------------------------------------------------------
	api.ResourceType("argoproj.io/v1alpha1/CronWorkflow"): {
		{Path: "spec.workflowSpec.serviceAccountName", Target: "v1/ServiceAccount"},
	},
	api.ResourceType("argoproj.io/v1alpha1/Workflow"): {
		{Path: "spec.serviceAccountName", Target: "v1/ServiceAccount"},
	},

	// -------------------------------------------------------------------------
	// Argo ApplicationSet
	// -------------------------------------------------------------------------
	api.ResourceType("argoproj.io/v1alpha1/ApplicationSet"): {
		{Path: "spec.generators.*.plugin.configMapRef.name", Target: "v1/ConfigMap"},
		{Path: "spec.generators.*.matrix.generators.*.plugin.configMapRef.name", Target: "v1/ConfigMap"},
		{Path: "spec.generators.*.merge.generators.*.plugin.configMapRef.name", Target: "v1/ConfigMap"},
	},

	// -------------------------------------------------------------------------
	// AWS ACK - EC2 Controller
	// ACK references use spec.<field>Ref.from.name to reference other ACK CRs.
	// -------------------------------------------------------------------------
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/DHCPOptions"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.vpcRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/InternetGateway"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.routeTableRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/RouteTable"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/NATGateway"): {
		{Path: "spec.subnetRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
		{Path: "spec.allocationRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/ElasticIPAddress"},
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/NetworkACL"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.subnetRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/RouteTable"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.routes.*.gatewayRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/InternetGateway"},
		{Path: "spec.routes.*.natGatewayRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/NATGateway"},
		{Path: "spec.routes.*.transitGatewayRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/TransitGateway"},
		{Path: "spec.routes.*.vpcEndpointRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPCEndpoint"},
		{Path: "spec.routes.*.vpcPeeringConnectionRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPCPeeringConnection"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/SecurityGroup"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.ingressRules.*.groupRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
		{Path: "spec.egressRules.*.groupRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/Subnet"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.routeTableRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/RouteTable"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/TransitGatewayVPCAttachment"): {
		{Path: "spec.transitGatewayRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/TransitGateway"},
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.subnetRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/VPCEndpoint"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.subnetRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
		{Path: "spec.securityGroupRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
		{Path: "spec.routeTableRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/RouteTable"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/VPCPeeringConnection"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
		{Path: "spec.peerVPCRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
	},
	api.ResourceType("ec2.services.k8s.aws/v1alpha1/Instance"): {
		{Path: "spec.subnetRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
		{Path: "spec.launchTemplate.launchTemplateRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/LaunchTemplate"},
	},

	// -------------------------------------------------------------------------
	// AWS ACK - EKS Controller
	// -------------------------------------------------------------------------
	api.ResourceType("eks.services.k8s.aws/v1alpha1/AccessEntry"): {
		{Path: "spec.clusterRef.from.name", Target: "eks.services.k8s.aws/v1alpha1/Cluster"},
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/Addon"): {
		{Path: "spec.clusterRef.from.name", Target: "eks.services.k8s.aws/v1alpha1/Cluster"},
		{Path: "spec.serviceAccountRoleRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Role"},
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/Cluster"): {
		{Path: "spec.roleRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Role"},
		{Path: "spec.resourcesVPCConfig.subnetRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
		{Path: "spec.resourcesVPCConfig.securityGroupRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/FargateProfile"): {
		{Path: "spec.clusterRef.from.name", Target: "eks.services.k8s.aws/v1alpha1/Cluster"},
		{Path: "spec.podExecutionRoleRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Role"},
		{Path: "spec.subnetRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/IdentityProviderConfig"): {
		{Path: "spec.clusterRef.from.name", Target: "eks.services.k8s.aws/v1alpha1/Cluster"},
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/Nodegroup"): {
		{Path: "spec.clusterRef.from.name", Target: "eks.services.k8s.aws/v1alpha1/Cluster"},
		{Path: "spec.nodeRoleRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Role"},
		{Path: "spec.subnetRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
		{Path: "spec.remoteAccess.sourceSecurityGroupRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
	},
	api.ResourceType("eks.services.k8s.aws/v1alpha1/PodIdentityAssociation"): {
		{Path: "spec.clusterRef.from.name", Target: "eks.services.k8s.aws/v1alpha1/Cluster"},
		{Path: "spec.roleRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Role"},
	},

	// -------------------------------------------------------------------------
	// AWS ACK - ELBv2 Controller
	// -------------------------------------------------------------------------
	api.ResourceType("elbv2.services.k8s.aws/v1alpha1/Listener"): {
		{Path: "spec.loadBalancerRef.from.name", Target: "elbv2.services.k8s.aws/v1alpha1/LoadBalancer"},
		{Path: "spec.defaultActions.*.targetGroupRef.from.name", Target: "elbv2.services.k8s.aws/v1alpha1/TargetGroup"},
		{Path: "spec.defaultActions.*.forwardConfig.targetGroups.*.targetGroupRef.from.name", Target: "elbv2.services.k8s.aws/v1alpha1/TargetGroup"},
	},
	api.ResourceType("elbv2.services.k8s.aws/v1alpha1/LoadBalancer"): {
		{Path: "spec.subnetRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
		{Path: "spec.securityGroupRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
	},
	api.ResourceType("elbv2.services.k8s.aws/v1alpha1/Rule"): {
		{Path: "spec.listenerRef.from.name", Target: "elbv2.services.k8s.aws/v1alpha1/Listener"},
		{Path: "spec.actions.*.targetGroupRef.from.name", Target: "elbv2.services.k8s.aws/v1alpha1/TargetGroup"},
		{Path: "spec.actions.*.forwardConfig.targetGroups.*.targetGroupRef.from.name", Target: "elbv2.services.k8s.aws/v1alpha1/TargetGroup"},
	},
	api.ResourceType("elbv2.services.k8s.aws/v1alpha1/TargetGroup"): {
		{Path: "spec.vpcRef.from.name", Target: "ec2.services.k8s.aws/v1alpha1/VPC"},
	},

	// -------------------------------------------------------------------------
	// AWS ACK - IAM Controller
	// -------------------------------------------------------------------------
	api.ResourceType("iam.services.k8s.aws/v1alpha1/InstanceProfile"): {
		{Path: "spec.roleRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Role"},
	},
	api.ResourceType("iam.services.k8s.aws/v1alpha1/Group"): {
		{Path: "spec.policyRefs.*.from.name", Target: "iam.services.k8s.aws/v1alpha1/Policy"},
	},
	api.ResourceType("iam.services.k8s.aws/v1alpha1/Role"): {
		{Path: "spec.policyRefs.*.from.name", Target: "iam.services.k8s.aws/v1alpha1/Policy"},
		{Path: "spec.permissionsBoundaryRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Policy"},
	},
	api.ResourceType("iam.services.k8s.aws/v1alpha1/User"): {
		{Path: "spec.policyRefs.*.from.name", Target: "iam.services.k8s.aws/v1alpha1/Policy"},
		{Path: "spec.permissionsBoundaryRef.from.name", Target: "iam.services.k8s.aws/v1alpha1/Policy"},
	},

	// -------------------------------------------------------------------------
	// AWS ACK - RDS Controller
	// -------------------------------------------------------------------------
	api.ResourceType("rds.services.k8s.aws/v1alpha1/DBCluster"): {
		{Path: "spec.dbClusterParameterGroupRef.from.name", Target: "rds.services.k8s.aws/v1alpha1/DBClusterParameterGroup"},
		{Path: "spec.dbSubnetGroupRef.from.name", Target: "rds.services.k8s.aws/v1alpha1/DBSubnetGroup"},
		{Path: "spec.vpcSecurityGroupRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
	},
	api.ResourceType("rds.services.k8s.aws/v1alpha1/DBClusterEndpoint"): {
		{Path: "spec.dbClusterIdentifierRef.from.name", Target: "rds.services.k8s.aws/v1alpha1/DBCluster"},
	},
	api.ResourceType("rds.services.k8s.aws/v1alpha1/DBClusterSnapshot"): {
		{Path: "spec.dbClusterIdentifierRef.from.name", Target: "rds.services.k8s.aws/v1alpha1/DBCluster"},
	},
	api.ResourceType("rds.services.k8s.aws/v1alpha1/DBInstance"): {
		{Path: "spec.dbParameterGroupRef.from.name", Target: "rds.services.k8s.aws/v1alpha1/DBParameterGroup"},
		{Path: "spec.dbSubnetGroupRef.from.name", Target: "rds.services.k8s.aws/v1alpha1/DBSubnetGroup"},
		{Path: "spec.vpcSecurityGroupRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/SecurityGroup"},
	},
	api.ResourceType("rds.services.k8s.aws/v1alpha1/DBSnapshot"): {
		{Path: "spec.dbInstanceIdentifierRef.from.name", Target: "rds.services.k8s.aws/v1alpha1/DBInstance"},
	},
	api.ResourceType("rds.services.k8s.aws/v1alpha1/DBSubnetGroup"): {
		{Path: "spec.subnetRefs.*.from.name", Target: "ec2.services.k8s.aws/v1alpha1/Subnet"},
	},
}
