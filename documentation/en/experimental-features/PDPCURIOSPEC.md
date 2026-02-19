# PDP in Curio Spec

This is a design document explaining the details of the subsystems of curio that implement the PDP storage provider protocol.  This is a design document describing code it does not aim to be a PDP protocol spec.  This design document does not aim to describe curio harmonyDB, harmony tasks, task scheduler though perhaps a new document describing these systems at a similar level would be useful.

This document is a work in progress.

# Overview

Curio has the capability to provide PDP protocol data storage.  It fullfills the functions of receiving uploads from users,  storing user data, providing IPFS compatible retrieval, managing the on chain dataset lifecycle and continually proving storage.  It also provides first class handling of the FWSS PDP service/application including automatic settlement of user payments.

Curio implements these functions with a user facing http handler and a variety of PDP specific harmony tasks and database schemas.  Data storage and retrieval are implemented largely by relying on existing subsystems used for storage and retrieval of filecoin PoRep storage. 

# The HTTP Handler

TODO: Structural overview, https details, service auth, ping endpoint

## Managing Datasets
TODO: creating, deleting, info
TODO: the pdp datasets table etc
## Managing Pieces
TODO: add, upload, delete, pull, info
TODO: the pdp pieceref table etc
TODO: "finalization" notify task

# Storage

## Park Pieces 

## TODO: probably a few more categories to include. There's a lot more to learn about storage 

# Retrieval

## Indexing

## IPNI

## TODO: probably more categories here too

# Chain Interaction

## SenderETH and message_waits_eth

## PDP Contract Bindings
TODO: PDPVerifier, PDPProvingSchedule, ListenerService
## FWSS Contract Bindings
TODO: Registry, WarmStorageService

# Proving 
TODO: constructing and submitting proofs

# Proof Clock

The PDP protocol requires periodic proofs at a fixed frequency from a storage provider to demonstrate that data is provably stored.  To achieve this the PDPVerifier contract accepts proof messages on the `provePossession` endpoint.  The contract also needs input from the storage provider to roll the "proof clock" forward into the next proving period.  The PDPVerifier endpoint that achieves this is `nextProvingPeriod`.  Across the FWSS and PDPVerifier contracts `nextProvingPeriod` is responsible for completing piece deletions, registering faults and resetting the new challenge epoch used to generate the next challenge window's proofs.

Curio needs to manage this clock and update the proving period in a sensible way that synchronizes with proving.  It achieves this with state in the `pdp_data_sets` table and scheduling logic in three tasks: `PDPv0_InitPP` `PDPv0_Prove` and `PDPv0_ProvPeriod`.

The majority of the fields of the `pdp_data_sets` table are relevant to understand the proving clock:

```sql 
CREATE TABLE pdp_data_sets (
    ...
    challenge_request_task_id BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,
    challenge_request_msg_hash TEXT,
    proving_period BIGINT,
    challenge_window BIGINT,
    prove_at_epoch BIGINT,
    init_ready BOOLEAN NOT NULL DEFAULT FALSE,
    unrecoverable_proving_failure_epoch BIGINT,
    next_prove_attempt_at BIGINT,
    consecutive_prove_failures INT NOT NULL DEFAULT 0,
    ...
);
```

## Proving Task

The `ProveTask` (named `PDPv0_Prove` in the database) executes the actual merkle inclusion proof generation and submission to the `provePossession` endpoint of the PDPVerifier.  They are scheduled as soon as two conditions are met: the dataset's latest `challenge_request_msg_hash` is confirmed as successful in `message_waits_eth` and the `prove_at_epoch` is in the past.  These conditions indicate that the PDPVerifier is ready for a new proof.

There is one subtle issue with this scheduling.  It may be the case that the proving task is backed up in the curio task scheduling queue either because the node was shut down and restarted or because other tasks are overloading the system.  Proving tasks can be scheduled, wake up, and actually be too late to execute in time.  To handle this case gracefully, immediately upon executing within their `Do()` function, prove tasks check that the chain is currently within an active challenge window and if not gracefully stop work.


## Next Proving Period Task

The `NextProvingPeriodTask` (named `PDPv0_ProvPeriod` in the database) is triggered for scheduling as soon as a proving challenge window is complete.  In particular these tasks wait for dataset table entries to satisfy the condition that `prove_at_epoch + challenge_window` is in the past.  

