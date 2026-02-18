---
description: >-
  This page covers the local indexing process in Curio, detailing how data is
  stored, indexed, and managed for efficient retrieval and processing.
---

# Indexing

Curio uses a local index store to manage the mapping of multihashes to content pieces. This system allows efficient retrieval of content across multiple nodes in the Curio cluster.

The index store does not interact with IPNI directly, but Curio may use index data to create and serve advertisements for the IPNI network when enabled.

## Key Components

* **Local Index Store**: A Cassandra-based local store that maps multihashes → piece CIDs + offsets/sizes.
* **Concurrency & Batching**: Configurable concurrency and batch sizing for index inserts.
* **Integration with Retrievals**: Retrieval performance depends on correct, timely indexing.
* **Task-Based Indexing**: Indexing work is performed asynchronously via tasks.

## Index Store

Curio’s indexing mechanism relies on a Cassandra-based storage system. The local copy of the indexes allows efficient lookups for retrieval operations.

The IndexStore component handles all interactions with the underlying Cassandra database, including creating, adding, and removing index entries, and querying pieces by multihashes.

### Concurrency and Batching

The IndexStore allows configurable concurrency and batching for operations. It uses multiple workers to process index entries, enabling parallel indexing to scale with the size of the dataset.

### Configuration

The `IndexingConfig` struct defines parameters that control how indexing operations are performed:

* **InsertBatchSize**: number of records per batch insert.
* **InsertConcurrency**: number of concurrent insert workers.

## Task-Based Indexing

Indexing tasks are created and executed asynchronously, allowing Curio to manage indexing without blocking other subsystems.

Tasks are assigned to nodes within the Curio cluster, and each node executes indexing tasks based on the available system resources and the availability of the local copy of unsealed sector containing the deal.

---

## How “CheckIndex” works (and why it fails)

Operators frequently see errors on a task called **CheckIndex** and aren’t sure what it does.

What it does (high level):
- Periodically scans for indexing and announcement work that should exist (or be retried).
- Schedules follow-up tasks when it finds missing/failed items.

Code reference:
- `tasks/indexing/task_check_indexes.go` implements the `CheckIndex` task.

### Common failure mode: DB health/latency

If Yugabyte is slow/unhealthy, downstream tasks (including market/indexing workflows) can appear broken.

Before chasing indexing logic:
- run the DB connectivity check from the Curio host
- check tserver logs for FATALs

See: [Yugabyte troubleshooting](../administration/yugabyte-troubleshooting.md)

### Common failure mode: duplicate key (SQLSTATE 23505)

You may see:
- `duplicate key value violates unique constraint ... (SQLSTATE 23505)`

This often indicates concurrency/retries inserting the same identity row.

What to do:
- Determine whether the pipeline is still progressing.
- If tasks are stuck, collect deal UUID + piece CID + failing task IDs and consult:
  - [Curio Market troubleshooting](troubleshooting.md)
