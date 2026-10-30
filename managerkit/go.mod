module github.com/confighub/sdk/managerkit

go 1.26.0

require (
	github.com/confighub/sdk/cliutil v0.0.0
	github.com/confighub/sdk/core v0.0.0
	github.com/confighub/sdk/cubutil v0.0.0
	github.com/google/uuid v1.6.0
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/a8m/envsubst v1.4.3 // indirect
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/alecthomas/participle/v2 v2.1.4 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/cockroachdb/errors v1.11.3 // indirect
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b // indirect
	github.com/cockroachdb/redact v1.1.5 // indirect
	github.com/confighub/sdk/configkit/yqkit v0.0.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dimchansky/utfbom v1.1.1 // indirect
	github.com/elliotchance/orderedmap v1.8.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/getsentry/sentry-go v0.27.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/hashicorp/hcl/v2 v2.24.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/itchyny/gojq v0.12.17 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mikefarah/yq/v4 v4.53.2 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/oapi-codegen/runtime v1.1.1 // indirect
	github.com/pelletier/go-toml/v2 v2.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sergi/go-diff v1.3.2-0.20230802210424-5b0b94c5c0d3 // indirect
	github.com/sethvargo/go-envconfig v1.3.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/swaggest/jsonschema-go v0.3.78 // indirect
	github.com/swaggest/refl v1.4.0 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	github.com/yuin/gopher-lua v1.1.2 // indirect
	github.com/zclconf/go-cty v1.18.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.4 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	gopkg.in/op/go-logging.v1 v1.0.0-20160211212156-b2cb9fa56473 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250318190949-c8a335a9a2ff // indirect
	sigs.k8s.io/kustomize/kyaml v0.19.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace (
	github.com/confighub/sdk/cliutil => ../cliutil
	github.com/confighub/sdk/configkit/appyamlkit => ../configkit/appyamlkit
	github.com/confighub/sdk/configkit/envkit => ../configkit/envkit
	github.com/confighub/sdk/configkit/inikit => ../configkit/inikit
	github.com/confighub/sdk/configkit/jsonkit => ../configkit/jsonkit
	github.com/confighub/sdk/configkit/k8skit => ../configkit/k8skit
	github.com/confighub/sdk/configkit/propkit => ../configkit/propkit
	github.com/confighub/sdk/configkit/textkit => ../configkit/textkit
	github.com/confighub/sdk/configkit/tomlkit => ../configkit/tomlkit
	github.com/confighub/sdk/configkit/yqkit => ../configkit/yqkit
	github.com/confighub/sdk/core => ../core
	github.com/confighub/sdk/cubutil => ../cubutil
	github.com/confighub/sdk/function-impl => ../function-impl

	// Fix CVE-2022-28948
	gopkg.in/yaml.v3 => gopkg.in/yaml.v3 v3.0.1

	// Pin kustomize to v5.5.0
	sigs.k8s.io/kustomize/api => sigs.k8s.io/kustomize/api v0.18.0
	sigs.k8s.io/kustomize/kyaml => sigs.k8s.io/kustomize/kyaml v0.18.1
)