Whether or not the proof has been submitted or is still in the process of being submitted is irrelevant to these tasks.  They greedily reschedule the next periods proving window as soon as there is no benefit to wait any longer.  Note that proving and next proving period tasks do not share any task id locking logic and run independently.

After a data set is picked up for next proving period scheduling the `challenge_request_task_id` field is then assigned to this task in the usual atomic fashion to lock the dataset to this harmony task.  The task then calls the `nextProvingPeriod` method on the PDPVerifier contract.

One important value must be determined to be passed as an argument to `nextProvingPeriod` -- the `challengeEpoch`.  This is the epoch at which the next proof can be generated.  Processing `nextProvingPeriod` sets this value in the state of `PDPVerifier` effectively ticking the clock forward.  The challenge epoch is computed by referencing the service contract's (FWSS) proving schedule abstraction (its implementation of `PDPVerifier`'s `IPDPProvingSchedule` interface) which includes a `nextPDPChallengeWindowStart(id)` method.  This value is both sent to the chain via `nextProvingPeriod` AND stored in the dataset's table entry as the `prove_at_epoch`.  The `prove_at_epoch` update is stored atomically alongside the insertion of the `challenge_request_msg_hash`, i.e. the tx hash of the newly sent `nextProvingPeriod` message, into the `message_waits_eth` table.  This completes the circle setting up the condition for proving tasks to wait on a dataset only in the case where `nextProvingPeriod` has been called and confirmed and updated the challenge epoch on chain to match the local `prove_at_epoch` state.

## Initial Proving Period Task

From the PDPVerifier's perspective when PDP datasets are initialized they are not immediately registered as ready for proving.  The storage provider must make an initial call into the `nextProvingPeriod` method.  Curio handles this special case with `InitProvingPeriodTask` (named `PDPv0_InitPP` in the database).  

