set export # Just variables are exported to environment variables

[private]
default:
	just --list

[private]
stop-local-registry: 
	docker stop registry || true
	docker rm registry || true

[private]
start-local-registry: (stop-local-registry)
	docker run -d -p 5000:5000 --name registry registry:2.7

[private]
push-to-local-registry component version:
	#!/usr/bin/bash
	set -euox pipefail

	rock_file=$(ls "${component}/${version}" | grep "\.rock")
	version="$(yq '.version' ${component}/${version}/rockcraft.yaml)"

	rockcraft.skopeo --insecure-policy copy --dest-tls-verify=false \
		"oci-archive:${component}/${version}/${rock_file}" \
		"docker://localhost:5000/${component}-dev:${version}"

[private]
format_directory directory:
	#!/usr/bin/bash
	directory="${directory#./}"
	directory="${directory%/}"

	echo $directory

[private]
get_rock_component rock_directory:
	#!/usr/bin/bash
	formatted_rock_directory="$(just format_directory $rock_directory)"

	echo "${formatted_rock_directory%%/*}"

[private]
get_rock_version rock_directory:
	#!/usr/bin/bash
	formatted_rock_directory="$(just format_directory $rock_directory)"

	echo "${formatted_rock_directory##*/}"

[private]
component_just_wrapper just_command rock_directory debug="":
	#!/usr/bin/bash
	set -euxo pipefail

	rock_component="$(just get_rock_component $rock_directory)"
	rock_version="$(just get_rock_version $rock_directory)"

	just --justfile="${rock_component}/justfile" $just_command $rock_version $debug

# Pack the rock hosted in the provided directory
pack rock_directory debug="":
	#!/usr/bin/bash
	debug_options=$(if [ -n "${debug}" ]; then echo "--debug"; fi)
	cd ${rock_directory} && rockcraft pack ${debug_options}

# Clean up the rock build environment hosted in the provided directory
clean rock_directory:
	just component_just_wrapper clean $rock_directory

# Pack the rock in the provided directory, and provide a shell in a pod launched with the rock
run rock_directory:
	just component_just_wrapper run $rock_directory

# Pack the rock in the provided directory, and run the component's goss tests on the rock in a K8s pod
test rock_directory:
	just component_just_wrapper test $rock_directory
