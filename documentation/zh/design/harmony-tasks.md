---
description: 本指南解释了Curio中可用的不同HarmonyTasks
---

# Harmony Tasks
# 和谐任务

Curio使用HarmonyTask作为通用任务容器，可以由轮询器定期安排执行。为了执行封装和证明的不同方面，Curio实现了以下任务类型。

### SDR
### SDR（空间数据复制）

SDR任务是复制证明过程的第一阶段，在这里进行数据的编码和复制。SDR任务主要使用单个CPU核心，并大量利用SHA256指令集。因此，建议使用具有SHA256指令集的CPU。所有11层计算按顺序逐层进行。每层大小为32GiB。当SDR过程完成时，您将生成384GiB的数据（32GiB未封装扇区 + (11层 x 32GiB)）。SDR任务需要commD作为输入参数之一。commD计算需要所有将成为扇区一部分的数据片段的大小和CID。在管道的这个阶段不需要数据片段本身（数据）。

### SDRTrees
### SDR树

SDRTrees任务可以进一步分为3个按顺序完成的部分。

#### TreeD
#### 树D

构建TreeD需要访问要封装到扇区中的数据。它使用数据构建一个Merkle树，并将其写入指定的输出路径，以"tree-d.dat"结尾。它还返回生成树的根CID。

#### TreeRC
#### 树RC

在TreeRC任务中，基于PreCommit 1中生成的11层计算列哈希，并构建Merkle树。这与Lotus-miner上的PreCommit 2相同。这些任务生成未封装CID和封装CID。未封装CID应与TreeD输出的根CID匹配。在此阶段，除了封装的32GiB扇区外，还存储了一个额外的64GiB文件（32GiB扇区）表示Merkle树。这使得一个扇区所需的总存储量达到约500 GiB。

### PreCommitSubmit
### 预提交提交

通过`PreCommitSector`消息，存储提供者为给定扇区的封装数据（通常称为SealedCID或复制承诺（commR））提交存款。消息被包含在链上后，扇区被注册到存储提供者，并进入WaitSeed状态，这是网络的安全等待要求。这种消息类型也可以批量处理，在一条消息中包含多个PreCommitSector消息，以节省支付给网络的gas费用。这些批量消息称为`PreCommitSectorBatch`。PreCommitSubmit任务本身不发送消息，而是将其交给`SendMessage`任务的队列。

### PoRep
### 复制证明

PoRep任务结合了Lotus-Miner封装管道的Commit1和Commit2部分。

在等待种子状态结束时获得的随机性用于Commit 1阶段，从PreCommit 2阶段生成的Merkle树中选择叶节点的随机子集。从它检查的叶节点子集中，它生成一个比完整Merkle树小得多的文件。该文件大小约为16MiB。

在Commit 2阶段，Commit 1的文件使用zk-SNARKs压缩成更小的证明。在Commit 2结束时生成的证明可以非常快速地验证其正确性，并且足够小，适合区块链。最终证明的大小约为2kib，并发布在区块链上。

### Finalize
### 完成

Finalize任务执行以下操作：

1. 它将TreeD文件的输出截断到扇区大小，然后将其移动到扇区的未封装文件位置。用户应注意，在封装管道的这一点之前不会存在未封装的扇区副本。只有当交易的"KeepUnsealed"为真时才会创建未封装的副本。
2. 在此阶段清理扇区的缓存文件。
3. 删除已添加到扇区的数据片段的本地副本。

### MoveStorage
### 移动存储

MoveStorage任务将数据从封装存储移动到永久存储。

### CommitSubmit
### 提交提交

在CommitSubmit任务中，我们为扇区创建`ProveCommitSector`消息，并将其交给`SendMessage`任务的队列。通过`ProveCommitSector`消息，存储提供者为在`PreCommitSector`消息中提交的扇区提供复制证明（PoRep）。这个证明必须在网络的安全等待要求（WaitSeed）之后，且在扇区的PreCommit过期之前提交。这种消息类型也可以聚合，在一条消息中包含多个ProveCommitSector消息。这些聚合消息称为`ProveCommitAggregate`。

### WindowPost
### 时空证明

WindowPost允许存储提供者可验证地证明他们已经将承诺给网络的数据存储在磁盘上，以创建一个可验证的、公开的记录，证明存储提供者持续承诺存储数据，或者让网络奖励存储提供者的贡献。在Curio中，整个WindowPost过程被分解为3个独立的任务。当TipSet变化时，这些任务由`CurioChainScheduler`触发。

#### WdPost
#### 时空证明

WindowPost任务负责为当前截止时间内的单个分区生成证明。Curio并行运行多个这样的任务，以加快每个截止时间的计算时间。

