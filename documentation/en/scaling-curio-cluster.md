---
description: >-
  This page describes how to add additional nodes or miner IDs to a Curio
  cluster
---

# Scaling Curio cluster

## Migrating additional lotus-miner to Curio

To migrate your second or later `lotus-miner` to an existing Curio cluster, you need to follow the same steps as before. The only exception would be that there is no need to install a new YugabyteDB cluster. After installing the `curio` binary on the `lotus-miner` node, you can run `curio guided-setup` to start the migration.

## Initialising additional Miner IDs in existing Curio cluster

The process to initiate a new minerID on the network is same as when you [initialise a new Curio cluster with a new minerID.](setup.md#initiating-a-new-curio-cluster) The only exception would be that there is no need to install a new YugabyteDB cluster

## Migrating lotus-worker to Curio cluster

Once you have migrated a minerID to the Curio cluster, you would need to repurpose all of your `lotus-worker` nodes attached to the migrated minerID as Curio nodes.

1. [Install](installation.md) `curio` binary on the `lotus-worker` node.
2. [Configure the service ENV file](curio-service.md#environment-variables-configuration) with correct details.
3. Start the new `curio` node and verify in GUI that the new node is now part of the cluster.
4. [Attach the existing storage](storage-configuration.md#attach-existing-storage-to-curio) to the Curio node.
5. Repeat for the rest of the workers.

## Adding nodes to Curio cluster

To add new nodes to an existing Curio cluster, please follow the below process.

* [Install](installation.md) `curio` binary.
* [Configure the service ENV file](curio-service.md#environment-variables-configuration) with correct details.
* Start the new `curio` node and verify in GUI that the new node is now part of the cluster.
* [Attach any new or existing storage](storage-configuration.md) if required.
* Repeat for any additional nodes to be attached.
