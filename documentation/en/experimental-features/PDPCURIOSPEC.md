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
