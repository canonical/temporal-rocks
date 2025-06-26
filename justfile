set quiet # Recipes are silent by default
set export # Just variables are exported to environment variables

[private]
default:
	just --list

[private]
start-local-registry: (stop-local-registry)
	docker run -d -p 5000:5000 --name registry registry:2.7

[private]
stop-local-registry: 
	docker stop registry || true
	docker rm registry || true

[private]
push-to-local-registry component:
	#!/usr/bin/bash
	set -euox pipefail

	rock_file=$(ls "${component}" | grep "\.rock")
	rock_version=$(yq ".version" "${component}/rockcraft.yaml")
	rockcraft.skopeo --insecure-policy copy --dest-tls-verify=false \
		"oci-archive:${component}/${rock_file}" \
		"docker://localhost:5000/${component}-dev:${rock_version}"

# Clean the rockcraft project of the component and remove any existing rock files
clean component:
	cd "${component}" && rockcraft clean
	cd "${component}" && rm -f "$(ls | grep *.rock)"
	rm -rf "${component}/temporal"

# Pack a rock for a specific component
pack component debug="":
	#!/usr/bin/bash
	debug_options=$(if [ -n "${debug}" ]; then echo "--debug"; fi)
	cd "${component}" && rockcraft pack ${debug_options}

# Pack rock for component and run pod with rock in kubernetes
run component: (start-local-registry) (pack component) (push-to-local-registry component)
	#!/usr/bin/bash
	set -euxo pipefail

	trap 'just stop-local-registry' EXIT

	rock_version=$(yq ".version" "${component}/rockcraft.yaml")
	extra_options=""
	goss_vars="GOSS_KUBECTL_BIN=\"$(which kubectl)\" GOSS_OPTS=\"--retry-timeout 60s --color\""
	
	if [ "${component}" = "temporal-server" ]; then
	rm -rf "${component}/temporal"
	git clone --branch "v${rock_version}" --single-branch https://github.com/temporalio/temporal ${component}/temporal

	goss_vars+=' GOSS_CONTAINER_PATH="/etc/temporal"'
	extra_options+="-d temporal/config/ -e TEMPORAL_ROOT=/etc/temporal -e TEMPORAL_ENVIRONMENT=development-sqlite"
	cd "${component}"
	fi

	eval "env ${goss_vars} kgoss edit -i localhost:5000/${component}-dev:${rock_version} ${extra_options}"

# Pack rock for component and run goss tests with rock in kubernetes
test component: (start-local-registry) (pack component) (push-to-local-registry component)
	#!/usr/bin/bash
	set -euxo pipefail

	trap 'just stop-local-registry' EXIT

	rock_version=$(yq ".version" "${component}/rockcraft.yaml")
	extra_options=""
	goss_vars="GOSS_KUBECTL_BIN=\"$(which kubectl)\" GOSS_OPTS=\"--retry-timeout 60s\""
	
	if [ "${component}" = "temporal-server" ]; then
	rm -rf "${component}/temporal"
	git clone --branch "v${rock_version}" --single-branch https://github.com/temporalio/temporal ${component}/temporal

	goss_vars+=' GOSS_CONTAINER_PATH="/etc/temporal"'
	extra_options+="-d temporal/config/ -e TEMPORAL_ROOT=/etc/temporal -e TEMPORAL_ENVIRONMENT=development-sqlite"
	cd "${component}"
	fi

	eval "env ${goss_vars} kgoss run -i localhost:5000/${component}-dev:${rock_version} ${extra_options}"
