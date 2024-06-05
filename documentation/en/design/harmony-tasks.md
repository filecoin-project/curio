---
description: This guide explains the different HarmonyTasks available in Curio
---

# Harmony Tasks

Curio uses HarmonyTask as a generic task container which can be scheduled by the poller in regular basis for the execution. To perform the different aspects of sealing and proving, Curio implements the following task types.

### SDR&#x20;

The SDR task is the first phase of the Proof-of-Replication process and is where encoding and replication of the data takes place. The SDR task is predominantly using a single CPU core, and is heavily utilizing the SHA256 instruction set. Using a CPU that has the SHA256 instruction set is therefore recommended. All 11 layers of calculation, layer by layer, are calculated sequentially. Each layer is 32GiB in size. When the SDR process is finished you will have generated data to the amount of 384GiB (A 32GiB unsealed sector + (11 layers x 32GiB)). SDR task requires commD as one of the input parameters. The commD calculation requires piece size and CID for all the pieces that will be part of the sector. The pieces themselves (data) is not required at this stage of the pipeline.

### SDRTrees&#x20;

The SDRTrees task can be further divided into 3 parts which are completed sequentially.

#### TreeD&#x20;

Building the TreeD requires the access to the data to be sealed into the sector. It builds a Merkle tree using the data and writes it to the specified output path ending with “tree-d.dat”. It also returns the root CID of the generated tree.

#### TreeRC&#x20;

In the TreeRC task, a column hash computation based on the 11 layers generated in PreCommit 1 is calculated, and a merkle tree gets constructed. This is same as PreCommit 2 on Lotus-miner. These tasks generates unsealed CID and sealed CID. The unsealed CID should match the root CID of the TreeD output. During this phase an additional 64GiB file (32GiB sectors) that represents the merkle tree is stored, in addition to the sealed 32GiB sector. Bringing the total amount of storage needed to approximately 500 GiB for one sector.

### PreCommitSubmit&#x20;

Through the `PreCommitSector` message a storage provider submits a deposit for a given sector’s sealed data, often referred to as the SealedCID, or commitment to replica (commR). After the message is included on-chain, the sector is registered to the storage provider and the sector enters the WaitSeed state, which is a security wait requirement by the network. This message type can also be batched to include multiple PreCommitSector messages in a single message to save gas fees paid to the network. These batched messages are called `PreCommitSectorBatch`. The message is not sent by the PreCommitSubmit task itself but is handed over to the queue of `SendMessage` task.

### PoRep&#x20;

The PoRep task combines the Commit1 and Commit2 parts of the Lotus-Miner sealing pipeline.

The randomness acquired at the end of the wait seed state is used in the Commit 1 phase to select a random subset of leaf nodes from the merkle tree generated in the PreCommit 2 phase. From the subset of leaf nodes it checks, it generates a much smaller file than the full merkle tree. That file is approximately 16MiB in size.

In the Commit 2 phase, the file from the Commit 1 gets compressed into a much smaller proof using zk-SNARKs. The proof generated at the end of Commit 2 can be verified that is correct very fast, and is small enough to be suitable for a blockchain. The final size of the proof is approximately 2kib, and gets published on the blockchain.

### Finalize&#x20;

The Finalize task performs the following operations:

1. It truncates the output of TreeD file to the sector size and them moves it to the unsealed file location of the sector. User should not that unsealed sector copy will not exist till point in the sealing pipeline. The unsealed copy is created on if “KeepUnsealed” is true for the deal.
2. The cache files for the sector are cleaned up at this stage.
3. Delete the local copy of the pieces that have been added to the sector.

### MoveStorage&#x20;

The MoveStorage task moves the data from sealing storage to permanent storage.

### CommitSubmit&#x20;

In CommitSubmit task, we create the `ProveCommitSector` message for the sector and hand it over to the queue of `SendMessage` task. Through the `ProveCommitSector` message the storage provider provides a Proof of Replication (PoRep) for the sector committed in the `PreCommitSector` message. This proof must be submitted AFTER the security wait requirement by the network (WaitSeed), and before the PreCommit expiration of the sector. This message type can also be aggregated to include multiple ProveCommitSector messages in a single message. These aggregated messages are called `ProveCommitAggregate`.

