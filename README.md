# Monolithic repository for Temporal Rocks

[Rocks](https://documentation.ubuntu.com/rockcraft/en/latest/) for [Temporal](https://temporal.io/).

This repository hosts all the necessary files to build Rocks for various Temporal components.

In order to interact with this repository, use [just](https://github.com/casey/just) to run commands.

```shell
$ just
Available recipes:
    clean component=default_component # Clean the rockcraft project of the component and remove any existing rock files
    pack component=default_component  # Pack a rock for a specific component
    run component=default_component   # Pack rock for component and run pod with rock in kubernetes
    test component=default_component  # Pack rock for component and run goss tests with rock in kubernetes
```

The following are necessary to run `just run/test`: `yq`, `kubectl`, `goss` and `kgoss`. We recommend downloading snaps for `yq` and `kubectl`, whereas `goss` and `kgoss` can be downloaded as follows:

```shell
goss_base_url="https://github.com/goss-org/goss/releases/latest/download"
curl -L ${goss_base_url}/goss-linux-amd64 -o /usr/local/bin/goss
chmod +rx /usr/local/bin/goss
curl -L ${goss_base_url}/kgoss -o /usr/local/bin/kgoss
chmod +rx /usr/local/bin/kgoss
```

Furthermore, to run `just run/test`, you will need to:

```shell
# configure kubectl to work with microk8s
microk8s config > ~/.kube/config
# enable microk8s registry add-on
sudo microk8s enable registry
```
