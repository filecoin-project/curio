---
description: A comprehensive guide to configuring and managing storage deals in Curio
---

# Storage Market

## Overview

The Curio Storage Market provides a comprehensive framework for managing deals, data retrieval, and storage through decentralized protocols. This page details the different configurations, commands, and workflows available for managing storage deals, both online and offline.

The storage market in Curio is built around several key concepts:

* **Deal Protocols**: Protocols and workflows for deal-making, sealing, and data transfers.
* **Tasks**: Various tasks managed by the storage market, such as commP, PSD, indexing deals, IPNI advertisement etc.
* **Deal Flows**: The workflows for processing online and offline deals, each of which has specific tasks and checks to ensure the deal is successfully completed.

## Configuration

The Curio storage market is configurable through the `StorageMarketConfig` structure. This section outlines the main configuration parameters and the implications of setting them.

```go
type MarketConfig struct {
    StorageMarketConfig StorageMarketConfig
}

type StorageMarketConfig struct {
    MK12        MK12Config
    IPNI        IPNIConfig
    Indexing    IndexingConfig
    PieceLocator []PieceLocatorConfig
}
```

### **MK12 Configuration**

The `MK12` configuration encompasses all deal-related settings for the **MK1.2.0** and **MK1.2.1** deal protocols (commonly referred to as **Boost deals**). This configuration controls key parameters like batching, sealing time, and the number of deals that can be published at once.

```go
type MK12Config struct {
    PublishMsgPeriod        Duration
    MaxDealsPerPublishMsg   uint64
    MaxPublishDealFee       types.FIL
    ExpectedPoRepSealDuration Duration
    ExpectedSnapSealDuration Duration
    SkipCommP               bool
    DisabledMiners          []string
    MaxConcurrentDealSizeGiB int64
    DenyUnknownClients bool
    DenyOnlineDeals bool
    DenyOfflineDeals bool
    CIDGravityToken string
    DefaultCIDGravityAccept bool
}
```

**Key Parameters**

1. **PublishMsgPeriod**:\
   Specifies the time to wait before publishing deals as a batch. Increasing this period allows more deals to be included in a single message but delays the publishing of deals. Lowering this period will result in faster publishing but fewer deals being batched together, increasing chain overhead.
2. **MaxDealsPerPublishMsg**:\
   Controls the maximum number of deals to include in one batch. If set too high, the publish message may become too large and expensive. Setting it too low might reduce efficiency, as the node sends more messages.
3. **MaxPublishDealFee**:\
   This defines the maximum fee you’re willing to pay per deal when sending the `PublishStorageDeals` message. The consequence of setting a low fee is that deal publishing may be delayed or fail if network congestion raises gas costs.
4. **ExpectedPoRepSealDuration**:\
   This value controls how long you expect the Proof of Replication (PoRep) sealing process to take. Deals that cannot be sealed within this time will fail.
5. **ExpectedSnapSealDuration**:\
   Similar to PoRep, this defines the expected time for snap sealing. The duration should account for hardware speed and network delays.
6. **SkipCommP**:\
   If set to `true`, the CommP (Commitment Proof) check is skipped before the `PublishDealMessage` is sent on-chain. Skipping this step is risky because if there’s a mismatch, all deals in the sector may need to be resent.
7. **DisabledMiners**:\
   A list of miner addresses excluded from participating in deal-making. Use this option to prevent specific miners from handling deals if needed.
8. **MaxConcurrentDealSizeGiB:**\
   MaxConcurrentDealSizeGiB is a sum of all size of all deals which are waiting to be added to a sector when the cumulative size of all deals in process reaches this number, new deals will be rejected. (Default: 0 = unlimited)
9. **DenyUnknownClients:**\
   DenyUnknownClients determines the default behaviour for the deal of clients which are not in allow/deny list. If True then all deals coming from unknown clients will be rejected.
10. **DenyOnlineDeals**: Determines whether the storage provider **accepts online deals**.
11. **DenyOfflineDeals**: Determines whether the storage provider **accepts offline deals**.
12. **CIDGravityToken**:\
    The authorization token used for **CIDGravity filters**, a service that filters deal proposals based on custom policies. If empty (`""`), **CIDGravity filtering is disabled**. If set, the miner will **query CIDGravity** for each deal proposal before accepting it.
