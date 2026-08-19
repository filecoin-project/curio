---
description: Curio command-line interface
---

# Curio CLI

Curio ships with 2 binaries called `curio` and `sptool` by default.

## Storage Provider Address (`SP_ADDRESS`)

Most `sptool` commands require a miner actor address and will not run without one. Set `SP_ADDRESS` in your environment so you do not have to pass `--actor` on every command:

```bash
export SP_ADDRESS=f01234
```

You can still override the address for a single command with `--actor`. Without `SP_ADDRESS` or `--actor`, `sptool` refuses to run.

## Curio Binary

The command line interface (CLI) for Curio operates slightly differently from typical software. Some commands, such as those related to storage, make API calls in the backend, while others interact directly with the database to perform the required actions.

Commands that make API calls require authentication since the Curio API is permissioned. This authentication involves passing a token and address with the API call. The Curio CLI handles this authentication seamlessly, eliminating the need to set environment variables for the token and address. It retrieves the authentication and actor details from the database and generates the API call accordingly.

This design allows you to make changes to a remote node without having direct access to it. For example, you can detach storage in Node 2 from Node 1. All such commands are nested under the `cli` subcommand.

To perform actions on a remote machine, you must provide the correct IP address and port using the `--machine` flag in the format `--machine=10.0.0.1:12300`.

This documentation explains the unique aspects of the Curio CLI, including its authentication process and how to interact with remote nodes.

The `curio` CLI references can be found [here](curio.md).

## Sptool Binary

Certain administrative and monitoring operations require updating or fetching information about the minerID from the chain. These operations do not need access to the database and therefore are not included in the Curio binary. Instead, these commands are hosted under the `sptool` binary. The `sptool` binary provides an interface with the Filecoin blockchain for operations required by a storage provider.

`sptool` requires a miner actor on every command. Set [`SP_ADDRESS`](#storage-provider-address-sp_address) (or pass `--actor`) before using it.

The `sptool` CLI references can be found [here](sptool.md).<br>
