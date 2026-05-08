Local source mounts for `contracts-bootstrap`.

Expected layout:

- `docker/local-src/filecoin-services`
- `docker/local-src/multicall3`

Pinned pair:

- `filecoin-services`: `v1.1.0`
- `pdp` (submodule inside filecoin-services): `v3.1.0`

Prepare local sources once (outside compose):

```bash
cd docker/local-src
git clone --branch v1.1.0 --depth 1 https://github.com/FilOzone/filecoin-services.git
git -C filecoin-services submodule update --init --recursive

git clone --depth 1 https://github.com/mds1/multicall3.git
git -C multicall3 submodule update --init --recursive

cd ../../
```

`docker/.env` defaults use `CONTRACT_SOURCE_MODE=local`, so runtime clone/download is skipped.