13. **DefaultCIDGravityAccept**:\
    Defines what happens if the **CIDGravity service is unavailable**. If`true`: **Accepts deals** even if CIDGravity is unreachable. If`false`: **Rejects deals** when CIDGravity is unavailable (**default**).


### **PieceLocator Configuration**

This configuration allows you to set up remote HTTP servers that provide piece data for offline deals. A `PieceLocator` config is a combination of a URL and headers for fetching pieces when requested by the miner. This is crucial for handling offline deals where data is not available immediately and must be retrieved during the commP and sealing phase.

```go
type PieceLocatorConfig struct {
    URL     string
    Headers http.Header
}
```

* **URL**: The endpoint where the piece data can be located.
* **Headers**: Any custom headers needed for the HTTP request, such as authorization tokens.

{% hint style="warning" %}
PieceLocator service will allow Curio to lookup details of a piece automatically for an offline deal. The `add-url` command should not be used for deal which are expected to fetch the data from PieceLocator services.
{% endhint %}

Consequences: If the piece data is not available at the specified URL, the offline deal will fail. Make sure that the remote server is properly configured and available.

## Enabling Storage Market

To enable the Curio market on a Curio node, the following configuration changes are required:

1. **Enable the Deal Market**:
   * Set `EnableDealMarket` to `true` in the `CurioSubsystemsConfig` for at least one node. This enables deal-making capabilities on the node.
2. **Enable CommP**:
   * On one of the nodes where `EnableDealMarket` is set to `true`, ensure that `EnableCommP` is also set to `true`. This allows the node to compute piece commitments (CommP) before publishing storage deal messages.
3. **Enable HTTP**:
   * At least one node must have HTTP enabled to support:
     * Retrievals.
     * IPNI sync.
     * Handling storage deals.
   * To enable HTTP, set the `Enable` flag in the `HTTPConfig` to `true` and specify the `ListenAddress` for the HTTP server.
4. **Set a Domain Name**:
   * Ensure that a valid `DomainName` is specified in the `HTTPConfig`. This is mandatory for proper HTTP server functionality and essential for enabling TLS. The domain name cannot be an IP address.
   * Domain name should be specified in the base layer
5. **HTTP Configuration Details**:
   * If TLS is managed by a reverse proxy, enable `DelegateTLS` in the `HTTPConfig` to allow the HTTP server to run without handling TLS directly.
   * Configure additional parameters such as `ReadTimeout`, `IdleTimeout`, and `CompressionLevels` to ensure the server operates efficiently.
6. **Libp2p Activation**:
   * The `libp2p` service will automatically start on one of the servers running the HTTP server where `EnableDealMarket` is set to `true`. If more than 1 node satsifies the condition and the node running libp2p goes down then it will switch over to another node after 5 minutes.
7. **Other Considerations**:
   * Ensure the `MK12Config` settings under `StorageMarketConfig` are properly configured for deal publishing. Key parameters include:
     * `PublishMsgPeriod` for deal batching frequency.
     * `MaxDealsPerPublishMsg` for the maximum number of deals per message.
     * `MaxPublishDealFee` to set the fee limit for publishing deals.
   * If handling offline deals, configure `PieceLocator` to specify the endpoints for piece retrieval.
8. Verify that HTTP server is working:

    * Curl to your domain name and verify that server is reachable from outside\


    ```shell
    curl https://<Domain name>

    Hello, World!
     -Curio
    ```

{% hint style="warning" %}
If you do not get above output then something went wrong with configuration and you should not proceed with migration from Boost or Deal making.
{% endhint %}

By applying these changes, the Curio market subsystem will be activated on the specified node(s), enabling storage deals, IPNI synchronization, and retrieval functionality.

## MK12 Deals (Boost Deals)

The **MK12** protocol governs the entire deal process, from proposing and publishing deals to sealing and validating them. It's designed to work efficiently for both online and offline deals.

Key tasks within MK12 include:

* **Piece Commitment**: Ensuring that the piece has been added to a sector.
* **Publish Storage Deals (PSD)**: Sending the on-chain message to register the deal.
* **Finding Deals**: Identifying the deal ID on-chain and adding it to the sector.