#### WdPostRecover
#### 时空证明恢复

WdPostRecover任务也是针对每个截止时间的每个分区执行的。我们检查所有先前故障的扇区，并确定哪些扇区现在已经恢复。它为当前截止时间内的每个分区创建恢复消息，并将这些消息提交到`SendMessage`任务的队列。

#### WdPostSubmit
#### 时空证明提交

WdPostSubmit为当前截止时间内的每个分区创建WindowPost消息，并将这些消息提交到`SendMessage`任务的队列。

### WinPost
### 赢得时空证明

赢得时空证明（WinningPoSt）是Filecoin网络奖励存储提供者对网络贡献的机制。作为这样做的要求，每个存储提供者都被要求为指定的扇区提交压缩的时空证明。每个成功创建区块的当选存储提供者都会获得FIL奖励，以及向其他Filecoin参与者收取费用以在区块中包含消息的机会。未能在必要的时间窗口内完成此操作的存储提供者将失去挖掘区块的机会。WinPost任务在每个纪元变化时触发，如果矿工地址赢得选举，则创建新区块并提交到链上。

### SendMessage
### 发送消息

SendMessage任务实现了一个消息队列，任何其他任务都可以向其添加消息。这些消息然后由`SendMessage`处理并单独处理。它通过HarmonyDB协调抽象了高可用性的消息发送。它确保以事务方式分配Nonce，并且消息正确广播到网络。它确保消息按顺序发送，并且发送失败不会导致nonce间隙。

### ParkPiece
### 停放数据片段

Curio在存储子系统中实现了一个新的文件位置，称为"piece"。这个目录用于在数据片段被封装时临时停放它们。`parked_pieces`还包含下载数据的URL和头信息。ParkPiece任务每15秒扫描一次HarmonyDB中的`parked_pieces`表。如果找到任何数据片段，则在存储的"piece"目录下创建相应的文件，并从URL下载数据到文件中。当外部市场节点调用`SectorAddPieceToAny`方法时，它会创建ParkPiece任务。

### DropPiece
### 删除数据片段

DropPiece任务负责从`Piece Park`中移除数据片段，并确保清理与该数据片段相关的所有文件和引用。此任务由扇区封装管道的Finalize任务触发。

### UpdateEncode
### 更新编码

SnapDeal封装任务是一种特殊类型的封装任务，允许存储提供者将已提交的封装扇区中放入交易数据。UpdateEncode任务将传入的未封装数据（交易数据）编码到现有的封装扇区中。一旦编码完成，生成并验证普通证明，以检查和确认数据已正确编码到封装扇区文件中。

### UpdateProve
### 更新证明

在UpdateProve阶段，UpdateEncode任务的输出使用zk-SNARKs压缩成更小的证明。UpdateProve后生成的zk-SNARK可以验证新数据是否编码在新的封装扇区中，并且足够小，适合区块链。zk-SNARK的生成可以由CPU完成，也可以使用GPU加速。

### Resource requirements for each Task type in Curio
### Curio中每种任务类型的资源需求

默认情况下，每种类型允许的任务数量在任何Curio节点上都没有限制。分布式调度器确保没有Curio节点过度承诺资源。

| 任务名称        | 任务描述                                   | CPU | RAM(GiB) | GPU | 重试次数 |
| --------------- | ------------------------------------------ | --- | -------- | --- | -------- |
| SDR             | 单数据复制                                 | 4   | 54       | 0   | 2        |
| SDRTrees        | 单数据复制树生成                           | 1   | 8        | 1   | 3        |
| PreCommitSubmit | 预提交提交                                 | 0   | 1        | 0   | 16       |
| PoRep           | 复制证明                                   | 1   | 50       | 1   | 5        |
| Finalize        | 完成封装                                   | 1   | 0.1      | 0   | 10       |
| MoveStorage     | 移动存储                                   | 1   | 0.128    | 0   | 10       |
| CommitSubmit    | 提交提交                                   | 0   | 0.001    | 0   | 16       |
| WdPostSubmit    | 窗口时空证明提交                           | 0   | 0.010    | 0   | 10       |
| WdPostRecover   | 窗口时空证明恢复                           | 1   | 0.128    | 0   | 10       |
| WdPost          | 窗口时空证明                               | 1   | 待定     | 待定 | 3        |
| WinPost         | 赢得时空证明                               | 1   | 待定     | 待定 | 3        |
| SendMessage     | 发送消息                                   | 0   | 0.001    | 0   | 1000     |
| UpdateEncode    | 更新编码                                   | 1   | 1        | 1   | 3        |
| UpdateProve     | 更新证明                                   | 1   | 50       | 1   | 3        |
