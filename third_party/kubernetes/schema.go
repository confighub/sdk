package k8sschema

import (
	_ "embed"
)

//go:embed swagger.json
var K8sSchema []byte
