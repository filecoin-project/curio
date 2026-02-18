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

## Initial Proving Period Task

## Next Proving Period Task

## Proving Task
TODO: focus on transitions between this and next proving period not the proving logic

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

**If a rail is fully settled/finalized:**
3. SettleWatcher calls `ensureDataSetDeletion()`, which sets `deletion_allowed = TRUE` in `pdp_delete_data_set`. This is then picked up by **PDPv0_DelDataSet** at step 6 above.

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
