---
description: >-
  This page details the IPNI provider's sync process over HTTP, covering
  proactive announcements, indexer polling, and advertisement chain retrieval.
---

# IPNI (Interplanetary Network Indexer) Provider

The IPNI provider in Curio is designed to manage HTTP-based content announcements and indexing for decentralized discovery through external indexing nodes. It facilitates content chunking, advertisement generation, and HTTP-based announcements to indexers. The following sections explain the provider design, configuration, tasks, advertisements, and the synchronization process based on the [IPNI HTTP Provider specification](https://github.com/ipni/specs/blob/main/IPNI_HTTP_PROVIDER.md).

## IPNI Provider Identification

**Curio's Approach to PeerID Management**

In Curio, a **single PeerID is used across all MinerIDs** and is maintained on-chain. This ensures a consistent identity for all interactions and prevents unauthorized modifications. If the PeerID is changed manually on-chain, Curio will revert it to the expected value upon restart, ensuring stability and automation. This peerID is used only for mk1.2 deals which use Graphsync protocol.

For **IPNI integration**, Curio assigns **unique PeerIDs per MinerID**, but these are **not stored on-chain**. These unique PeerIDs are essential for retrieval verification. Spark uses them to correctly identify miners and validate retrieval operations. This dual approach—using a **single on-chain PeerID for general operations** and **unique off-chain PeerIDs for IPNI**—ensures compatibility across different systems while maintaining efficiency.

**Implementation Details**

To support this architecture, the **MinerPeerIDMapping** smart contract has been deployed at **0x40721e8Ef366375492ee63c54e881068b15C8633**. This contract provides a decentralized and secure way to manage MinerID-to-PeerID mappings, ensuring that only authorized entities can modify these associations.

* **Public State Mapping**: The contract maintains a **gas-free lookup table** mapping Filecoin MinerIDs (`uint64`) to PeerIDs.
* **Secure Update Mechanism**: Mappings can be modified through three operations using one of the control addresses:
  * **Add**: Associates a new PeerID with a MinerID.
  * **Update**: Replaces an existing PeerID with a new one.
  * **Delete**: Removes the binding between a MinerID and a PeerID.
* **Signed Message Verification**: Updates require a JSON-encoded signed message, ensuring authenticity. The message is signed using the on-chain PeerID.
* **Automated Integration with Curio**: Curio ensures that the on-chain PeerID remains updated automatically, preventing unauthorized changes. This allows Spark to correctly associate Ads with MinerIDs, even when different PeerIDs are used in other systems.

## Provider Design

The Curio IPNI provider operates over HTTP, managing content updates through advertisement creation and announcement. It does not use libp2p; instead, it interacts with indexing nodes by sending HTTP requests to announce content and expose the advertisement chain for indexing.

### Key Components

* **Chunking**: Large datasets are divided into smaller chunks using the `Chunker` to create manageable entries for advertisement. Each chunk contains a subset of the data for efficient indexing by external nodes.
* **Advertisement Creation**: After chunking the content, the provider generates IPLD-based advertisements. These advertisements contain essential metadata, including the content's addresses, the provider's identity, and a chain linking the advertisement to previous content updates.
* **HTTP Announcements**: Once an advertisement is created, HTTP requests are sent to specific indexing nodes via the configured `DirectAnnounceURLs`, informing them about new advertisements. This proactive method ensures that indexers are notified of content changes quickly.
* **Sync Mechanism**: Indexers receive announcements about new advertisements via HTTP. In cases where no new announcements are received for a while, the indexers poll the provider’s `/ipni/v1/ad/head` endpoint to retrieve the latest advertisement head CID. From there, they traverse the advertisement chain, fetching the missing updates. This ensures that indexers stay up-to-date with the provider's latest content, even if no explicit announcements are sent for a period of time.

## Configuration

The behaviour of the Curio IPNI provider is controlled through the `IPNIConfig` structure, which defines how content announcements and synchronization with indexing nodes are handled.

### Default Configuration

```go
 [Market.StorageMarketConfig.IPNI]
      # Disable set whether to disable indexing announcement to the network and expose endpoints that
      # allow indexer nodes to process announcements. Default: False
      #
      # type: bool
      #Disable = false

      # The network indexer web UI URL for viewing published announcements
      # TODO: should we use this for checking published heads before publishing? Later commit
      #
      # type: []string
      #ServiceURL = ["https://cid.contact"]

      # The list of URLs of indexing nodes to announce to. This is a list of hosts we talk to tell them about new
      # heads.
      #
      # type: []string
      #DirectAnnounceURLs = ["https://cid.contact/ingest/announce"]
```

### **IPNIConfig Fields**

* **Disable**: Disables indexing announcements if set to `true`. Default: `false`.
* **ServiceURL**: URLs for accessing the indexer web UI to view published advertisements.
* **DirectAnnounceURLs**: URLs of indexing nodes where the provider sends HTTP announcements of new advertisements.

## IPNI Task

The **IPNI task** is responsible for reading content, chunking it, creating advertisements, and sending HTTP announcements to indexing nodes.

#### Task Lifecycle

1. **Content Reading**: The task reads content (sectors) from storage.
2. **Chunking**: The content is divided into smaller chunks using a`chunker`, organizing it for efficient indexing.
3. **Advertisement Creation**: A signed IPLD-based advertisement is generated for the chunked content, linking it to previous advertisements in the chain.
4. **Database Update**: The task marks itself as complete in the database once the content is advertised and announced to the indexer.

The task ensures that content is indexed and made available to indexing nodes by creating manageable advertisement entries and sending updates via HTTP.

## Chunker

The **chunker** generates entries for advertisements by leveraging the [**index store**](indexing.md#index-store)

1. **First CID for Speed Optimization**:
   * The chunking process begins with multihashes (`multihash.Multihash`) stored in the database.
   * For each chunk, the **first CID** (Content Identifier) is crucial as it is used to quickly locate and reconstruct the chunk when needed. This CID is often stored as the `first_cid` in the database along with other metadata like chunk number, piece CID, and offsets.
2. **Sorted Entries in Database**:
   * During chunk creation, the entries (multihashes) are first sorted in ascending order based on their binary value for efficient processing and retrieval.
   * Sorting ensures that duplicates are identified and removed, minimizing redundancy. This process involves:
     * Sorting the multihashes.
     * Removing duplicates.
   * After sorting, the entries are split into chunks of predefined size (`entriesChunkSize`, e.g., 16,384 multihashes per chunk).
3. **Chunk Metadata Storage**:
   * Each chunk is associated with metadata stored in the database, including:
     * `cid`: Unique identifier for the chunk.
     * `piece_cid`: Identifier linking the chunk to its corresponding data piece.
     * `chunk_num`: Chunk sequence number.
     * `first_cid`: First multihash in the chunk for quick lookup.
     * `start_offset`: Byte offset (if applicable).
     * `num_blocks`: Number of entries in the chunk.
4. **Chunk Retrieval and Reconstruction**:
   * To reconstruct a chunk:
     * The database is queried using the `cid` to fetch metadata, including the `first_cid`.
     * The system then either reads data directly from the database (sorted entries).
   * For each chunk, a linked structure (`schema.EntryChunk`) is created, linking to the next chunk via an IPLD link.
5. **Linked Chunks**:
   * Chunks are linked to each other using the `Next` field in the IPLD node structure, forming a chain that can be navigated from the head.
6. **Efficient Querying**:
   * The system leverages the database for rapid querying of metadata and uses caching (e.g., LRU caches) to store recent chunk data for reuse and speculative pre-fetching.

This design ensures fast chunking and reconstruction by leveraging the sorted entries and storing the first CID, allowing efficient access and reusability.

## Advertisements

Advertisements describe the content available for indexing and discovery. Each advertisement is represented as an IPLD node containing the following fields:

* **PreviousID**: The CID of the previous advertisement, forming a chain of advertisements. It’s empty for the genesis (first) advertisement.
* **Provider**: The unique identifier of the content provider (peerID). This peerID is unique for IPNI per miner ID.
* **Addresses**: Multiaddresses where clients can access the provider’s content i.e. retrievals.
* **Entries**: A link to the multihashes of the advertised content.
* **ContextID**: An identifier used to track updates or removals associated with the advertisement.
* **Metadata**: Additional protocol-specific data used for retrieval.
* **IsRm**: A flag indicating whether the advertisement removes previously published content.

### Entries Structure

A linked list of multihashes in an advertisement, where `Next` links to the next chunk. Each chunk is kept under 4MB, allowing up to 16384 multihashes per chunk.

## Announcement

The provider sends HTTP announcements to notify indexing nodes of new advertisements. This is a proactive method for updating indexers about content changes without relying solely on polling.

### Announcement Workflow

1. **Ad Creation**: The provider creates IPLD-based advertisements containing content metadata and multiaddresses.
2. **HTTP Announcements**: These advertisements are announced to the indexer nodes specified in the `DirectAnnounceURLs` via HTTP requests.
3. **Sync:** Indexer nodes query the IPNI provider for new advertisements based on the announcements and sync the available context indexes.
4. **Client Access**: Clients query the indexer nodes to discover content based on the newly announced advertisements.

Announcements ensure that indexers are aware of updates quickly, reducing the time it takes to ingest new content.

## Serving IPNI Ads and Entries

To serve IPNI advertisements and entries in Curio, the HTTP server exposes specific route. This route allow the retrieval of advertisement data and entry chunks through the following paths:

1. **Head Request Path:**
   * **Endpoint:** `/ipni-provider/{providerId}/ipni/v1/ad/head`
   * **Description:** This endpoint allows indexers to fetch the latest advertisement from a provider by requesting the head of the advertisement chain.
2. **Advertisement and Entry Request Path:**
   * **Endpoint:** `/ipni-provider/{providerId}/ipni/v1/ad/{cid}`
   * **Description:** This endpoint serves both advertisements and entry chunks based on the requested CID. The type of content returned (advertisement or entry) is determined by the CID and schema provided in the request headers. If the schema is not specified, the server defaults to checking for an advertisement and, if not found, falls back to serving the entry chunk associated with the given CID.

These routes are registered in the Curio HTTP server as part of the IPNI integration, enabling smooth and efficient data sharing between providers and indexers. The server also periodically publishes the latest advertisement head for each provider.

## IPNI Sync

The synchronization process ensures that indexing nodes stay updated with the latest content announcements from the provider.

#### Sync Process:

1. **Proactive Announcements**: Indexing nodes are notified via HTTP announcements whenever the provider has new advertisements. This is the primary mechanism for keeping indexers in sync with the provider.
2. **Head Resource for Polling**: If no announcements are received for a period of time, indexers may poll the provider’s `/ipni/v1/ad/head` endpoint to check for new content. This head CID represents the latest advertisement in the chain.
3. **Chain Retrieval**: When indexers receive a new head CID, they traverse the advertisement chain backward, starting from the head, fetching each advertisement via HTTP (`/ipni/v1/ad/{CID}`). The indexer processes the advertisement chain in order from the oldest unseen advertisement to the newest.
4. **Continuous Updates**: Through a combination of announcements and periodic polling, indexers ensure they have the most up-to-date content from the provider, even if an explicit announcement was not sent.

This process ensures that indexers remain synchronized with the provider’s content, maintaining up-to-date knowledge of available advertisements and content retrieval addresses.

By using this mechanism, indexers can stay synchronized with the provider’s content in real-time or through periodic polling, ensuring continuous updates.

### Serving Advertisements:

* **Advertisement Fetching**: Indexers fetch advertisements and entries directly from the provider via HTTP, making the advertised IPLD objects available for ingestion.
* **Head Requests**: The provider exposes the latest advertisement through the `/ipni/v1/ad/head` endpoint, allowing indexers to know the most recent state of the advertisement chain.
* **Serving Entries:** When indexers need to fetch advertised entries, they request specific entry chunks through their corresponding CID by making a GET request to the `/ipni/v1/ad/{CID}` endpoint. The provider serves these entries by reading either from the CAR file or from the stored index data, depending on how the entries were chunked during advertisement creation. This ensures efficient retrieval of multihashes for large datasets.

This synchronization process ensures that indexers can efficiently track the latest updates from the provider, enabling quicker content discovery across the network.
