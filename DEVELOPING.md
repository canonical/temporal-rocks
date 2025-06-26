# Developing Temporal Rocks

## Installing dependencies

The following are dependencies to be able to develop and test changes to the Temporal rocks:

- a K8s distribution (ideally `k8s` by Canonical)
- `rockcraft`
- `yq`
- `kubectl`
- `just`
- `goss`
- `kgoss`

There are convenient snaps for all of the above dependencies besides `goss` and `kgoss`. The recommended way to install these until a `goss` snap is released would be:

```shell
goss_base_url="https://github.com/goss-org/goss/releases/latest/download"
curl -L ${goss_base_url}/goss-linux-amd64 -o /usr/local/bin/goss
chmod +rx /usr/local/bin/goss
curl -L ${goss_base_url}/kgoss -o /usr/local/bin/kgoss
chmod +rx /usr/local/bin/kgoss
```

## Directory structure

Each temporal rock is organized into a separate directory (e.g. `temporal-server/`).

## Building Temporal rocks

Each temporal rock is designed to be built by running `rockcraft pack` from the rock's directory:

```shell
cd temporal-server
rockcraft pack
```

Furthermore, we have `just` commands to be able to pack from the project root. For instance, to build the temporal-server rock, you can run the following from the project root:

```
just pack temporal-server
```

## Running a built Temporal rock

To run the built rock in a K8s distribution, please ensure that `kubectl` has the proper configuration to interact with the K8s distribution.

If you are using microk8s:

```shell
microk8s config > ~/.kube/config
```

If you are using Canonical K8s:

```shell
k8s kubectl config view --raw > ~/.kube/config
```

Then, you can use the `just run <rock_name>` command to launch a pod in K8s with a container based on the rock. Once you exit out of the container's shell, the pod will be deleted and other relevant cleanup will take place.

## Running Temporal rock tests

As described in the `Running a built Temporal rock` section, please ensure that `kubectl` is configured to interact with your K8s distribution.

Then, you can use the `just test <rock_name>` command to launch a pod in K8s with a container based on the rock, and execute tests defined in the `goss.yaml` file within the rock's directory.
