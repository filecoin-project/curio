---
description: How to run a local network with Curio using docker
---

# Docker Devnet

## Prerequisites

To ensure a stable and functional network, the Curio devnet requires running multiple binaries in parallel. To simplify this process, we have packaged the devnet using Docker. Please make sure to install the latest version of Docker on your system before proceeding.

* Install Docker - [https://docs.docker.com/get-docker/](https://docs.docker.com/get-docker/)

## Building Docker images

Build images from the root of the Curio repository

```
make clean docker/devnet
```

*   If you need to build containers using a specific version of lotus then provide the version as a parameter. The version must be a tag of [Lotus git repo](https://github.com/filecoin-project/lotus). We are shipping images  for all releases from Lotus in our [Github image repo](https://github.com/filecoin-shipyard/lotus-containers/pkgs/container/lotus-containers).\


    ```bash
    make clean docker/devnet lotus_version=v1.29.2
    ```

    \

*   If the branch or tag you requested does not exist in our [Github image repository](https://github.com/filecoin-shipyard/lotus-containers/pkgs/container/lotus-containers) then you can build the lotus image manually.\


    ```bash
    make clean docker/devnet lotus_version=test/branch1 build_lotus=1
    ```

## Start devnet Docker stack

* Run

```
make devnet/up
```

* It will spin up `lotus`, `lotus-miner`, `yugabyte` and `curio` containers. All temporary data will be saved in `./docker/data` folder.
* The initial setup could take up to 5 min or more as it needs to download Filecoin proof parameters. During the initial setup, it is normal to see error messages in the log. Containers are waiting for the lotus to be ready. It may timeout several times. Restart is expected to be managed by `docker`.
* Try opening the Curio GUI [http://localhost:4701](http://localhost:4701) . Devnet is ready to operate when the URL opens and indicates no errors on the startup page.
* You can inspect the status using `cd docker/devnet && docker compose logs -f`.
