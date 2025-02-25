---
description: This is a step by step guide for new users to get onboarded with Curio
---

# Getting Started

## Curio Database and Distributed Architecture

### Familiarizing Yourself with Curio

Before diving into the setup and configuration of Curio, we highly recommend becoming familiar with [Curio's design and fundamental principles](design/). This foundational knowledge will greatly assist in effective administration and troubleshooting.

### **HarmonyDB with YugabyteDB**

Curio utilizes YugabyteDB to create an abstraction layer known as HarmonyDB. This HarmonyDB serves two primary purposes:

1. **Metadata Storage**: It stores all Curio-related metadata.
2. **Consensus Layer**: It establishes a consensus layer for the distributed architecture of a Curio cluster.

{% hint style="danger" %}
We recommend using at least 3 node YugabyteDB cluster for HA and scalability. Loss of the DB will render Curio dead. YugabyteDB should also be backed up regularly.
{% endhint %}

### Key Features of HarmonyDB

* **High Availability**: Ensures that the metadata and consensus information is always available, even in the event of node failures.
* **Scalability**: Capable of handling increasing amounts of data and expanding as the Curio cluster grows.
* **Consistency**: Maintains data consistency across the distributed nodes of the Curio cluster.

### Benefits of Using YugabyteDB for HarmonyDB

* **Distributed SQL**: Combines the benefits of SQL with the resilience and scalability of a distributed database.
* **Fault Tolerance**: Provides strong fault tolerance, ensuring the reliability of the Curio cluster.
* **Multi-Region Deployment**: Supports deployment across multiple regions for improved performance and redundancy.

## Chain Node

Curio requires access to at least one Filecoin chain node like [Lotus](https://lotus.filecoin.io/lotus/get-started/what-is-lotus/) or [Forest](https://docs.forest.chainsafe.io/) (integration in progress). This chain node is used by Curio to get the current chain state and send messages to the chain. Curio support using multiple chain nodes.

## Network

Following port must be opened on each Curio node for API and GUI access

| Port  | Details                                                                                       |
| ----- | --------------------------------------------------------------------------------------------- |
| 12300 | Default API port                                                                              |
| 4701  | Default GUI port. Not all Curio nodes are required to enable GUI                              |
| 32100 | Market Port. This port is determined by the user when enabling Boost access in configuration. |

## Boost Compatibility (Deprecated)

Boost is no longer compatible with latest Curio releases. Boost adapter is no longer shipped with our main branch and we recommend users to migrate to Curio markets.

## Installing Curio and creating a Curio cluster

With an understanding of Curio's internal mechanisms, you can now proceed to [install the Curio binaries](installation.md). We recommend using [Debian packages](installation.md#debian-package-installation) for the installation, as they facilitate easy installation, upgrades, and process management. After installing your first Curio binary, you can move on to [setting up Curio](setup.md), whether you are [migrating from lotus-miner](setup.md#migrating-from-lotus-miner-to-curio) or [initializing a new minerID](setup.md#initiating-a-new-curio-cluster).

## Best Practices

We have compiled [a list of best practices](best-practices.md) for deploying and maintaining a Curio cluster. All users are encouraged to follow these recommendations to avoid potential issues.

New users should also familiarize themselves with [both binaries shipped with Curio](curio-cli/) and the [GUI pages](curio-gui.md).
