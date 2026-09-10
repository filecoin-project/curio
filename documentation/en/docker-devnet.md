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

The root `Dockerfile` builds the standalone Curio image with `curio` and `sptool`. The devnet build first creates its debug variant as `curio/curio:debug`, then uses `docker/devnet/Dockerfile` to add Lotus, the piece server, indexer, Node.js, and Foundry. This produces `curio/curio-all-in-one:latest-debug`, which the devnet service images extend. The debug variant matches the existing devnet network; the standalone `2k` variant is a separate network build.

To build only the standalone debug image, run `make docker/curio-base`. To build the shared devnet image and its dependencies, run `make docker/curio-all-in-one`. Set `curio_docker_user` to change the image namespace, or `curio_runtime_image` to change the local standalone image tag. The service image names and Compose configuration are unchanged.

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

* It will spin up `lotus`, `lotus-miner`, `yugabyte`, `curio` and `piece-server` containers. All temporary data will be saved in `./docker/data` folder.
* The initial setup could take up to 5 min or more as it needs to download Filecoin proof parameters. During the initial setup, it is normal to see error messages in the log. Containers are waiting for the lotus to be ready. It may timeout several times. Restart is expected to be managed by `docker`.
* Try opening the Curio GUI [http://localhost:4701](http://localhost:4701) . Devnet is ready to operate when the URL opens and indicates no errors on the startup page.
* You can inspect the status using `docker compose -f docker/docker-compose.yaml logs -f` from the repository root.

## Make a deal in devnet
1. Login to `piece-server` container either via docker desktop UI or with below command
    ```shell
    docker exec -it piece-server /bin/bash
    ```
2. Run the below command to make a deal and follow the on-screen instructions.
    ```shell
    ./sample/make-a-deal.sh
    ```
