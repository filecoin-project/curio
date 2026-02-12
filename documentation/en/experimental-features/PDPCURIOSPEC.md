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
    -- task invoking nextProvingPeriod, the task should be spawned any time prove_at_epoch+challenge_window is in the past
    challenge_request_task_id BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,

    -- nextProvingPeriod message hash, when the message lands prove_task_id will be spawned and
    -- this value will be set to NULL
    challenge_request_msg_hash TEXT,

    -- the proving period for this proofset and the challenge window duration
    proving_period BIGINT,
    challenge_window BIGINT,

    -- the epoch at which the next challenge window starts and proofs can be submitted
    -- initialized to NULL indicating a special proving period init task handles challenge generation
    prove_at_epoch BIGINT,

    -- flag indicating that the proving period is ready for init.  Currently set after first add
    -- Set to true after first root add
    init_ready BOOLEAN NOT NULL DEFAULT FALSE,

    
...
);
```

## Proving Task

`PDPv0_Prove` tasks execute the actual merkle inclusion proof generatinon and submission to the `provePossession` endpoint of the PDPVerifier.  They are scheduled as soon as two conditions are met: the dataset's latest `challenge_request_msg_hash` is confirmed as successful in `message_waits_eth` and the `prove_at_epoch` is in the past.  These conditions indicate that the PDPVerifier is ready for a new proof.

There is one subtle issue with this scheduling.  It may be the case that the proving task is backed up in the curio task scheduling queue either because the node was shut down and restarted or because other tasks are overloading the system.  Proving tasks can be scheduled, wake up, and actually be too late to execute in time.  To handle this case gracefully, immediately upon executing within their `Do()` function, prove tasks check that the chain is currently within an active challenge window and if not gracefully stop work.


## Next Proving Period Task

Next proving period or `PDPv0_Prove` tasks are triggerd for scheduling as soon as a proving challenge window is complete.  In particular these tasks wait for dataset table entries to satisfy the condition that `prove_at_epoch + challenge_window` is in the past.  

Whether or not the proof has been submitted or is still in the process of being submitted is irrelevant to these tasks.  They greedily reschedule the next periods proving window as soon as there is no benefit to wait any longer.  Note that proving and next proving period tasks do not share any task id locking logic and run independently.

After a data set is picked up for next proving period scheduling the `challenge_request_task_id` field is then assigned to this task in the usual atomic fashion to lock the dataset to this harmony task.  The task then calls the `nextProvingPeriod` method on the PDPVerifier contract.

One important value must be determined to be passed as an argument to `nextProvingPeriod` -- the `challengeEpoch`.  This is the epoch at which the next proof can be generated.  Processing `nextProvingPeriod` sets this value in the state of PDPVerifier effectively ticking the clock forward.  The challenge epoch is computed by referencing the FWSS contract's proving schedule abstraction (`PDPConfig`) which includes a proving period and challenge window constant.  This value is both sent to the chain via `nextProvingPeriod` AND stored in the dataset's table entry as the `prove_at_epoch`.  The `prove_at_epoch` update is stored atomically alongside the insertion of the `challenge_request_msg_hash`, i.e. the tx hash of the newly sent `nextProvingPeriod` message, into the `message_waits_eth` table.  This completes the circle setting up the condition for proving tasks to wait on a dataset only in the case where `nextProvingPeriod` has been called and confirmed and updated the challenge epoch on chain to match the local `prove_at_epoch` state.

## Initial Proving Period Task

From the PDPVerifier's perspective when PDP datasets are initialized they are not immediately registered as ready for proving.  The storage provider must make an initial into the `nextProvingPeriod` method.  Curio handles this special case with a specific `PDPv0_InitPP` task.  

Curio waits for the `init_ready` flag to be set for a data set table entry.  This is triggered via the `DataSetWatch` chain scheduler callback when the first piece is added to the dataset.  Then the task executes essentially the same logic as that described above in the `Next Proving Period Task` section.  The only significant difference is the init task's reference to the `InitChallengeWindowStart` parameter in the `PDPConfig`.


## Retries and unrecoverable errors 



# Dataset Termination

## FWS Termination

## TODO: more categories

# Payment Settlement
