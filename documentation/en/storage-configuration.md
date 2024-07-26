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

## Filter sector types <a href="#filter-sector-types" id="filter-sector-types"></a>

You can filter for what sectors types are allowed in each sealing path by adjusting the configuration file in: `<path-to-storage>/sectorstorage.json`.

```json
{
  "ID": "1626519a-5e05-493b-aa7a-0af71612010b",
  "Weight": 10,
  "CanSeal": false,
  "CanStore": true,
  "MaxStorage": 0,
  "Groups": [],
  "AllowTo": [],
  "AllowTypes": null,
  "DenyTypes": null
}
```

Valid values for `AllowTypes` and `DenyTypes` are:

```javascript
"unsealed"
"sealed"
"cache"
"update"
"update-cache"
```

These values must be put in an array to be valid (e.g `"AllowTypes": ["unsealed", "update-cache"]`), any other values will generate an error on startup of the `Curio`. A restart of the `Curio` node where this storage is attached is also needed for changes to take effect.

## Separate sealed and unsealed&#x20;

A very basic setup where you want to separate unsealed and sealed sectors could be achieved by:

* Add `"DenyTypes": ["unsealed"]` to long-term storage path(s) where you want to store the sealed sectors.
* Add `"AllowTypes": ["unsealed"]` to long-term storage path(s) where you want to store the unsealed sectors.

Setting only `unsealed` for `AllowTypes` will still allow `cache` and `update-cache` files to be placed in this storage path. If you want to completely deny all other types of sectors in this path, you can add additional valid values to the `"DenyTypes"` field.

{% hint style="info" %}
If there are existing files with disallowed types in a storage path, those files will remain readable for PoSt/Retrieval. So the worst that can happen in case of misconfiguration in the storage path is that sealing tasks will get stuck waiting for storage to become available.
{% endhint %}

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
