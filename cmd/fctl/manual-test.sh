#!/bin/bash -x

# To regenerate golden outputs, do export DIR=golden-output before running
if [ -z "$DIR" ] ; then
    DIR=out
fi
mkdir -p ${DIR}
rm -f ${DIR}/*

if [ -z "$FCTL" ] ; then
    FCTL=../bin/fctl
fi

if [ -z "$FSRV" ] ; then
    FSRV=../bin/functionsrv
fi

${FSRV} &
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM SIGHUP EXIT

while ! ${FCTL} ok ; do
    sleep 1
done

${FCTL} list > ${DIR}/list.txt
${FCTL} do test-data/deployment-sample.yaml "MyDeployment" get-placeholders > ${DIR}/get-placeholders.txt
${FCTL} do test-data/deployment-sample.yaml "MyDeployment" vet-placeholders > ${DIR}/vet-placeholders.txt
${FCTL} do test-data/deployment-sample.yaml "MyDeployment" search-replace confighubplaceholder replaced > ${DIR}/search-replace.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-resources > ${DIR}/get-resources.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-resources none > ${DIR}/get-resources-none.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-resources json > ${DIR}/get-resources-json.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-namespace myns > ${DIR}/set-namespace.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-namespace > ${DIR}/get-namespace.txt
${FCTL} do test-data/rolebinding.yaml "MyRB" set-namespace myns > ${DIR}/set-namespace2.txt
${FCTL} do test-data/rolebinding.yaml "MyRB" get-namespace > ${DIR}/get-namespace2.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-annotation confighub.com/key changed > ${DIR}/set-annotation.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-annotation confighub.com/upsert-test new-value > ${DIR}/set-annotation-upsert.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-label app nginx > ${DIR}/set-label.txt
#The env var key/value pairs end up in a random order due to map ordering
#${FCTL} do test-data/deployment-with-env.yaml "MyDeployment" set-env nginx SUCCESS=true HOPE=true > ${DIR}/set-env.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-container-name > ${DIR}/get-container-name.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-image nginx "mynginx:stable" > ${DIR}/set-image.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-image nginx > ${DIR}/get-image.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-image "*" > ${DIR}/get-image-wildcard.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" vet-images '{"AllowStrings":{"nginx:latest":true,"otel/opentelemetry-collector:latest-amd64":true}}' > ${DIR}/vet-images-pass.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" vet-images '{"DenyStrings":{"nginx:latest":true}}' > ${DIR}/vet-images-fail.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-image-uri nginx example.myreg.com/nginx > ${DIR}/set-image-uri.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-image-reference nginx ":v17.5.2" > ${DIR}/set-image-reference.txt
${FCTL} do test-data/confighub.yaml "app" set-image-reference-by-uri ghcr.io/example/example ":testbuild" > ${DIR}/set-image-reference-by-uri.txt
${FCTL} do test-data/deployment-with-env.yaml "MyDeployment" set-env-var nginx SUCCESS true > ${DIR}/set-env-var.txt
${FCTL} do test-data/deployment-with-env.yaml "MyDeployment" get-env-var nginx HOPE > ${DIR}/get-env-var.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-container-resources nginx all 500m 256Mi 2 > ${DIR}/set-container-resources.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-container-volume-mount-path nginx config-volume /etc/config configMap > ${DIR}/set-container-volume-mount-path.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-container-port nginx http 8080 TCP > ${DIR}/set-container-port.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-pod-defaults true true true true true > ${DIR}/set-pod-defaults.yaml
${FCTL} do ${DIR}/set-pod-defaults.yaml MyApp vet-schemas > ${DIR}/vet-schemas-set-pod-defaults.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only -- set-pod-defaults --pod-security=true --automount-service-account-token=true --security-context=true --resources=true --probes=false > ${DIR}/set-pod-defaults-no-probes.yaml

# Test new defaulting functions (split from set-pod-defaults)
${FCTL} do test-data/namespace.yaml "MyNS" --data-only set-pod-security-defaults > ${DIR}/set-pod-security-defaults.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-automount-service-account-token-false > ${DIR}/set-automount-service-account-token-false.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-pod-container-security-context-defaults > ${DIR}/set-pod-container-security-context-defaults.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-container-resources-defaults > ${DIR}/set-container-resources-defaults.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-container-probe-defaults > ${DIR}/set-container-probe-defaults.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-automount-service-account-token-false > ${DIR}/set-automount-service-account-token-false.yaml

# Container attributes, whose paths are declared on the Containers shape. Each writes through
# the path it is registered at, so a wrong element spelling shows up here as a missed write.
${FCTL} do test-data/deployment.yaml "MyDeployment" get-container-image nginx > ${DIR}/get-container-image.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-container-image nginx nginx:1.27.0 > ${DIR}/set-container-image.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" get-container-repository-uri nginx > ${DIR}/get-container-repository-uri.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-container-image-reference nginx > ${DIR}/get-container-image-reference.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-workload-labels > ${DIR}/get-workload-labels.txt

# References. get-references is what cub variant upload and the installer match on to create
# links; the -of-type pair selects the same paths by their ResourceType property.
${FCTL} do test-data/all-in-one.yaml "MyUnit" get-references > ${DIR}/get-references.txt
${FCTL} do test-data/all-in-one.yaml "MyUnit" get-references-of-type v1/ConfigMap > ${DIR}/get-references-of-type-configmap.txt
${FCTL} do test-data/all-in-one.yaml "MyUnit" --data-only set-references-of-type v1/ConfigMap renamed-cm > ${DIR}/set-references-of-type-configmap.yaml
${FCTL} do test-data/deployment-sample.yaml "MyDeployment" set-default-names "template:{{.UnitSlug | normalizeName}}-{{.SpaceSlug | normalizeName}}" > ${DIR}/set-default-names.txt
${FCTL} do test-data/deployment-sample.yaml "MyApp" get-needed > ${DIR}/get-needed.txt
# An HPA's scaleTargetRef requires one of several workload controllers, so its needed path
# carries them as alternatives. This was disabled for producing unordered output; reference
# registration is sorted now.
${FCTL} do test-data/hpa.yaml "MyObj" get-needed > ${DIR}/get-needed2.txt
${FCTL} do test-data/kubernetes-headlamp.yaml "Headlamp" get-needed > ${DIR}/get-needed3.txt
${FCTL} do test-data/namespace.yaml "MyNS" get-provided > ${DIR}/get-provided.txt
${FCTL} do test-data/deployment.yaml MyApp vet-celexpr 'r.kind != "Deployment" || r.spec.replicas > 1' > ${DIR}/vet-celexpr.txt
${FCTL} do test-data/deployment.yaml MyApp vet-celexpr 'r.kind != "Deployment" || r.spec.replicas > 5' > ${DIR}/vet-celexpr2.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.paused = false" > ${DIR}/where-filter1.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.paused = true" > ${DIR}/where-filter2.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.replicas > 2" > ${DIR}/where-filter3.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.replicas < 3" > ${DIR}/where-filter4.txt

# Test the .|syntax for where-filter (split path feature)
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.template.spec.containers.*.|securityContext.runAsNonRoot != true" > ${DIR}/where-filter-split-path.txt
${FCTL} do test-data/deployment.yaml MyApp vet-schemas > ${DIR}/vet-schemas.txt

# Test vet-format linter
${FCTL} do test-data/deployment.yaml MyApp vet-format > ${DIR}/vet-format-clean.txt
${FCTL} do test-data/deployment-lint-problems.yaml MyApp vet-format > ${DIR}/vet-format-problems.txt
${FCTL} do test-data/deployment-lint-anchors.yaml MyApp vet-format > ${DIR}/vet-format-anchors.txt

${FCTL} do test-data/deployment.yaml "MyDeployment" set-replicas 5 > ${DIR}/set-replicas.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-replicas > ${DIR}/get-replicas.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-string-path apps/v1/Deployment spec.template.spec.dnsPolicy > ${DIR}/get-string-path.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-string-path apps/v1/Deployment spec.template.spec.dnsPolicy None > ${DIR}/set-string-path.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-string-path apps/v1/Deployment "spec.template.spec.containers.0.image#uri" > ${DIR}/get-string-path2.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-string-path apps/v1/Deployment "spec.template.spec.containers.?name=nginx.image#uri" > ${DIR}/get-string-path3.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-string-path apps/v1/Deployment "spec.template.spec.containers.0.image#uri" example.myreg.com/nginx > ${DIR}/set-string-path2.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-int-path apps/v1/Deployment spec.replicas > ${DIR}/get-int-path.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-int-path apps/v1/Deployment spec.replicas 5 > ${DIR}/set-int-path.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-bool-path apps/v1/Deployment spec.paused > ${DIR}/get-bool-path.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-bool-path apps/v1/Deployment spec.paused true > ${DIR}/set-bool-path.txt
${FCTL} do test-data/deployment-sample.yaml "MyDeployment" set-attributes "$(<test-data/imageuri.json)" > ${DIR}/set-attributes.txt
# get-paths answers by merge key, whichever way it was asked, so that its output is a
# key the requester can match and a path set-attributes can write through an insertion.
${FCTL} do test-data/deployment-initcontainers.yaml "MyDeployment" get-paths "$(<test-data/initcontainer-paths.json)" > ${DIR}/get-paths.txt
${FCTL} do test-data/deployment-initcontainers.yaml "MyDeployment" set-attributes "$(<test-data/initcontainer-setattrs.json)" > ${DIR}/set-attributes-mergekey.txt
${FCTL} doseq test-data/deployment.yaml "MyDeployment" "$(<test-data/valfuncs.json)" > ${DIR}/doseqval.txt
${FCTL} doseq test-data/deployment.yaml "MyDeployment" "$(<test-data/getfuncs.json)" > ${DIR}/doseqget.txt
${FCTL} doseq test-data/deployment.yaml "MyDeployment" "$(<test-data/mutatefuncs.json)" > ${DIR}/doseqmutate.txt
${FCTL} doseq --num-filters 1 --stop test-data/deployment.yaml "MyDeployment" "$(<test-data/filter.json)" > ${DIR}/doseqfilter3.txt
${FCTL} doseq --num-filters 1 --stop test-data/deployment10.yaml "MyDeployment" "$(<test-data/filter.json)" > ${DIR}/doseqfilter10.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-yq '.spec.replicas' > ${DIR}/yq-relicas.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-yq '.spec.replicas = 7' > ${DIR}/yq-i-relicas.txt
${FCTL} do test-data/service.yaml "MyService" ensure-namespaces > ${DIR}/ensure-namespaces-insert.txt
${FCTL} do test-data/all-in-one.yaml "MyUnit" ensure-namespaces > ${DIR}/ensure-namespaces-skipclusterscoped.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" ensure-context true > ${DIR}/ensure-context-true.txt
${FCTL} do test-data/deployment10.yaml "MyDeployment" compute-mutations "$(<test-data/deployment.yaml)" 0 > ${DIR}/compute-mutations.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" patch-mutations "$(<test-data/original-mutations.json)" "$(<test-data/patch-mutations.json)" > ${DIR}/patch-mutations.txt
${FCTL} do test-data/all-in-one-resolved.yaml "MyDeployment" reset "$(<test-data/reset-preds.json)" > ${DIR}/reset.txt
${FCTL} do test-data/cubby-frontend.yaml "Frontend" set-hostname prod.chat.cubby.bz > ${DIR}/set-hostname.txt
${FCTL} do test-data/cubby-frontend.yaml "Frontend" set-hostname-subdomain chat > ${DIR}/set-subdomain.txt
${FCTL} do test-data/cubby-frontend.yaml "Frontend" set-hostname-domain cubby.bz > ${DIR}/set-domain.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-path-comment apps/v1/Deployment spec.replicas "TODO: autoscale" > ${DIR}/set-path-comment.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" delete-path apps/v1/Deployment "spec.template.spec.containers.?name=otel-sidecar" > ${DIR}/delete-path.txt
${FCTL} do test-data/all-in-one.yaml MyApp select-where-resource "*" "ConfigHub.ResourceType IN ('v1/Service','v1/ServiceAccount')" > ${DIR}/select-where-resource.txt
${FCTL} do --where-resource "ConfigHub.ResourceType = 'apps/v1/Deployment'" test-data/all-in-one.yaml "MyUnit" get-resources > ${DIR}/get-resources-where-resource.txt

${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" set-bool-path SimpleApp "database.ssl.enabled" false > ${DIR}/set-bool-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" set-int-path SimpleApp "database.port" 5433 > ${DIR}/set-int-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" set-string-path SimpleApp "database.host" postgres.local.cubby.bz > ${DIR}/set-string-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" get-bool-path SimpleApp "database.ssl.enabled" > ${DIR}/get-bool-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" get-int-path SimpleApp "database.port" > ${DIR}/get-int-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" get-string-path SimpleApp "database.host"  > ${DIR}/get-string-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app2.properties "MyConfig" compute-mutations "$(<test-data/app.properties)" 0 > ${DIR}/compute-mutations-properties.txt

${FCTL} do --toolchain "AppConfig/TOML" test-data/app.toml "MyConfig" set-bool-path SimpleApp "database.ssl.enabled" false > ${DIR}/set-bool-path-toml.txt
${FCTL} do --toolchain "AppConfig/TOML" test-data/app.toml "MyConfig" set-int-path SimpleApp "database.port" 5433 > ${DIR}/set-int-path-toml.txt
${FCTL} do --toolchain "AppConfig/TOML" test-data/app.toml "MyConfig" set-string-path SimpleApp "database.host" postgres.local.cubby.bz > ${DIR}/set-string-path-toml.txt
${FCTL} do --toolchain "AppConfig/TOML" test-data/app.toml "MyConfig" get-bool-path SimpleApp "database.ssl.enabled" > ${DIR}/get-bool-path-toml.txt
${FCTL} do --toolchain "AppConfig/TOML" test-data/app.toml "MyConfig" get-int-path SimpleApp "database.port" > ${DIR}/get-int-path-toml.txt
${FCTL} do --toolchain "AppConfig/TOML" test-data/app.toml "MyConfig" get-string-path SimpleApp "database.host"  > ${DIR}/get-string-path-toml.txt
#${FCTL} do --toolchain "AppConfig/TOML" test-data/app2.toml "MyConfig" compute-mutations "$(<test-data/app.toml)" 0 > ${DIR}/compute-mutations-toml.txt

${FCTL} do --toolchain "AppConfig/INI" test-data/app.ini "MyConfig" set-bool-path SimpleApp "database.ssl.enabled" false > ${DIR}/set-bool-path-ini.txt
${FCTL} do --toolchain "AppConfig/INI" test-data/app.ini "MyConfig" set-int-path SimpleApp "database.port" 5433 > ${DIR}/set-int-path-ini.txt
${FCTL} do --toolchain "AppConfig/INI" test-data/app.ini "MyConfig" set-string-path SimpleApp "database.host" postgres.local.cubby.bz > ${DIR}/set-string-path-ini.txt
${FCTL} do --toolchain "AppConfig/INI" test-data/app.ini "MyConfig" get-bool-path SimpleApp "database.ssl.enabled" > ${DIR}/get-bool-path-ini.txt
${FCTL} do --toolchain "AppConfig/INI" test-data/app.ini "MyConfig" get-int-path SimpleApp "database.port" > ${DIR}/get-int-path-ini.txt
${FCTL} do --toolchain "AppConfig/INI" test-data/app.ini "MyConfig" get-string-path SimpleApp "database.host"  > ${DIR}/get-string-path-ini.txt
#${FCTL} do --toolchain "AppConfig/INI" test-data/app2.ini "MyConfig" compute-mutations "$(<test-data/app.ini)" 0 > ${DIR}/compute-mutations-ini.txt

${FCTL} do --toolchain "AppConfig/Env" test-data/app.env "MyConfig" set-string-path SimpleApp "DATABASE_SSL_ENABLED" false > ${DIR}/set-string-path-bool-env.txt
${FCTL} do --toolchain "AppConfig/Env" test-data/app.env "MyConfig" set-string-path SimpleApp "DATABASE_PORT" 5433 > ${DIR}/set-string-path-int-env.txt
${FCTL} do --toolchain "AppConfig/Env" test-data/app.env "MyConfig" set-string-path SimpleApp "DATABASE_HOST" postgres.local.cubby.bz > ${DIR}/set-string-path-env.txt
${FCTL} do --toolchain "AppConfig/Env" test-data/app.env "MyConfig" get-string-path SimpleApp "DATABASE_SSL_ENABLED" > ${DIR}/get-string-path-bool-env.txt
${FCTL} do --toolchain "AppConfig/Env" test-data/app.env "MyConfig" get-string-path SimpleApp "DATABASE_PORT" > ${DIR}/get-string-path-int-env.txt
${FCTL} do --toolchain "AppConfig/Env" test-data/app.env "MyConfig" get-string-path SimpleApp "DATABASE_HOST"  > ${DIR}/get-string-path-env.txt
#${FCTL} do --toolchain "AppConfig/Env" test-data/app2.env "MyConfig" compute-mutations "$(<test-data/app.env)" 0 > ${DIR}/compute-mutations-env.txt

${FCTL} do --toolchain "AppConfig/JSON" test-data/app.json "MyConfig" set-bool-path SimpleApp "database.ssl.enabled" false > ${DIR}/set-bool-path-json.txt
${FCTL} do --toolchain "AppConfig/JSON" test-data/app.json "MyConfig" set-int-path SimpleApp "database.port" 5433 > ${DIR}/set-int-path-json.txt
${FCTL} do --toolchain "AppConfig/JSON" test-data/app.json "MyConfig" set-string-path SimpleApp "database.host" postgres.local.cubby.bz > ${DIR}/set-string-path-json.txt
${FCTL} do --toolchain "AppConfig/JSON" test-data/app.json "MyConfig" get-bool-path SimpleApp "database.ssl.enabled" > ${DIR}/get-bool-path-json.txt
${FCTL} do --toolchain "AppConfig/JSON" test-data/app.json "MyConfig" get-int-path SimpleApp "database.port" > ${DIR}/get-int-path-json.txt
${FCTL} do --toolchain "AppConfig/JSON" test-data/app.json "MyConfig" get-string-path SimpleApp "database.host"  > ${DIR}/get-string-path-json.txt
#${FCTL} do --toolchain "AppConfig/JSON" test-data/app2.json "MyConfig" compute-mutations "$(<test-data/app.json)" 0 > ${DIR}/compute-mutations-json.txt

${FCTL} do --toolchain "AppConfig/YAML" test-data/app.yaml "MyConfig" set-bool-path SimpleApp "database.ssl.enabled" false > ${DIR}/set-bool-path-yaml.txt
${FCTL} do --toolchain "AppConfig/YAML" test-data/app.yaml "MyConfig" set-int-path SimpleApp "database.port" 5433 > ${DIR}/set-int-path-yaml.txt
${FCTL} do --toolchain "AppConfig/YAML" test-data/app.yaml "MyConfig" set-string-path SimpleApp "database.host" postgres.local.cubby.bz > ${DIR}/set-string-path-yaml.txt
${FCTL} do --toolchain "AppConfig/YAML" test-data/app.yaml "MyConfig" get-bool-path SimpleApp "database.ssl.enabled" > ${DIR}/get-bool-path-yaml.txt
${FCTL} do --toolchain "AppConfig/YAML" test-data/app.yaml "MyConfig" get-int-path SimpleApp "database.port" > ${DIR}/get-int-path-yaml.txt
${FCTL} do --toolchain "AppConfig/YAML" test-data/app.yaml "MyConfig" get-string-path SimpleApp "database.host"  > ${DIR}/get-string-path-yaml.txt
#${FCTL} do --toolchain "AppConfig/YAML" test-data/app2.yaml "MyConfig" compute-mutations "$(<test-data/app.yaml)" 0 > ${DIR}/compute-mutations-yaml.txt

# TODO: Do something appropriate for Text
#${FCTL} do --toolchain "AppConfig/Text" test-data/app.text "MyConfig" set-bool-path SimpleApp "database.ssl.enabled" false > ${DIR}/set-bool-path-text.txt
#${FCTL} do --toolchain "AppConfig/Text" test-data/app.text "MyConfig" set-int-path SimpleApp "database.port" 5433 > ${DIR}/set-int-path-text.txt
#${FCTL} do --toolchain "AppConfig/Text" test-data/app.text "MyConfig" set-string-path SimpleApp "database.host" postgres.local.cubby.bz > ${DIR}/set-string-path-text.txt
#${FCTL} do --toolchain "AppConfig/Text" test-data/app.text "MyConfig" get-bool-path SimpleApp "database.ssl.enabled" > ${DIR}/get-bool-path-text.txt
#${FCTL} do --toolchain "AppConfig/Text" test-data/app.text "MyConfig" get-int-path SimpleApp "database.port" > ${DIR}/get-int-path-text.txt
#${FCTL} do --toolchain "AppConfig/Text" test-data/app.text "MyConfig" get-string-path SimpleApp "database.host"  > ${DIR}/get-string-path-text.txt
#${FCTL} do --toolchain "AppConfig/Text" test-data/app2.text "MyConfig" compute-mutations "$(<test-data/app.text)" 0 > ${DIR}/compute-mutations-text.txt

# Test vet-jsonschema with Grafana LDAP configuration
${FCTL} do --toolchain "AppConfig/TOML" test-data/grafana.toml "GrafanaLDAP" vet-jsonschema "$(<test-data/grafana-ldap-schema.json)" > ${DIR}/vet-jsonschema-grafana.txt
${FCTL} do --toolchain "AppConfig/TOML" test-data/grafana-invalid.toml "GrafanaLDAPInvalid" vet-jsonschema "$(<test-data/grafana-ldap-schema.json)" > ${DIR}/vet-jsonschema-grafana-invalid.txt

# Test upsert-resource function
# First get the resources from service.yaml to get the ResourceList
SERVICE_RESOURCES=$(${FCTL} do --output-only test-data/service.yaml "MyService" get-resources)
# Now upsert the service into deployment.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" upsert-resource "${SERVICE_RESOURCES}" "v1/Service" "/my-service" > ${DIR}/upsert-resource.txt

# Test delete-resource function
# Delete the ConfigMap resource from all-in-one-resolved.yaml
${FCTL} do test-data/all-in-one-resolved.yaml "MyDeployment" delete-resource "v1/ConfigMap" "foobar/myconfig" > ${DIR}/delete-resource.txt

# Test upsert functionality with different path expressions
${FCTL} do test-data/deployment.yaml "MyDeployment" set-bool-path "apps/v1/Deployment" "spec.template.spec.containers.*.securityContext.runAsNonRoot" true > ${DIR}/set-bool-path-upsert-wildcard.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-bool-path "apps/v1/Deployment" "spec.template.spec.containers.?name=nginx.securityContext.runAsNonRoot" true > ${DIR}/set-bool-path-upsert-assoc.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-bool-path "apps/v1/Deployment" "spec.template.spec.containers.0.securityContext.|runAsNonRoot" true > ${DIR}/set-bool-path-upsert-existence.txt

# Test starlark
${FCTL} do test-data/ingress-route.yaml "MyDeployment" set-starlark 'for route in r["spec"]["routes"]:                         
      route["match"] = re.sub(params["pattern"], "Host(`" + params["hostname"] + "`)", route["match"])' hostname=test1.testwebsite.prod.confighub.net 'pattern=Host\(`[^`]*`\)( *\|\| *Host\(`[^`]*`\))*' > ${DIR}/starlark-regexp.txt

# Test vet-immutable
${FCTL} do test-data/deployment.yaml MyApp vet-immutable > ${DIR}/vet-immutable-no-live.txt
${FCTL} do test-data/deployment.yaml MyApp vet-immutable --other-data LastReleasedRevisionNum=test-data/deployment.yaml > ${DIR}/vet-immutable-same.txt
${FCTL} do test-data/deployment-selector-changed.yaml MyApp vet-immutable --other-data LastReleasedRevisionNum=test-data/deployment.yaml > ${DIR}/vet-immutable-changed.txt

# These maps are unordered, so this is problematic
# ${FCTL} listpaths  > ${DIR}/listpaths.txt

 ${FCTL} shutdown

# Check results
# To show the diffs inline in the output, set export QUIET=no before running
status=0
if [ "$QUIET" = "no" ] ; then
    QUIET=""
else
    QUIET="-q"
fi

if [ ${DIR} = out ] ; then
    for output in out/* ; do
        if ! diff $QUIET $output golden-output/${output#out/} ; then
            status=1
        fi
    done
fi

exit $status
