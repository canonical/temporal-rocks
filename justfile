set quiet # Recipes are silent by default
set export # Just variables are exported to environment variables

default_component := "temporal-server"

[private]
default:
	just --list

[private]
push-to-registry component:
	#!/usr/bin/bash
	set -e
	echo "Pushing ${component} to local"
	rock_file=$(ls "${component}" | grep "\.rock")
	rock_version=$(yq ".version" "${component}/rockcraft.yaml")
	rockcraft.skopeo --insecure-policy copy --dest-tls-verify=false \
		"oci-archive:${component}/${rock_file}" \
		"docker://localhost:32000/${component}-dev:${rock_version}"

# Clean the rockcraft project of the component and remove any existing rock files
clean component=default_component:
	cd "${component}" && rockcraft clean
	cd "${component}" && rm -f "$(ls | grep *.rock)"
	rm -rf "${component}/temporal"

# Pack a rock for a specific component
pack component=default_component:
	cd "${component}" && rockcraft pack

# Pack rock for component and run pod with rock in kubernetes
run component=default_component: (pack component) (push-to-registry component)
	#!/usr/bin/bash
	set -e
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

	eval "env ${goss_vars} kgoss edit -i localhost:32000/${component}-dev:${rock_version} ${extra_options}"

# Pack rock for component and run goss tests with rock in kubernetes
test component=default_component: (pack component) (push-to-registry component)
	#!/usr/bin/bash
	set -e
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

	eval "env ${goss_vars} kgoss run -i localhost:32000/${component}-dev:${rock_version} ${extra_options}"