Curio waits for the `init_ready` flag to be set for a data set table entry.  This is triggered via the `DataSetWatch` chain scheduler callback when the first piece is added to the dataset.  Then the task executes essentially the same logic as that described above in the _Next Proving Period Task_ section.  The only significant difference is the init task's reference to the `initChallengeWindowStart` parameter in service contract's `getPDPConfig()` return type (as per `PDPVerifier`'s `IPDPProvingSchedule` interface).

## Retries and unrecoverable errors 

Curio has some robustness against failures of `provePossession` and `nextProvingPeriod` calls.  The strategy is to retry with exponential backoff any failures in these "proving clock methods" until a certain threshold of failure is reached.  This threshold is reached after `MaxConsecutiveFailures = 5` retries.  The pdp_data_set table counts successive backoff attempts with the consecutive_prove_failures variable.  When this value is too high and a failure of one of the proving tasks occurs the data set is marked as unrecoverably failed and scheduled for deletion.  

To ensure that this retry behavior is achieved scheduling of the three tasks under discussion involves checking the associated retry state for data set table entries.  In particular all three tasks ensure that the `unrecoverable_proving_failure_epoch` is unset and the `next_prove_attempt_at` is either NULL or in the past in addition to the other scheduling conditions already discussed.


# Dataset Termination

## FWS Termination

## TODO: more categories

# Payment Settlement

# PDP Harmony Tasks

All PDP-related functionality is implemented as harmony tasks and chain-handler watchers. Tasks implement the `TaskInterface` and are scheduled by the harmony task engine. Chain-handler watchers are registered via `chainsched.AddHandler` and fire on every new chain head.

### Scheduling Mechanisms

Harmony tasks are created through three trigger mechanisms:

- **Chain handlers** — Registered via `chainsched.AddHandler`, these callbacks fire on every chain head change. They inspect on-chain state (e.g. transaction receipts, epoch thresholds) and call `AddTask` to insert work into the harmony_task queue when conditions are met. The proving-cycle tasks (InitPP, ProvPeriod, Prove) use this mechanism so they respond immediately to new tipsets.
- **IAmBored** — An optional callback in `TaskTypeDetails` that the task engine invokes when a machine has spare capacity and no queued work exists for that task type. A `passcall.Every(duration, ...)` wrapper rate-limits invocations. Tasks like TerminateFWSS (1 min), DeleteDataSet (1 hour), and Settle (12 hours) use IAmBored because they generate work opportunistically rather than in response to chain events.
- **Polling** — Some tasks use a dedicated poller goroutine that periodically queries the database for pending work and calls `AddTask`. The Notify (2s) and PullPiece (10s) tasks use this pattern because their triggers are purely database-driven (new uploads or pull requests) with no chain dependency.

All three mechanisms funnel through `harmonytask.AddTask()`, which atomically inserts a task record. The main poller loop (every 3s) then discovers unowned tasks and assigns them to machines with available resources.

## Registered Tasks

| Task Name | Struct | File | Trigger |
|-----------|--------|------|---------|
| `PDPv0_InitPP` | `InitProvingPeriodTask` | `tasks/pdp/task_init_pp.go` | Chain handler |
| `PDPv0_ProvPeriod` | `NextProvingPeriodTask` | `tasks/pdp/task_next_pp.go` | Chain handler |
| `PDPv0_Prove` | `ProveTask` | `tasks/pdp/task_prove.go` | Chain handler |
| `PDPv0_Notify` | `PDPNotifyTask` | `tasks/pdp/notify_task.go` | Polling (2s) |
| `PDPv0_PullPiece` | `PDPPullPieceTask` | `tasks/pdp/task_pull_piece.go` | Polling (10s) |
| `PDPv0_Indexing` | `PDPIndexingTask` | `tasks/indexing/task_pdp_indexing.go` | IAmBored (3s) |
| `PDPv0_IPNI` | `PDPIPNITask` | `tasks/indexing/task_pdp_ipni.go` | IAmBored (30s) |
| `PDPv0_TermFWSS` | `TerminateFWSSTask` | `tasks/pdp/task_terminate_fwss.go` | IAmBored (1 min) |
| `PDPv0_DelDataSet` | `DeleteDataSetTask` | `tasks/pdp/task_delete_data_set.go` | IAmBored (1 hour) |
| `Settle` | `SettleTask` | `tasks/pay/settle_task.go` | IAmBored (12 hours) |

## Chain-Handler Watchers

These are not harmony tasks but chain-event handlers registered via `chainsched.AddHandler`. They fire on every new chain head and process pending on-chain transactions.

| Watcher | File | Description |
|---------|------|-------------|
| `DataSetWatch` | `tasks/pdp/dataset_watch.go` | Runs `processPendingDataSetCreates` then `processPendingDataSetPieceAdds` sequentially. Extracts `DataSetCreated` and `PiecesAdded` events from transaction receipts. Sets `init_ready=TRUE` on first piece addition. |
| `TerminateServiceWatcher` | `tasks/pdp/watch_fwss_terminate.go` | Monitors `terminateService` transaction completion. Retrieves `PdpEndEpoch` from the FWSS contract and updates `service_termination_epoch` in `pdp_delete_data_set`. |
| `DataSetDeleteWatcher` | `tasks/pdp/watch_data_set_delete.go` | Monitors `deleteDataSet` transaction completion. Verifies dataset is no longer live on-chain, then deletes the `pdp_data_sets` row (CASCADE deletes pieces and prove tasks). |
| `PieceDeleteWatcher` | `tasks/pdp/watch_piece_delete.go` | Monitors piece removal transaction completion. Marks pieces as `removed=TRUE`, cleans up `pdp_piecerefs` after a 24-hour grace period, and publishes IPNI removal advertisements. |
| `SettleWatcher` | `tasks/pay/watcher.go` | Monitors settlement transaction completion. Verifies settlement status on-chain via the FWSS contract. Detects terminated or defaulting rails and triggers service termination and dataset deletion via `pdp.EnsureServiceTermination` and `ensureDataSetDeletion`. |

# Task Dependency Tree

The sections below describe how tasks and watchers connect to form the PDP lifecycle. Each step produces a database state or on-chain event that the next step consumes.

## Piece Ingestion

There are two paths for getting pieces into the system:

**Direct upload path:**
1. Client uploads piece data via HTTP. The data is written to `parked_pieces`.
2. When the upload completes (`parked_pieces.complete = TRUE`), **PDPv0_Notify** picks it up, sends an HTTP callback to `notify_url` if configured, and moves the reference from `pdp_piece_uploads` to `pdp_piecerefs`.

**Pull path:**
1. Client submits a pull request via HTTP, creating a row in `pdp_piece_pull_items`.
2. **PDPv0_PullPiece** downloads the piece from the external URL, computes and verifies CommP, and stores the result in `parked_pieces` + `pdp_piecerefs`.

**Indexing and IPNI advertisement (both paths):**
1. Once a piece is in `pdp_piecerefs` with `needs_indexing = TRUE`, **PDPv0_Indexing** reads the piece data as a CAR, builds a block-level index in the index store, and sets `needs_ipni = TRUE`.
2. **PDPv0_IPNI** then creates an IPNI advertisement (signed ad chain entry with chunked multihash blocks) and publishes it so the piece is discoverable via IPNI.

## Dataset Creation and Proving Cycle

1. Client calls `createDataSet()` via HTTP. This sends an on-chain transaction and records the hash in `pdp_data_set_creates`.
2. When the transaction lands, **DataSetWatch** extracts the `DataSetCreated` event from the receipt, records the `dataSetId`, then processes any pending piece additions and sets `init_ready = TRUE`.
3. **PDPv0_InitPP** picks up datasets where `init_ready = TRUE` and `prove_at_epoch IS NULL`. It calls `PDPVerifier.nextProvingPeriod()` and records the resulting `prove_at_epoch` and `challenge_request_msg_hash`.
4. Once the challenge request transaction lands and `prove_at_epoch` is reached, **PDPv0_Prove** builds SHA-256 Merkle tree proofs and submits them via `PDPVerifier.provePossession()`. It then resets `challenge_request_msg_hash` to NULL.
5. After the challenge window closes (`prove_at_epoch + challenge_window <= current_height`), **PDPv0_ProvPeriod** calls `PDPVerifier.nextProvingPeriod()` to start the next cycle, setting new `prove_at_epoch` and `challenge_request_msg_hash` values.
6. Steps 4-5 repeat indefinitely for each dataset.

## Payment Settlement and Termination

1. **Settle** periodically calls `filecoinpayment.SettleLockupPeriod()` and writes the transaction to `filecoin_payment_transactions` + `message_waits_eth`.
2. When the transaction lands, **SettleWatcher** verifies settlement on-chain and inspects each rail's status.

**If a rail is terminated or near default:**
3. SettleWatcher calls `pdp.EnsureServiceTermination()`, which creates a `pdp_delete_data_set` row.
4. **PDPv0_TermFWSS** picks it up, calls `FWSS.terminateService()`.
5. When that transaction lands, **TerminateServiceWatcher** retrieves `PdpEndEpoch` from the FWSS contract and sets `service_termination_epoch`.
6. Once `service_termination_epoch <= current_block` and `deletion_allowed = TRUE`, **PDPv0_DelDataSet** calls `PDPVerifier.deleteDataSet()`.
7. When the delete transaction lands, **DataSetDeleteWatcher** verifies the dataset is no longer live on-chain and deletes the `pdp_data_sets` row (CASCADE removes pieces and prove tasks).

## Piece Removal

1. Client calls `removePieces()` via HTTP, which sends a `PDPVerifier.removePieces()` transaction.
2. When the transaction lands, **PieceDeleteWatcher** marks the pieces as `removed = TRUE` in `pdp_data_set_pieces`.
3. After a 24-hour grace period, the watcher cleans up `pdp_piecerefs` and `parked_piece_refs`, and publishes IPNI removal advertisements.

## Error Recovery

**PDPv0_Prove failures:**
- **Transient errors** — Harmony retries the task automatically (up to 5 attempts).
- **Contract reverts** — Exponential backoff: 100 blocks * 2^(failures-1), capped at 28800 blocks. After 5 consecutive failures the error is marked unrecoverable.
- **Unrecoverable errors** — Sets `unrecoverable_proving_failure_epoch` and the dataset stops proving.

**PDPv0_ProvPeriod recovery:**
- If `ProvingPeriodNotInitialized` is detected, the dataset is reset to init state (`init_ready = TRUE`, `prove_at_epoch = NULL`) and picked up again by **PDPv0_InitPP**.
