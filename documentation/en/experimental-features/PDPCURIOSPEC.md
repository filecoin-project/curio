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

Curio provides automated settlement of payments made to a storage providers' addresses on the filecoin-pay payment system.  It accomplishes this through the coordinated efforts of two components: the `Settle` task and the `SettleWatcher` curio chain schedule callback.

Because the full settlement of payments is an important precondition for the full termination of an FWSS managed PDP data set this subsystem interacts closely with the PDP data set termination pipeline.

## Settle Task 

The settle task is scheduled twice a day through the IAmBored entrypoint.  Upon waking up the settle task queries the `eth_keys` table for the first keys it finds with role == `pdp`.  The task then queries the provider registry to learn the PDP service address which it this key is providing for.  In all cases today this should be the FWSS contract address on whichever filecoin network (main/calib) this curio node is running on.  `Settle` task then delegates to the `filecoinpayment` library method `SettleLockupPeriod` to attempt to settle all rails in need of settlement that are paying out to the provider's key and using the appropriate FWSS contract as the rail's operator contract.

`SettleLockupPeriod` uses eth call methods on the filecoin-pay contract to lookup all rails operator and payee.  The method's intention is to settle rails that have any possibility of client default between this run of the task and the next expected run in 12 hours.  To determine this condition each rail's `settledUpTo` and `lockupPeriod` value is inspected.  When a rail has not been settled for over one `lockupPeriod` the client can be in default.  `SettleLockuPerod` settles all rails that are within one day of meeting this condition.  Additionally all termianted rails in the process of finishing out their last `lockupPeriod` of life are marked for settlement.

Using `multicall.Multicall3Call` the task settles rails in batches of 10 and atomically adds the tx hashes to `message_waits_eth` and the settlement tracking table `filecoin_payment_transactions`. 