### WindowPost&#x20;

The WindowPost allow storage providers to verifiably prove they have the data they have committed to the network on disk to create a verifiable, and public record attesting to the storage providers continued commitment of storing the data, or for the network to reward storage providers for their contributions. The overall WindowPost process has been broken into 3 independent tasks in Curio. Each of these tasks are triggered by the `CurioChainScheduler` when the TipSet changes.

#### WdPost&#x20;

WindowPost task is responsible for generating the proof for an individual partition in the current deadline. Curio runs multiple such tasks in parallel to speed up the calculation time for each deadline.

#### WdPostRecover&#x20;

The WdPostRecover task is also executed on per partition basis for each deadline. We check all the previously faulty sectors and determine which sectors have now recovered since then. it creates the recovery message for each partition in the current deadline and submits these messages to the queue of `SendMessage` task.

#### WdPostSubmit&#x20;

WdPostSubmit creates the WindowPost messages for each partition in the current deadline and submits these messages to the queue of `SendMessage` task.

### WinPost&#x20;

Winning Proof-of-SpaceTime (WinningPoSt) is the mechanism by which storage providers are rewarded by the Filecoin network for their contributions to it. As a requirement for doing so, each storage provider is tasked with submitting a compressed Proof-of-Spacetime for a specified sector. Each elected storage provider who successfully creates a block is granted FIL, as well as the opportunity to charge other Filecoin participants fees to include messages in the block. Storage providers who fail to do this in the necessary window will forfeit their opportunity to mine a block. The WinPost task is triggered on each epoch change and if Miner address wins the election then a new block is created and submitted to the chain.

### SendMessage&#x20;

The SendMessage task implements a message queue where message can be added by any other task. These messages are then processed by the `SendMessage` and processed individually. It abstracts away highly-available message sending with coordination through HarmonyDB. It makes sure that Nonce are assigned in transactional manner, and that messages are correctly broadcast to the network. It ensures that messages are sent serially, and that failures to send doesn’t cause a nonce gap.

### ParkPiece&#x20;

Curio has implemented a new file location within the storage subsystem called “piece”. This directory is used to temporarily park the pieces while they are being sealed. The `parked_pieces` also contains the URL and headers to download the data. The ParkPiece task scans the `parked_pieces` table in HarmonyDB every 15 seconds. If any pieces are found, a corresponding file is created in under “piece” directory of the storage and data is downloaded to the file from the URL. When `SectorAddPieceToAny` method is called by an external market node, it creates a ParkPiece tasks.

### DropPiece&#x20;

The DropPiece tasks are responsible for removing a piece from `Piece Park` and ensuring all the files and reference related to the piece are cleaned up. This task is triggered by the Finalize task of a sector is sealing pipeline.

### Resource requirements for each Task type in Curio&#x20;

By default, the number of tasks allowed for each type are not limited on any Curio node. The distributed scheduler ensures that no Curio node over-commits the resources.

| Task Name       | CPU | RAM(GiB) | GPU | Retry |
| --------------- | --- | -------- | --- | ----- |
| SDR             | 4   | 54       | 0   | 2     |
| SDRTrees        | 1   | 8        | 1   | 3     |
| PreCommitSubmit | 0   | 1        | 0   | 16    |
| PoRep           | 1   | 50       | 1   | 5     |
| Finalize        | 1   | 0.1      | 0   | 10    |
| MoveStorage     | 1   | 0.128    | 0   | 10    |
| CommitSubmit    | 0   | 0.001    | 0   | 16    |
| WdPostSubmit    | 0   | 0.010    | 0   | 10    |
| WdPostRecover   | 1   | 0.128    | 0   | 10    |
| WdPost          | 1   | TBD      | TBD | 3     |
| WinPost         | 1   | TBD      | TBD | 3     |
| SendMessage     | 0   | 0.001    | 0   | 1000  |

\
