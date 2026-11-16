# kubeconform

`validator/validator.go` is `pkg/validator/validator.go` from
https://github.com/yannh/kubeconform at v0.6.7, Apache 2.0, in LICENSE beside this file. The rest
of kubeconform (`pkg/cache`, `pkg/registry`, `pkg/resource`) is imported as a module.

kubeconform reports only the top-level causes of a resource's schema validation error. Those are
often wrappers: an object with two invalid fields is reported as one error at the object, with an
empty message, and what is wrong with each field is lost. `vet-schemas` needs each error at the
field it is about, so the copy differs in one place, marked "ConfigHub": `leafErrors` walks the
causes to the errors that say what failed, and reports the JSON Schema keyword that failed with
each, as `ValidationError.Keyword`. kubeconform v0.8.0 reports errors the same way as v0.6.7, so
upgrading does not make this copy unnecessary.
