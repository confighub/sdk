package gaby

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMultiDoc(t *testing.T) {
	sample := []byte(
		`test:
  value: 10
---
test2: 20
`)
	val, err := ParseAll(sample)
	if err != nil {
		t.Errorf("Failed to parse: %v", err)
		return
	}
	if val.Search("test", "value").Data() == nil {
		t.Errorf("Didn't find test.value")
	}
	if val.Search("test2").Data() == nil {
		t.Errorf("Didn't find test2")
	}
}

func TestStrangeMultiDoc(t *testing.T) {
	sample := []byte(
		`---

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cert-manager-edit
  labels:
    app: cert-manager
    app.kubernetes.io/name: cert-manager
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/component: "controller"
    app.kubernetes.io/version: "v1.17.2"
    app.kubernetes.io/managed-by: Helm
    helm.sh/chart: cert-manager-v1.17.2
    rbac.authorization.k8s.io/aggregate-to-edit: "true"
    rbac.authorization.k8s.io/aggregate-to-admin: "true"
rules:
  - apiGroups: ["cert-manager.io"]
    resources: ["certificates", "certificaterequests", "issuers"]
    verbs: ["create", "delete", "deletecollection", "patch", "update"]
  - apiGroups: ["cert-manager.io"]
    resources: ["certificates/status"]
    verbs: ["update"]
  - apiGroups: ["acme.cert-manager.io"]
    resources: ["challenges", "orders"]
    verbs: ["create", "delete", "deletecollection", "patch", "update"]

---# Permission to approve CertificateRequests referencing cert-manager.io Issuers and ClusterIssuers
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cert-manager-controller-approve:cert-manager-io
  labels:
    app: cert-manager
    app.kubernetes.io/name: cert-manager
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/component: "cert-manager"
    app.kubernetes.io/version: "v1.17.2"
    app.kubernetes.io/managed-by: Helm
    helm.sh/chart: cert-manager-v1.17.2
rules:
  - apiGroups: ["cert-manager.io"]
    resources: ["signers"]
    verbs: ["approve"]
    resourceNames:
    - "issuers.cert-manager.io/*"
    - "clusterissuers.cert-manager.io/*"
`)
	docs, err := ParseAll(sample)
	assert.NoError(t, err, "Error parsing YAML")
	assert.Equal(t, 2, len(docs), "Expected 2 documents")
}
