# kustomize

From https://github.com/kubernetes-sigs/kustomize, Apache 2.0, in LICENSE beside this file.

This directory once held a fork of kustomize's `namereference.go` and the types that parse it.
Its `NameReferenceFieldSpecs` table is the list of fields in built-in Kubernetes types that
name another resource: a Deployment's `spec/template/spec/serviceAccountName` names a
ServiceAccount, an Ingress's `spec/tls/secretName` names a Secret.

Those references are now declared in
`public/configkit/k8skit/resource_type_specs.yaml`, as `resource-name` paths carrying the type
each names, alongside everything else ConfigHub declares about a type. The data is derived from
kustomize's table and this is its attribution; the Go that parsed the table at registration is
gone, along with the segment-name list it needed to guess which path segments were arrays.

Corrections made in transcribing it are named in the commit that moved it, and each is a case
the parse could not have got right: an escaped `/` inside an annotation key that the split
turned into a stray backslash, a group field carrying its own version, a table entry naming a
type with no group at all.
