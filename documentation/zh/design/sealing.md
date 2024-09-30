---
description: 本页解释了Curio中密封管道的功能
---

# Sealing
# 密封

## Sealing Pipeline
## 密封管道

Curio的密封过程由HarmonyTasks驱动。密封扇区涉及的每个阶段都被分为更小的独立任务。这些单独的任务然后由Curio集群中的不同机器接管。这确保了任务在整个系统中有效分配，资源得到高效利用。

<figure><img src="../.gitbook/assets/curio-sealing.png" alt="Curio密封管道概览"><figcaption><p>Curio密封管道</p></figcaption></figure>

## SealPoller
## 密封轮询器

SealPoller结构设计用于跟踪密封操作的进度。密封工作流中的每个可能状态都由一个pollTask结构表示。这个结构通过在harmony数据库的`sectors_sdr_pipeline`表的单独列中设置布尔标志和保存任务ID来跟踪密封操作可能处于的每个步骤。

```go
type pollTask struct {
	SpID         int64 `db:"sp_id"`
	SectorNumber int64 `db:"sector_number"`

	TaskSDR  *int64 `db:"task_id_sdr"`
	AfterSDR bool   `db:"after_sdr"`

	TaskTreeD  *int64 `db:"task_id_tree_d"`
	AfterTreeD bool   `db:"after_tree_d"`

	TaskTreeC  *int64 `db:"task_id_tree_c"`
	AfterTreeC bool   `db:"after_tree_c"`

	TaskTreeR  *int64 `db:"task_id_tree_r"`
	AfterTreeR bool   `db:"after_tree_r"`

	TaskPrecommitMsg  *int64 `db:"task_id_precommit_msg"`
	AfterPrecommitMsg bool   `db:"after_precommit_msg"`

	AfterPrecommitMsgSuccess bool   `db:"after_precommit_msg_success"`
	SeedEpoch                *int64 `db:"seed_epoch"`

	TaskPoRep  *int64 `db:"task_id_porep"`
	PoRepProof []byte `db:"porep_proof"`
	AfterPoRep bool   `db:"after_porep"`

	TaskFinalize  *int64 `db:"task_id_finalize"`
	AfterFinalize bool   `db:"after_finalize"`

	TaskMoveStorage  *int64 `db:"task_id_move_storage"`
	AfterMoveStorage bool   `db:"after_move_storage"`

	TaskCommitMsg  *int64 `db:"task_id_commit_msg"`
	AfterCommitMsg bool   `db:"after_commit_msg"`

	AfterCommitMsgSuccess bool `db:"after_commit_msg_success"`

	Failed       bool   `db:"failed"`
	FailedReason string `db:"failed_reason"`
}
```

SealPoller从数据库中检索所有`after_commit_msg_success`或`after_move_storage`不为真的`pollTasks`，并尝试在可能的情况下推进它们的状态。当一个`pollTask`的依赖项（由"after_"字段指示）完成，且任务本身尚未排队（其任务ID为nil）或完成（其"After"字段为false）时，就会推进该任务。每个pollTask的推进都会触发一个数据库事务，尝试用从HarmonyDB接收到的新任务ID更新任务ID。该事务确保在读取状态和更新任务ID之间，任务没有被其他人排队。这个轮询过程按顺序进行，每个阶段都有不同的条件，确保在继续之前满足所有先前的条件。如果一个任务由于其先前的依赖项未完成而无法继续，轮询器将在下一轮回来。大多数情况下，轮询器操作期间发生的错误会被记录，不会导致轮询器停止。但如果在数据库事务期间发生严重问题，它将被回滚，并给出详细的错误消息。通过这种方式组织工作，SealPoller确保密封程序中的每个步骤按正确的顺序发生，并在可能的情况下取得进展。它允许在考虑其他正在进行的任务的约束条件下，尽可能高效地密封扇区。

<figure><img src="../.gitbook/assets/sealing-tasks.png" alt="密封任务执行"><figcaption><p>Curio harmony任务执行</p></figcaption></figure>

## Piece Park
## 数据块停放

传统上，数据需要在可以密封存储之前可用。然而，这可能导致效率低下。Curio通过引入"数据块停放"来解决这个问题。Curio的密封管道不要求数据一开始就随时可用。这允许我们在数据下载之前就开始密封过程。在密封过程进行时，数据被"停放"在存储位置内名为"piece"的指定目录中。这避免了长时间保持市场连接开放。本质上，本地数据块停放区作为数据的临时保存区，简化了密封过程并优化了资源使用。

Curio使用两个任务：
ParkPiece：这个任务处理数据的下载并将其放置在"piece"目录中。
DropPiece：一旦不再需要数据，这个任务负责清理停放的数据。

将来，这个本地存储还可以允许Curio在原始扇区在密封过程中丢失的情况下，在新的扇区中重新密封数据。

## LMRPCProvider
## LM远程过程调用提供者

LMRPCProvider提供了一组方法来与扇区和数据块相关的各种数据进行交互。这些方法是市场实现（Boost）所需要的，用于跟踪交易的密封进度。


ActorAddress：这个方法返回与LMRPCProvider相关的actor地址。换句话说，它返回矿工的地址。
WorkerJobs：这个函数返回一个以UUID为索引的工作任务映射。
SectorsStatus：这个方法根据给定的扇区标识符sid返回扇区的状态。这个函数包括有关扇区的详细信息，如密封状态、交易ID、日志、质押和到期时间等。
SectorsList：这个函数提供当前存储的扇区编号列表。
SectorsSummary：这个函数给出扇区的摘要，按其状态分类。它返回一个映射，将每个扇区状态映射到其数量。
SectorsListInStates：这个方法返回处于给定状态集合中的扇区编号列表。
ComputeDataCid：这个函数用于计算数据的CID。
AuthNew：这个函数为给定的权限创建一个新的授权令牌（JWT）。


## Piece Ingester
## 数据块接收器

数据块接收器为给定的矿工地址分配一个数据块到一个扇区。它检查数据块大小是否与扇区大小匹配，确定首选的密封证明类型，检索矿工ID，分配扇区编号，将数据块和扇区管道条目插入数据库，并返回分配的数据块的扇区和偏移量。
