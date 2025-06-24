// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

var fakeContext api.FunctionContext = api.FunctionContext{
	UnitDisplayName: "MyK8sUnit",
	New:             true,
}

func stringArgsToFunctionArgs(stringArgs []string) []api.FunctionArgument {
	args := make([]api.FunctionArgument, len(stringArgs))
	for i, stringArg := range stringArgs {
		args[i].Value = stringArg
	}
	return args
}

func TestK8sFnSetImage(t *testing.T) {
	// Define test cases for each supported resource
	testCases := []struct {
		name          string
		yamlFixture   string
		args          []string
		expectedImage string
	}{
		{
			name: "Deployment apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.15.0"},
			expectedImage: "nginx:1.15.0",
		},
		{
			name: "ReplicaSet apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: example-replicaset
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.16.0"},
			expectedImage: "nginx:1.16.0",
		},
		{
			name: "DaemonSet apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: example-daemonset
spec:
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.17.0"},
			expectedImage: "nginx:1.17.0",
		},
		{
			name: "StatefulSet apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: example-statefulset
spec:
  serviceName: "example-service"
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.18.0"},
			expectedImage: "nginx:1.18.0",
		},
		{
			name: "Job batch/v1",
			yamlFixture: `
apiVersion: batch/v1
kind: Job
metadata:
  name: example-job
spec:
  template:
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
      restartPolicy: Never
`,
			args:          []string{"example-container", "nginx:1.19.0"},
			expectedImage: "nginx:1.19.0",
		},
		{
			name: "CronJob batch/v1",
			yamlFixture: `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: example-cronjob
spec:
  schedule: "*/1 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: example-container
            image: nginx:1.14.2
          restartPolicy: OnFailure
`,
			args:          []string{"example-container", "nginx:1.20.0"},
			expectedImage: "nginx:1.20.0",
		},
		{
			name: "CronJob batch/v1",
			yamlFixture: `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: example-cronjob
spec:
  schedule: "*/1 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: example-container
            image: nginx:1.14.2
          restartPolicy: OnFailure
`,
			args:          []string{"example-container", "nginx:1.21.0"},
			expectedImage: "nginx:1.21.0",
		},
		{
			name: "Pod core/v1",
			yamlFixture: `
apiVersion: v1
kind: Pod
metadata:
  name: example-pod
spec:
  containers:
  - name: example-container
    image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.14.2"},
			expectedImage: "nginx:1.14.2",
		},
		{
			name: "Deployment apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 2
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.22.0"},
			expectedImage: "nginx:1.22.0",
		},
		{
			name: "ReplicaSet apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: example-replicaset
spec:
  replicas: 3
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.23.0"},
			expectedImage: "nginx:1.23.0",
		},
		{
			name: "DaemonSet apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: example-daemonset
spec:
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.24.0"},
			expectedImage: "nginx:1.24.0",
		},
		{
			name: "Deployment apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 2
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.25.0"},
			expectedImage: "nginx:1.25.0",
		},
		{
			name: "Deployment apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 2
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.14.2"},
			expectedImage: "nginx:1.14.2",
		},
		{
			name: "StatefulSet apps/v1",
			yamlFixture: `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: example-statefulset
spec:
  serviceName: "example-service"
  replicas: 3
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`,
			args:          []string{"example-container", "nginx:1.26.0"},
			expectedImage: "nginx:1.26.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			docs, err := gaby.ParseAll([]byte(tc.yamlFixture))
			assert.NoError(t, err)

			newYaml, _, err := setImageHandler(&fakeContext, docs, stringArgsToFunctionArgs(tc.args), []byte{})
			assert.NoError(t, err)
			assert.Contains(t, newYaml.String(), tc.expectedImage)
		})
	}
}

func TestK8sFnSetImageReference(t *testing.T) {
	// Multi-doc YAML fixture
	yamlFixture := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      # This is the container we want to change the image for
      - name: example-container
        image: nginx:1.14.2
        ports:
        - containerPort: 80
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	newYaml, _, err := setImageReferenceHandler(&fakeContext, docs, stringArgsToFunctionArgs([]string{"example-container", ":1.15.0"}), []byte{})
	assert.NoError(t, err)
	assert.Contains(t, newYaml.String(), "nginx:1.15.0")
}

func TestK8sFnSetImageUri(t *testing.T) {
	// Multi-doc YAML fixture
	yamlFixture := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      # This is the container we want to change the image for
      - name: example-container
        image: example.foo.bar:30100/nginx:1.14.2
        ports:
        - containerPort: 80
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	newYaml, _, err := setImageUriHandler(&fakeContext, docs, stringArgsToFunctionArgs([]string{"example-container", "nginx-plus"}), []byte{})
	assert.NoError(t, err)
	assert.Contains(t, newYaml.String(), "nginx-plus:1.14.2")
}

func TestK8sFnSetEnv(t *testing.T) {
	yamlTestFixture := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: test-container
    image: busybox
    env:
    - name: EXISTING_VAR
      value: existing_value
`

	// Parse the YAML
	configYaml, err := gaby.ParseAll([]byte(yamlTestFixture))
	assert.NoError(t, err)

	// Define arguments
	args := []string{"test-container", "NEW_VAR=new_value", "EXISTING_VAR="}

	// Call the function
	output, _, err := k8sFnSetEnv(&fakeContext, configYaml, stringArgsToFunctionArgs(args), []byte{})
	assert.NoError(t, err)

	// Expected output
	expectedYaml := `apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
    - image: busybox
      name: test-container
      env:
        - name: NEW_VAR
          value: new_value
`

	// Compare the output with expected YAML
	assert.YAMLEq(t, expectedYaml, output.String())
}

func TestK8sFnSetEnv_Duplicated(t *testing.T) {
	yamlTestFixture := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: acme-todo
  namespace: acme-todo-ns
  annotations:
    confighub.com/resolved-at: "2024-10-14T10:32:57-07:00"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: acme-todo
  template:
    metadata:
      labels:
        app: acme-todo
    spec:
      containers:
      - name: acme-todo-api
        image: someurl:mytag
        ports:
        - containerPort: 8080
        env:
        - name: asdf
          value: sdddd
        - name: asdf
          value: sdddd2
      - name: acme-todo-frontend
        image: ghcr.io/confighubai/acme-todo/ui:main-44636a1
        ports:
        - containerPort: 80
        env:
        - name: PROXY_ADDRESS
          value: acme-todo-api:8080
      imagePullSecrets:
      - name: registry-secret
`
	configYaml, err := gaby.ParseAll([]byte(yamlTestFixture))
	if err != nil {
		t.Fatalf("Failed to parse YAML: %v", err)
	}

	// Define arguments
	args := []string{
		"acme-todo-frontend",
		"PROXY_ADDRESS=acme-todo-api:8080",
		"FOO=44111",
		"DDD=111",
	}

	// call the function
	output, _, err := k8sFnSetEnv(&fakeContext, configYaml, stringArgsToFunctionArgs(args), []byte{})
	assert.NoError(t, err)

	// call the function again
	configYaml = output
	output, _, err = k8sFnSetEnv(&fakeContext, configYaml, stringArgsToFunctionArgs(args), []byte{})
	assert.NoError(t, err)

	expectedYaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: acme-todo
  namespace: acme-todo-ns
  annotations:
    confighub.com/resolved-at: "2024-10-14T10:32:57-07:00"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: acme-todo
  template:
    metadata:
      labels:
        app: acme-todo
    spec:
      containers:
      - name: acme-todo-api
        image: someurl:mytag
        ports:
        - containerPort: 8080
        env:
        - name: asdf
          value: sdddd
        - name: asdf
          value: sdddd2
      - name: acme-todo-frontend
        image: ghcr.io/confighubai/acme-todo/ui:main-44636a1
        ports:
        - containerPort: 80
        env:
        - name: PROXY_ADDRESS
          value: acme-todo-api:8080
        - name: FOO
          value: "44111"
        - name: DDD
          value: "111"
      imagePullSecrets:
      - name: registry-secret
`
	assert.YAMLEq(t, expectedYaml, output.String())
}
