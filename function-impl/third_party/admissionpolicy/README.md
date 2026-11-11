# admissionpolicy

From [k8s.io/apiserver](https://github.com/kubernetes/apiserver) v0.35.1, Apache 2.0, in
LICENSE beside this file. It is what `vet-validating-admission-policy` evaluates a
ValidatingAdmissionPolicy with, so that a policy decides a resource the way the API server
would.

- `validator.go`, `interface.go`, `message.go` and `policy_decision.go` are copied unchanged from
  `pkg/admission/plugin/policy/validating`, but for the package name.
- `matching.go` is adapted from `pkg/admission/plugin/policy/matching/matching.go` and
  `pkg/admission/plugin/policy/generic/policy_matcher.go`: a namespace selector is matched
  against labels a caller supplies rather than against a namespace lister and an API client,
  and a nil selector matches everything, as the API server's defaulting makes it.

The `validating` package itself is not imported because it brings in the webhook admission
plugin, and through it gRPC and the egress selector, which nothing else here needs. The
packages these files use, such as `pkg/admission/plugin/cel` and
`pkg/admission/plugin/webhook/matchconditions`, are imported as they are.

When `k8s.io/apiserver` is upgraded, compare these files with the new version's.
