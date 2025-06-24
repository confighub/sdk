package yamlpatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPatch_WithComments(t *testing.T) {
	original := `# This is a Kubernetes Deployment for nginx
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  # Labels for the deployment
  labels:
    app: nginx
spec:
  # Number of replicas to run
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      # Main nginx container
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
`

	patch := `
- op: add
  path: /spec/template/spec/containers/0/env
  value:
  - name: NGINX_PORT
    value: "80"
`

	expected := `# This is a Kubernetes Deployment for nginx
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  # Labels for the deployment
  labels:
    app: nginx
spec:
  # Number of replicas to run
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      # Main nginx container
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
        env:
        - name: NGINX_PORT
          value: "80"
`
	result, err := applyPatch(original, patch)
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestPatch_WithCommentsInPatchContents(t *testing.T) {
	original := `# This is a Kubernetes Deployment for nginx
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  # Labels for the deployment
  labels:
    app: nginx
spec:
  # Number of replicas to run
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      # Main nginx container
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
`

	patch := `
- op: add
  path: /spec/template/spec/containers/0/env
  value:
  - # NGINX_PORT declaration  
    name: NGINX_PORT # name
    value: "80" # this is the default port
`

	expected := `# This is a Kubernetes Deployment for nginx
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  # Labels for the deployment
  labels:
    app: nginx
spec:
  # Number of replicas to run
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      # Main nginx container
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
        env:
        - name: NGINX_PORT
          value: "80"
`
	result, err := applyPatch(original, patch)
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}