### MK12 Tasks

Tasks refer to operations that the system performs on deals to ensure their success. Tasks include:

* **CommP tasks**: Commitment proof tasks, ensuring the piece information is correct by calculating the commitment locally.
* **PSD tasks**: Tasks for sending and validating the PublishStorageDeals message.
* **Find Deal tasks**: These tasks poll the blockchain to identify the deal’s status and retrieve its ID after the deal has been published successfully with PSD task.

### Online Deal Flow

1. **Deal Proposal**:\
   A client proposes a deal, specifying the amount of data and terms.
2. **Data Transfer**:\
   Data is transferred immediately. The system checks that the entire piece has been received.
3. **CommP Task**:\
   Once data is received, a commitment proof is generated.
4. **PSD Task**:\
   The deal is published on-chain.
5. **Sector Assignment**:\
   The deal is assigned to a sector and sealed.

### Offline Deal Flow

1. **Deal Proposal**:\
   The client proposes a deal for data that is not yet available on the miner’s node.
2. **PieceLocator**:\
   The miner is provided with a URL where the data can be fetched later.
3. **Data Fetch**:\
   The miner fetches the piece from the provided URL using the `PieceLocator` configuration or [local database](storage-market.md#add-data-url-for-offline-deals).
4. **CommP Task**:\
   Once the piece is retrieved, a commitment proof is generated.
5. **PSD Task**:\
   The deal is published on-chain.
6. **Sector Assignment**:\
   The deal is added to a sector and sealed.

### Add data URL for offline deals

The `add-url` command allows you to specify a URL from which the miner can fetch piece data for offline deals. This is essential for deals where the client does not transfer the data immediately upon deal acceptance.

{% hint style="warning" %}
The `add-url` command should not be used for deal which are expected to fetch the data from PieceLocator services.
{% endhint %}

```bash
curio market add-url [command options] <deal UUID> <raw size/car size>
```

**Example Usage**:

```bash
curio market add-url --url "https://data.server/pieces?id=pieceCID" --header "Authorization: Bearer token" <UUID> <raw size>

OR

curio market add-url --url "https://data.server/filename" --header "Authorization: Bearer token" <UUID> <raw size>

OR

curio market add-url --url "https://data.server/filename" <UUID> <raw size>
```

**Options**:

* **--url**: The URL where the piece data can be fetched.
* **--header**: Custom headers to include in the HTTP request.

Consequences: If the URL is not accessible or the headers are incorrect, the deal will fail to retrieve the data and will not be able to complete.

### Move funds to escrow

The `move-to-escrow` command moves funds from the deal collateral wallet to the escrow account with the storage market actor. This is necessary to lock in collateral for a deal.

```bash
curio market move-to-escrow [command options] <amount>
```

**Example Usage**:

```bash
curio market move-to-escrow --actor <actor address> --max-fee 3 <amount>
```

**Options**:

* **--actor**: Specifies the actor address that should start sealing sectors for the deal.
* **--max-fee**: Maximum fee in FIL you’re willing to pay for this message.

Consequences: If insufficient funds are moved to escrow, the deal may not be processed, and the collateral may not be secured.

### Start Sealing Early

The `seal` command allows you to start sealing a deal's sector early, before all the deals have been batched.

```bash
curio market seal [command options] <sector>
```

**Example Usage**:

```bash
curio market seal --actor <actor address> <sector>
```

**Options**:

* **--actor**: Specifies the actor address.

Consequences: Sealing early can speed up the process, but it may result in inefficiencies if all deals are not batched correctly.

## Offline Verified DDO deals

Curio only supports offline verified DDO deals as of now. The allocation must be created by the client for the piece and handed over to the SP alongside the data.

### How to create allocation

Clients can create allocation using the `sptool toolbox` or other methods.

```shell
sptool --actor t01000 toolbox mk12-client allocate -p <MINER ID> --piece-cid <COMMP> --piece-size <PIECE SIZE>
```

### Start a DDO deal

Storage providers can onboard the DDO deal using the below command.

```shell
curio market ddo --actor <MINER ID> <client-address> <allocation-id>
```

Since this is an offline deal, user must either make the data available via PieceLocator or add a data URL for this offline deal.
