---
description: >-
  This guide describes how to attach and configure sealing and permanent storage
  for Curio nodes
---

# Storage Configuration

Each Curio node keeps track of defined storage locations in `~/.curio/storage.json` (or `$CURIO_REPO_PATH/storage.json`) and uses `~/.curio` path as default.

Upon initialization of a storage location, a `<path-to-storage>/sectorstorage.json` file is created that contains the UUID assigned to this location, along with whether it can be used for sealing or storing.

## Adding sealing storage location&#x20;

Before adding your sealing storage location you will need to consider where the sealing tasks are going to be performed. This command must be run locally from the Curio node where you want to attach the storage.

```sh
curio cli --machine <Machine IP:Port> storage attach --init --seal <PATH_FOR_SEALING_STORAGE>
```

## Adding long-term storage location&#x20;

**Custom location for storing:** After the _sealing_ process is completed, sealed sectors are moved to the _store_ location, which can be specified as follows:

```sh
curio cli --machine <Machine IP:Port> storage attach --init --store <PATH_FOR_LONG_TERM_STORAGE>
```

This command must be run locally from the Curio node where you want to attach the storage. This location can be made of large capacity, albeit slower, spinning-disks.

## Attach existing storage to Curio

The storage location used by `lotus-miner` or `lotus-worker` can be reused by the Curio cluster. It can be attached once the migrated `lotus-miner` or `lotus-worker` is already running a Curio service.

```
curio cli --machine <Machine IP:Port> storage attach <PATH_FOR_LONG_TERM_STORAGE>
```

## Segregating long-term storage per miner&#x20;

Users can allocate long-term storage to specific miner IDs by specifying miner address strings in the `sectorstore.json` file. This configuration allows precise control over which miners can use the storage.

*   To allow specific miners, include their addresses in the AllowMiners array:

    ```
    "AllowMiners": ["t01000", "t01002"]
    ```

    This configuration permits only the listed miners (t01000 and t01002) to access the storage.
*   Similarly, to deny specific miners access to the storage, include their addresses in the DenyMiners array:

    ```
    "DenyMiners": ["t01003", "t01004"]
    ```

    In this example, miners with addresses t01003 and t01004 are explicitly denied access to the storage.

This dual configuration approach allows for flexible and secure management of storage access based on miner IDs.

\
