---
description: >-
  This page covers the local indexing process in Curio, detailing how data is
  stored, indexed, and managed for efficient retrieval and processing.
---

# Indexing

Curio uses a local index store to manage the mapping of multihashes to content pieces. This system allows efficient retrieval of content across multiple nodes in the Curio cluster. The index store does not interact with IPNI directly but may be used to create and serve advertisements for the IPNI network when needed.

### Key Components:

* **Local Index Store**: The core of Curio's indexing system is a local Cassandra-based database that stores mappings of multihashes to piece CIDs along with offsets and sizes for retrieval purposes.
* **Concurrency & Batching**: The index store uses concurrent processes and batching for adding or removing entries to ensure efficient handling of large-scale indexing tasks.
* **Piece Mapping**: The system maps multihashes to pieces and provides the necessary offset and size data for locating content within a piece. This mapping is key for retrieval operations, especially when multiple pieces share common multihashes.
* **Integration with Retrievals**: The indexing system works hand-in-hand with Curio's retrieval system to ensure content can be efficiently located and served. By maintaining an accurate and up-to-date index, retrieval times are optimized.
* **Task-Based Indexing**: Indexing operations are carried out in the background through task-based processing for each deal.

## Index Store

Curio’s indexing mechanism relies on a Cassandra-based storage system. The local copy of the indexes allows efficient lookups for retrieval operations, independent of any interaction with IPNI (Interplanetary Network Indexer). However, when needed, this index can serve as a base for generating advertisements for IPNI.

The IndexStore component handles all interactions with the underlying Cassandra database, including creating, adding, and removing index entries, and querying pieces by multihashes.

### Concurrency and Batching

The IndexStore allows configurable concurrency and batching for operations. It uses multiple workers to process index entries, enabling parallel indexing to scale with the size of the dataset. These operations are critical for large datasets where inserting and managing millions of records can become a bottleneck without proper concurrency control.

### Configuration

The `IndexingConfig` struct defines two key configuration parameters that control how indexing operations are performed in Curio:

* **InsertBatchSize**: Specifies the number of records that will be grouped and inserted into the database in a single batch. This helps optimize performance by reducing the number of individual insert operations.
* **InsertConcurrency**: Defines how many concurrent workers will be used to handle the insertion of records into the index. This allows for parallel processing and improved performance, particularly when dealing with large-scale indexing tasks.

## Task-Based Indexing

To handle large indexing jobs efficiently, Curio leverages a task-based approach. Indexing tasks are created and executed asynchronously, allowing the system to manage and prioritize indexing operations without blocking other processes.

Tasks are assigned to individual nodes within the Curio cluster, and each node executes its indexing tasks based on the available system resources and the availability of the local copy of unsealed sector containing the deal. The results of these tasks are committed to the index store, ensuring that the data is available for retrieval.
