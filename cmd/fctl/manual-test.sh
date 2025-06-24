#!/bin/bash -x

# To regenerate golden outputs, do export DIR=golden-output before running
if [ -z "$DIR" ] ; then
    DIR=out
fi
mkdir -p ${DIR}
rm -f ${DIR}/*

if [ -z "$FCTL" ] ; then
    FCTL=../../function/bin/fctl
fi

if [ -z "$FSRV" ] ; then
    FSRV=../../function/bin/functionsrv
fi

${FSRV} &
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM SIGHUP EXIT

while ! ${FCTL} ok ; do
    sleep 1
done

${FCTL} list > ${DIR}/list.txt
${FCTL} do test-data/deployment-sample.yaml "MyDeployment" get-placeholders > ${DIR}/get-placeholders.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-resources > ${DIR}/get-resources.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-namespace myns > ${DIR}/set-namespace.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-namespace > ${DIR}/get-namespace.txt
${FCTL} do test-data/rolebinding.yaml "MyRB" set-namespace myns > ${DIR}/set-namespace2.txt
${FCTL} do test-data/rolebinding.yaml "MyRB" get-namespace > ${DIR}/get-namespace2.txt
${FCTL} do test-data/rolebinding.yaml "MyRB" get-needed-namespaces > ${DIR}/get-needed-namespaces.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-annotation confighub.com/key changed > ${DIR}/set-annotation.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-label app nginx > ${DIR}/set-label.txt
#The env var key/value pairs end up in a random order due to map ordering
#${FCTL} do test-data/deployment-with-env.yaml "MyDeployment" set-env nginx SUCCESS=true HOPE=true > ${DIR}/set-env.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-container-name > ${DIR}/get-container-name.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-image nginx "mynginx:stable" > ${DIR}/set-image.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" get-image nginx > ${DIR}/get-image.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-image-uri nginx example.myreg.com/nginx > ${DIR}/set-image-uri.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-image-reference nginx ":v17.5.2" > ${DIR}/set-image-reference.txt
${FCTL} do test-data/confighub.yaml "confighub" set-image-reference-by-uri ghcr.io/confighubai/confighub ":testbuild" > ${DIR}/set-image-reference-by-uri.txt
${FCTL} do test-data/deployment-with-env.yaml "MyDeployment" set-env-var nginx SUCCESS true > ${DIR}/set-env-var.txt
${FCTL} do test-data/deployment-with-env.yaml "MyDeployment" get-env-var nginx HOPE > ${DIR}/get-env-var.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-container-resources nginx all 500m 256Mi 2 > ${DIR}/set-container-resources.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" --data-only set-pod-defaults > ${DIR}/set-pod-defaults.yaml
${FCTL} do ${DIR}/set-pod-defaults.yaml MyApp validate > ${DIR}/validate-set-pod-defaults.txt
${FCTL} do test-data/deployment-sample.yaml "MyDeployment" set-default-names > ${DIR}/set-default-names.txt
${FCTL} do test-data/service.yaml "MyApp" get-attributes > ${DIR}/get-attributes.txt
${FCTL} do test-data/deployment.yaml "MyApp" get-attributes > ${DIR}/get-attributes2.txt
${FCTL} do test-data/deployment-sample.yaml "MyApp" get-needed > ${DIR}/get-needed.txt
${FCTL} do test-data/hpa.yaml "MyObj" get-needed > ${DIR}/get-needed2.txt
${FCTL} do test-data/kubernetes-headlamp.yaml "Headlamp" get-needed > ${DIR}/get-needed3.txt
${FCTL} do test-data/namespace.yaml "MyNS" get-provided > ${DIR}/get-provided.txt
${FCTL} do test-data/deployment.yaml MyApp cel-validate 'r.kind != "Deployment" || r.spec.replicas > 1' > ${DIR}/cel-validate.txt
${FCTL} do test-data/deployment.yaml MyApp cel-validate 'r.kind != "Deployment" || r.spec.replicas > 5' > ${DIR}/cel-validate2.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.paused = false" > ${DIR}/where-filter1.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.paused = true" > ${DIR}/where-filter2.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.replicas > 2" > ${DIR}/where-filter3.txt
${FCTL} do test-data/deployment.yaml MyApp where-filter "apps/v1/Deployment" "spec.replicas < 3" > ${DIR}/where-filter4.txt
${FCTL} do test-data/deployment.yaml MyApp validate > ${DIR}/validate.txt
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
${FCTL} doseq test-data/deployment.yaml "MyDeployment" "$(<test-data/valfuncs.json)" > ${DIR}/doseqval.txt
${FCTL} doseq test-data/deployment.yaml "MyDeployment" "$(<test-data/getfuncs.json)" > ${DIR}/doseqget.txt
${FCTL} doseq test-data/deployment.yaml "MyDeployment" "$(<test-data/mutatefuncs.json)" > ${DIR}/doseqmutate.txt
${FCTL} doseq --num-filters 1 --stop test-data/deployment.yaml "MyDeployment" "$(<test-data/filter.json)" > ${DIR}/doseqfilter3.txt
${FCTL} doseq --num-filters 1 --stop test-data/deployment10.yaml "MyDeployment" "$(<test-data/filter.json)" > ${DIR}/doseqfilter10.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" yq '.spec.replicas' > ${DIR}/yq-relicas.txt
${FCTL} do test-data/service.yaml "MyService" ensure-namespaces > ${DIR}/ensure-namespaces-insert.txt
${FCTL} do test-data/all-in-one.yaml "MyUnit" ensure-namespaces > ${DIR}/ensure-namespaces-skipclusterscoped.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" ensure-context true > ${DIR}/ensure-context-true.txt
${FCTL} do test-data/all-in-one.yaml "MyUnit" get-details > ${DIR}/get-details-all.txt
${FCTL} do test-data/deployment10.yaml "MyDeployment" compute-mutations "$(<test-data/deployment.yaml)" 0 > ${DIR}/compute-mutations.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" patch-mutations "$(<test-data/original-mutations.json)" "$(<test-data/patch-mutations.json)" > ${DIR}/patch-mutations.txt
${FCTL} do test-data/all-in-one-resolved.yaml "MyDeployment" reset "$(<test-data/reset-preds.json)" > ${DIR}/reset.txt
${FCTL} do test-data/cubby-frontend.yaml "Frontend" set-hostname prod.chat.cubby.bz > ${DIR}/set-hostname.txt
${FCTL} do test-data/cubby-frontend.yaml "Frontend" set-hostname-subdomain chat > ${DIR}/set-subdomain.txt
${FCTL} do test-data/cubby-frontend.yaml "Frontend" set-hostname-domain cubby.bz > ${DIR}/set-domain.txt
${FCTL} do test-data/deployment.yaml "MyDeployment" set-path-comment apps/v1/Deployment spec.replicas "TODO: autoscale" > ${DIR}/set-path-comment.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" set-bool-path SimpleApp "database.ssl.enabled" false > ${DIR}/set-bool-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" set-int-path SimpleApp "database.port" 5433 > ${DIR}/set-int-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" set-string-path SimpleApp "database.host" postgres.local.cubby.bz > ${DIR}/set-string-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" get-bool-path SimpleApp "database.ssl.enabled" > ${DIR}/get-bool-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" get-int-path SimpleApp "database.port" > ${DIR}/get-int-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app.properties "MyConfig" get-string-path SimpleApp "database.host"  > ${DIR}/get-string-path-properties.txt
${FCTL} do --toolchain "AppConfig/Properties" test-data/app2.properties "MyConfig" compute-mutations "$(<test-data/app.properties)" 0 > ${DIR}/compute-mutations-properties.txt

# Test upsert-resource function
# First get the resources from service.yaml to get the ResourceList
SERVICE_RESOURCES=$(${FCTL} do --output-only test-data/service.yaml "MyService" get-resources)
# Now upsert the service into deployment.yaml
${FCTL} do test-data/deployment.yaml "MyDeployment" upsert-resource "${SERVICE_RESOURCES}" "v1/Service" "/my-service" > ${DIR}/upsert-resource.txt

# Test delete-resource function
# Delete the ConfigMap resource from all-in-one-resolved.yaml
${FCTL} do test-data/all-in-one-resolved.yaml "MyDeployment" delete-resource "v1/ConfigMap" "foobar/myconfig" > ${DIR}/delete-resource.txt

# These maps are unordered, so this may be problematic, but...
 ${FCTL} listpaths  > ${DIR}/listpaths.txt

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
