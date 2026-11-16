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

On the public firewall, open only **TCP 80 and 443**. Those ports serve the HTTP API (market, retrieval, and PDP) and Let's Encrypt. The admin GUI, Curio API, database, and chain RPC stay on the private network or localhost.

| Port    | Details                                                                                                      |
| ------- | ------------------------------------------------------------------------------------------------------------ |
| 80, 443 | Public HTTP API. These are the only ports to open on the public firewall.                                   |
| 12300   | Default API port. Cluster administration on the private network.                                            |
| 4701    | Default GUI port. Localhost an SSH tunnel, or Tailscale connection. Not every Curio node needs the GUI.                           |
| 12310   | Default HTTP listen port when it is not 80/443. Reach it from a reverse proxy on the private network.       |

[Curio-PDP](curio-pdp.md) publishes the PDP API on 80 and 443 and keeps the admin GUI on localhost port **4701**. Its bundled Forest chain node also needs its libp2p ports public: TCP and QUIC (UDP). Forest picks an ephemeral port for each unless `--p2p-listen-address` pins them.

## Boost Compatibility (Deprecated)

Boost is no longer compatible with latest Curio releases. Boost adapter is no longer shipped with our main branch and we recommend users to migrate to Curio markets.

## Choose a deployment

### Full Curio cluster (sealing and market)

With an understanding of Curio's internal mechanisms, you can now proceed to [install the Curio binaries](installation.md). We recommend using [Debian packages](installation.md#debian-package-installation) for the installation, as they facilitate easy installation, upgrades, and process management. After installing your first Curio binary, you can move on to [setting up Curio](setup.md), whether you are [migrating from lotus-miner](setup.md#migrating-from-lotus-miner-to-curio) or [initializing a new minerID](setup.md#initiating-a-new-curio-cluster).

### Curio-PDP (PDP-only)

If you are operating a **PDP storage provider** without PoRep or sealing, use **Dockerized Curio-PDP**. That stack is a supported path — not an experimental feature. Docker Compose runs Curio-PDP, YugabyteDB, and Forest.

Start with [Curio-PDP](curio-pdp.md). Review [hardware requirements](experimental-features/Enable-PDP.md#hardware-requirements) first. Run the Docker stack on **fast storage** (NVMe/SSD). Piece data mapped into the container data folder can live on **slower disks**.

{% content-ref url="curio-pdp.md" %}
[curio-pdp.md](curio-pdp.md)
{% endcontent-ref %}

## Best Practices

We have compiled [a list of best practices](best-practices.md) for deploying and maintaining a Curio cluster. All users are encouraged to follow these recommendations to avoid potential issues.

New users should also familiarize themselves with [both binaries shipped with Curio](curio-cli/) and the [GUI pages](curio-gui.md).
