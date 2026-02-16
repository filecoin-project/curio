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

## Registered Tasks

| Task Name | Struct | File | Trigger | Description |
|-----------|--------|------|---------|-------------|
| `PDPv0_InitPP` | `InitProvingPeriodTask` | `tasks/pdp/task_init_pp.go` | Chain handler | Initializes the first proving period for a dataset by calling `PDPVerifier.nextProvingPeriod()`. Picks up datasets where `init_ready=TRUE` and `prove_at_epoch IS NULL`. |
| `PDPv0_ProvPeriod` | `NextProvingPeriodTask` | `tasks/pdp/task_next_pp.go` | Chain handler | Schedules subsequent proving periods after each challenge window completes. Fires when `prove_at_epoch + challenge_window <= current_height`. Detects `ProvingPeriodNotInitialized` errors and resets dataset back to init state. |
| `PDPv0_Prove` | `ProveTask` | `tasks/pdp/task_prove.go` | Chain handler | Generates SHA-256 Merkle tree proofs and submits them via `PDPVerifier.provePossession()`. Scheduled when the `challenge_request_msg_hash` transaction lands on-chain and `prove_at_epoch` is reached. Most resource-intensive task (2GB RAM for memtree). |
| `PDPv0_Notify` | `PDPNotifyTask` | `tasks/pdp/notify_task.go` | Polling (2s) | Finalizes completed piece uploads by sending an optional HTTP callback to `notify_url` and moving the piece reference from temporary `pdp_piece_uploads` to permanent `pdp_piecerefs`. |
| `PDPv0_PullPiece` | `PDPPullPieceTask` | `tasks/pdp/task_pull_piece.go` | Polling (10s) | Downloads pieces from external URLs, streams data while computing CommP, verifies against expected piece CID, and stores in `parked_pieces` for long-term storage. |
| `PDPv0_TermFWSS` | `TerminateFWSSTask` | `tasks/pdp/task_terminate_fwss.go` | IAmBored (1 min) | Calls `FWSS.terminateService()` for datasets pending service termination. Retrieves `PdpEndEpoch` from the FWSS contract. |
| `PDPv0_DelDataSet` | `DeleteDataSetTask` | `tasks/pdp/task_delete_data_set.go` | IAmBored (1 hour) | Calls `PDPVerifier.deleteDataSet()` for datasets where `service_termination_epoch <= current_block` and `deletion_allowed=TRUE`. |
| `Settle` | `SettleTask` | `tasks/pay/settle_task.go` | IAmBored (12 hours) | Settles the FWSS lockup period for the PDP operator. Calls `filecoinpayment.SettleLockupPeriod()` with the operator and payee addresses. |

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

The following diagrams show how tasks and watchers connect to form the PDP lifecycle. Arrows represent causal dependencies: the source produces a database state or on-chain event that the target consumes.

## Piece Ingestion

```
HTTP Upload                        HTTP Pull Request
     │                                    │
     ▼                                    ▼
parked_pieces                     pdp_piece_pull_items
(upload in progress)                      │
     │                                    ▼
     │                            PDPv0_PullPiece (10s poll)
     │                            ── downloads piece
     │                            ── verifies CommP
     │                                    │
     │                                    ▼
     │                            parked_pieces + pdp_piecerefs
     │
     ▼
parked_pieces.complete=TRUE
     │
     ▼
PDPv0_Notify (2s poll)
── sends callback to notify_url
── moves pdp_piece_uploads → pdp_piecerefs
```

## Dataset Creation and Proving Cycle

```
HTTP: createDataSet()
     │
     ▼
pdp_data_set_creates (create_message_hash)
     │
     ▼  on-chain tx lands
DataSetWatch (chain handler)
── processPendingDataSetCreates: extracts dataSetId from receipt
── processPendingDataSetPieceAdds: extracts pieceIds, sets init_ready=TRUE
     │
     ▼  init_ready=TRUE, prove_at_epoch IS NULL
PDPv0_InitPP (chain handler)
── calls PDPVerifier.nextProvingPeriod()
── sets prove_at_epoch, challenge_request_msg_hash
     │
     ▼  challenge_request_msg_hash tx lands, prove_at_epoch reached
PDPv0_Prove (chain handler)
── builds Merkle tree proofs
── calls PDPVerifier.provePossession()
── resets challenge_request_msg_hash to NULL
     │
     ▼  prove_at_epoch + challenge_window <= current_height
PDPv0_ProvPeriod (chain handler)
── calls PDPVerifier.nextProvingPeriod()
── sets new prove_at_epoch, challenge_request_msg_hash
     │
     └──────► loops back to PDPv0_Prove
```

## Payment Settlement and Termination

```
Settle (every 12 hours)
── calls filecoinpayment.SettleLockupPeriod()
── writes tx to filecoin_payment_transactions + message_waits_eth
     │
     ▼  tx lands
SettleWatcher (chain handler)
── verifies settlement on-chain
── detects terminated/defaulting rails
     │
     ├──► pdp.EnsureServiceTermination()     (rail terminated or near default)
     │         │
     │         ▼  pdp_delete_data_set row created
     │    PDPv0_TermFWSS (every 1 min)
     │    ── calls FWSS.terminateService()
     │         │
     │         ▼  tx lands
     │    TerminateServiceWatcher (chain handler)
     │    ── retrieves PdpEndEpoch from FWSS
     │    ── sets service_termination_epoch
     │         │
     │         ▼  service_termination_epoch <= current_block, deletion_allowed=TRUE
     │    PDPv0_DelDataSet (every 1 hour)
     │    ── calls PDPVerifier.deleteDataSet()
     │         │
     │         ▼  tx lands
     │    DataSetDeleteWatcher (chain handler)
     │    ── verifies dataset no longer live
     │    ── deletes pdp_data_sets (CASCADE)
     │
     └──► ensureDataSetDeletion()             (rail fully settled/finalized)
               sets deletion_allowed=TRUE in pdp_delete_data_set
               picked up by PDPv0_DelDataSet above
```

## Piece Removal

```
HTTP: removePieces()
── sends PDPVerifier.removePieces() tx
     │
     ▼  tx lands
PieceDeleteWatcher (chain handler)
── marks pdp_data_set_pieces.removed=TRUE
── after 24h grace: cleans up pdp_piecerefs, parked_piece_refs
── publishes IPNI removal advertisements
```

## Error Recovery

```
PDPv0_Prove failure
     │
     ├── Transient error ──► harmony retry (up to 5 attempts)
     │
     ├── Contract revert ──► exponential backoff
     │   (100 blocks * 2^(failures-1), max 28800 blocks)
     │   after 5 consecutive failures ──► unrecoverable
     │
     └── Unrecoverable error ──► sets unrecoverable_proving_failure_epoch
                                  dataset stops proving

PDPv0_ProvPeriod detects ProvingPeriodNotInitialized
     └──► resets dataset to init state (init_ready=TRUE, prove_at_epoch=NULL)
          picked up by PDPv0_InitPP
```
