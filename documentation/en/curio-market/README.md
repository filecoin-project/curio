---
description: An overview of the Curio market module
---

# Curio Market

The Curio Storage Market is a comprehensive framework designed to manage Filecoin storage deals using a flexible and scalable architecture that supports multiple protocols, currently including MK1.2, with future protocols planned for seamless integration. The market handles deal-making through both online and offline processes, providing efficient tools for each step of the deal lifecycle.

* **Protocols:** The market is designed to support multiple deal protocols. Currently, MK1.2 is implemented, but the system can easily expand to accommodate future protocols without major reconfiguration.
* **Deal Flow:** The market manages both online and offline deals, ensuring smooth operations for various storage needs. Offline deals can utilize a PieceLocator configuration with HTTP servers to retrieve pieces when requested.
* **libp2p and Networking:** Curio’s market integrates with libp2p for peer-to-peer communication, allowing for decentralized and secure data transmission between storage clients and providers.
* **HTTP Server:** The HTTP server plays a vital role in managing tasks like offline deal data retrieval through URLs and handling client requests for deal processing.
* **Retrieval and Indexing:** Deals are efficiently indexed and can be retrieved for future use, ensuring that stored data remains accessible and manageable throughout its lifecycle.

The Curio Storage Market provides a highly configurable and extensible system for deal management, optimized for current and future decentralized storage protocols.

## Curio vs Boost

How is Curio market different from Boost?

### Deal Processing

In Curio’s deal processing, the task-based approach introduces modularity and flexibility in handling different stages of a deal's lifecycle. Each step, from data preparation to sealing, is managed as an individual task. This approach breaks down the complexity of the process by distributing specific responsibilities, such as data validation, publishing deals, and commP calculation, across multiple independent tasks.

Each task is monitored, retried on failure, and orchestrated through the Harmony task system, ensuring better resource utilization and scalability. This structure allows for parallel processing of deals and ensures that failures in one part do not impact the entire deal flow. Additionally, it provides flexibility for incorporating new protocols as they are introduced, seamlessly integrating them into the task orchestration.

This method ensures that Curio’s deal processing remains future-proof, adaptable to different protocols, and scalable across large volumes of data and interactions.

### Offline Deals

In Boost, offline deals were initiated by manually importing data on the storage provider's side. However, Curio simplifies this process by allowing users to [add a URL for offline deal data directly into the database](storage-market.md#add-data-url-for-offline-deals) or use the [`PieceLocator` configuration](storage-market.md#piecelocator-configuration) to point to a remote server that can serve the deal’s piece. Since Curio operates as a cluster rather than a single node, data might be needed on different nodes depending on task scheduling and execution. The remote read design in Curio provides flexibility by enabling any node to fetch the required data dynamically during task execution, ensuring efficient processing across the cluster.

### Optional CommP

In Boost, the CommP (Commitment to Piece) calculation was a mandatory step in the deal processing workflow. However, in Curio, CommP is optional and can be skipped if desired. This flexibility allows users to streamline deal processing by bypassing the CommP check when necessary, while still ensuring that deals can progress smoothly without compromising efficiency in specific use cases.

### HTTP only retrievals

Curio’s retrieval mechanism is HTTP-only, allowing serving deal data from multiple nodes, each running an HTTP server. These nodes can operate under different domain names, providing flexibility and scalability. This distributed retrieval design ensures that data can be fetched from any node within the cluster, based on availability and proximity, optimizing retrieval speed. By supporting multiple nodes and domains for HTTP-based retrieval, Curio enhances resilience and performance, allowing clients to access deal data efficiently without relying on a single centralized source, all within the HTTP protocol.

### IPNI Sync

Curio creates dedicated peerID for each miner ID which are used to identify the provider on IPNI. This peer ID is different from the peer ID on chain.

Curio announces per piece instead of per deal to IPNI nodes. This removes unnecessary overhead of mapping deals to ads and instead takes a simpler approach of advertising only for the piece we have regardless of how many times they are onboarded.

The chunking in Curio process leverages a database to ensure fast and efficient chunk creation and reconstruction. By storing the first CID of each chunk along with metadata like offsets and chunk numbers, the system can quickly locate and retrieve chunks. Sorted entries minimize redundancy by eliminating duplicates and allow for efficient querying and retrieval. Additionally, caching mechanisms and database indexing further enhance speed, making the system optimized for rapid chunking and reconstruction. This makes IPNI sync much faster in Curio compared to Boost.
