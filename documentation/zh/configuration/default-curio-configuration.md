---
description: The default curio configuration
---

# Default Curio Configuration

```toml
[Subsystems]
  # 启用窗口后证明在此 curio 实例上执行。集群中每台启用了窗口后证明的机器也将参与窗口后证明调度器。可以有多台启用了窗口后证明的机器，这将提供冗余，并且在每个截止日期有多个分区的情况下，将允许并行处理分区。
  # 
  # 可以有同时处理窗口后证明和获胜后证明的实例，这可以提供冗余而无需额外的机器。在这样的设置中，通常建议运行 partitionsPerDeadline+1 台机器。
  #
  # 类型：bool
  #EnableWindowPost = false

  # 类型：int
  #WindowPostMaxTasks = 0

  # 启用获胜后证明在此 curio 实例上执行。集群中每台启用了获胜后证明的机器也将参与获胜后证明调度器。
  # 可以混合使用启用了窗口后证明和获胜后证明的机器，详情请参阅 EnableWindowPost 文档。
  #
  # 类型：bool
  #EnableWinningPost = false

  # 类型：int
  #WinningPostMaxTasks = 0

  # 启用"数据块停放"任务在此节点上运行。此任务负责从网络获取数据块并将其存储在存储子系统中，直到扇区被密封。此任务
  # 仅适用于与 boost 集成时，并且应在将保存来自 boost 的交易数据的节点上启用，直到包含相关数据块的扇区构建了 TreeD/TreeR。
  # 请注意，未来的 Curio 实现将有一个单独的任务类型用于从互联网获取数据块。
  #
  # 类型：bool
  #EnableParkPiece = false

  # 类型：int
  #ParkPieceMaxTasks = 0

  # 启用 SDR 任务运行。SDR 是在扇区缓存目录中创建 11 个层文件的长序列计算。
  # 
  # SDR 是密封管道中的第一个任务。它的输入仅仅是未密封数据的哈希（CommD）、扇区编号、矿工 ID 和密封证明类型。
  # 它的输出是扇区缓存目录中的 11 个层文件。
  # 
  # 在 lotus-miner 中，这是作为 PreCommit1 的一部分运行的。
  #
  # 类型：bool
  #EnableSealSDR = false

  # 可以同时运行的 SDR 任务的最大数量。请注意，最大任务数量也将受到机器上可用资源的限制。
  #
  # 类型：int
  #SealSDRMaxTasks = 0

  # 系统开始接受新任务之前需要排队的 SDR 任务的最大数量。
  # 此设置的主要目的是允许积累足够的任务以进行批量密封。当集群中存在批量密封节点时，此值应设置为 batch_size+1，以允许批量密封节点填满批次。
  # 此设置还可以用于通过在应该具有较低优先级的节点上设置较高的值来给予集群中的其他节点优先级。
  #
  # 类型：int
  #SealSDRMinTasks = 0

  # 启用 SDR 管道树构建任务运行。
  # 此任务处理未密封数据编码到最后一个 SDR 层，并构建 TreeR、TreeC 和 TreeD。
  # 
  # 此任务在 SDR 之后运行
  # 首先计算 TreeD，可选输入未密封数据
  # TreeR 从副本计算，副本首先计算为最后一个 SDR 层和 TreeD 底层（即未密封数据）的字段加法
  # TreeC 从 11 个 SDR 层计算
  # 这 3 个树稍后将用于计算 PoRep 证明。
  # 
  # 在 SyntheticPoRep 的情况下，PoRep 的挑战将在此步骤预生成，树和层将被丢弃。SyntheticPoRep 通过预生成一个非常大的挑战集（磁盘上约 30GiB）
  # 然后使用其中的一小部分子集进行实际的 PoRep 计算。这允许在 PreCommit 和 PoRep 生成之间显著节省临时空间，代价是更多的计算（在此步骤生成挑战）
  # 
  # 在 lotus-miner 中，这是作为 PreCommit2 的一部分运行的（TreeD 在 PreCommit1 中运行）。
  # 请注意，启用了 SDRTrees 的节点也将响应 Finalize 任务，
  # 这只是在计算 PoRep 后删除不需要的树数据。
  #
  # 类型：bool
  #EnableSealSDRTrees = false

  # 可以同时运行的 SealSDRTrees 任务的最大数量。请注意，最大任务数量也将受到机器上可用资源的限制。
  #
  # 类型：int
  #SealSDRTreesMaxTasks = 0
  # FinalizeMaxTasks 是可以同时运行的最大完成任务数量。
  # 完成任务在所有处理 SDRTrees 任务的机器上都启用。完成任务始终在持有扇区缓存文件的机器上运行，因为它在计算 PoRep 后删除不需要的树数据。
  # 完成任务将与 SubmitCommitMsg 任务并行运行。
  #
  # 类型：int
  #FinalizeMaxTasks = 0

  # EnableSendPrecommitMsg 启用从此 curio 实例向链发送预提交消息。
  # 这在 SDRTrees 之后运行，并使用输出的 CommD / CommR（TreeD / TreeR 的根）作为消息内容
  #
  # 类型：bool
  #EnableSendPrecommitMsg = false

  # EnablePoRepProof 启用 porep 证明的计算
  # 
  # 此任务在交互式 porep 种子可用后运行，这发生在预提交消息上链后 150 个纪元（75 分钟）。此任务应在具有 GPU 的机器上运行。普通 PoRep 证明
  # 从持有扇区缓存文件的机器请求，该机器很可能是运行 SDRTrees 任务的机器。
  # 
  # 在 lotus-miner 中，这是 Commit1 / Commit2
  #
  # 类型：bool
  #EnablePoRepProof = false

  # 可以同时运行的 PoRepProof 任务的最大数量。请注意，最大任务数量也将受到机器上可用资源的限制。
  #
  # 类型：int
  #PoRepProofMaxTasks = 0

  # EnableSendCommitMsg 启用从此 curio 实例向链发送提交消息。
  #
  # 类型：bool
  #EnableSendCommitMsg = false

  # 是否在批处理中任何扇区激活失败时中止（仅适用于新密封的扇区，仅使用 ProveCommitSectors3）。
  #
  # 类型：bool
  #RequireActivationSuccess = true

  # 是否在批处理中任何扇区激活失败时中止（更新扇区，仅使用 ProveReplicaUpdates3）。
  #
  # 类型：bool
  #RequireNotificationSuccess = true

  # EnableMoveStorage 启用在此 curio 实例上运行移动到长期存储的任务。
  # 此任务应仅在具有长期存储的节点上启用。
  # 
  # MoveStorage 任务是密封管道中的最后一个任务。它将密封的扇区数据从 SDRTrees 机器移动到长期存储中。此任务在完成任务之后运行。
  #
  # 类型：bool
  #EnableMoveStorage = false

  # 可以同时运行的 MoveStorage 任务的最大数量。请注意，最大任务数量也将受到机器上可用资源的限制。建议将此值设置为一个能够充分利用机器上所有可用网络（或磁盘）带宽而不造成瓶颈的数字。
  #
  # 类型：int
  #MoveStorageMaxTasks = 0

  # EnableUpdateEncode 在此 curio 实例上启用 SnapDeal 过程的编码步骤。
  # 此步骤涉及将数据编码到扇区中并计算更新的 TreeR（使用 gpu）。
  #
  # 类型：bool
  #EnableUpdateEncode = false

  # EnableUpdateProve 在此 curio 实例上启用 SnapDeal 过程的证明步骤。
  # 此步骤为更新的扇区生成 snark 证明。
  #
  # 类型：bool
  #EnableUpdateProve = false

  # EnableUpdateSubmit 启用从此 curio 实例向区块链提交 SnapDeal 证明。
  # 此步骤将生成的证明提交到链上。
  #
  # 类型：bool
  #EnableUpdateSubmit = false

  # UpdateEncodeMaxTasks 设置此实例上可以运行的并发 SnapDeal 编码任务的最大数量。
  #
  # 类型：int
  #UpdateEncodeMaxTasks = 0

  # UpdateProveMaxTasks 设置此实例上可以运行的并发 SnapDeal 证明任务的最大数量。
  #
  # 类型：int
  #UpdateProveMaxTasks = 0

  # BoostAdapters 是矿工地址和端口/IP 的元组列表，用于监听市场（例如 boost）请求。
  # 此接口与 lotus-miner RPC 兼容，实现了存储市场操作所需的子集。
  # 字符串应采用 "actor:ip:port" 格式。IP 不能为 0.0.0.0。我们建议使用私有 IP。
  # 示例："f0123:127.0.0.1:32100"。可以指定多个地址。
  # 
  # 当市场节点（如 boost）向 Curio 的市场 RPC 提供要放入扇区的交易时，Curio 首先将交易数据存储在临时位置 "Piece Park" 中，然后再将其分配给扇区。这要求集群中至少有一个节点启用了 EnableParkPiece 选项，并有足够的临时空间来存储交易数据。
  # 这与 lotus-miner 不同，lotus-miner 在收到交易后立即将交易数据存储到 "未密封" 的扇区中。当计算扇区的 TreeD 和 TreeR 时会访问 PiecePark 中的交易数据，但在初始 SDR 层计算时不需要。在引用该数据的所有扇区都密封后，PiecePark 中的数据将被删除。
  # 
  # 要获取 boost 配置的 API 信息，请运行 'curio market rpc-info'
  # 
  # 注意：所有交易数据都将通过此服务流动，因此应将其放置在运行 boost 的机器上或处理 ParkPiece 任务的机器上。
  #
  # 类型：[]string
  #BoostAdapters = []

  # EnableWebGui 在此 curio 实例上启用 Web GUI。UI 的本地开销很小，但通常只需要在集群中的一台机器上运行。
  #
  # 类型：bool
  #EnableWebGui = false

  # 应该监听 Web GUI 请求的地址。
  #
  # 类型：string
  #GuiAddress = "0.0.0.0:4701"

  # UseSyntheticPoRep 为所有新扇区启用合成 PoRep。设置为 true 时，将在 TreeRC 任务完成后将磁盘上保留的缓存数据量减少到 11GiB。
  #
  # 类型：bool
  #UseSyntheticPoRep = false

  # 可以同时运行的 SyntheticPoRep 任务的最大数量。请注意，最大任务数量也将受到机器上可用资源的限制。
  #
  # 类型：int
  #SyntheticPoRepMaxTasks = 0

  # 批量密封
  #
  # 类型：bool
  #EnableBatchSeal = false


[Fees]
  # 类型：types.FIL
  #DefaultMaxFee = "0.07 FIL"

  # 类型：types.FIL
  #MaxPreCommitGasFee = "0.025 FIL"

  # 类型：types.FIL
  #MaxCommitGasFee = "0.05 FIL"

  # 类型：types.FIL
  #MaxTerminateGasFee = "0.5 FIL"

  # WindowPoSt 是一个高价值操作，因此默认费用应该较高。
  #
  # 类型：types.FIL
  #MaxWindowPoStGasFee = "5 FIL"

  # 类型：types.FIL
  #MaxPublishDealsFee = "0.05 FIL"
  # 是否使用可用的矿工余额作为扇区抵押品，而不是随每条消息一起发送
  #
  # type: bool
  #CollateralFromMinerBalance = false

  # 即使矿工参与者没有可用余额，也不要随消息发送抵押品
  #
  # type: bool
  #DisableCollateralFallback = false

  [Fees.MaxPreCommitBatchGasFee]
    # 类型：types.FIL
    #Base = "0 FIL"

    # 类型：types.FIL
    #PerSector = "0.02 FIL"

  [Fees.MaxCommitBatchGasFee]
    # 类型：types.FIL
    #Base = "0 FIL"

    # 类型：types.FIL
    #PerSector = "0.03 FIL"


[[Addresses]]
  #PreCommitControl = []

  #CommitControl = []

  #TerminateControl = []

  #DisableOwnerFallback = false

  #DisableWorkerFallback = false

  #MinerAddresses = []


[Proving]
  # 并行运行的最大扇区检查数量。(0 = 无限制)
  # 
  # 警告：将此值设置得太高可能会导致节点因耗尽堆栈而崩溃
  # 警告：将此值设置得太低可能会使扇区挑战读取变得更慢，导致由于提交延迟而失败的 PoSt
  # 
  # 更改此选项后，通过调用 'lotus-miner proving compute window-post 0' 确认新值在您的设置中是否有效
  #
  # type: int
  #ParallelCheckLimit = 32

  # 扇区证明预检可以花费的最长时间。如果检查超时，该扇区将被跳过
  # 
  # 警告：将此值设置得太低可能会导致扇区被跳过，即使它们是可访问的，只是读取测试挑战花费的时间超过了此超时时间
  # 警告：将此值设置得太高可能会在与此扇区相关的 IO 操作被阻塞的情况下错过 PoSt 截止时间（例如，在 NFS 挂载断开连接的情况下）
  #
  # type: Duration
  #SingleCheckTimeout = "10m0s"

  # 整个分区的证明预检可以花费的最长时间。如果检查超时，未能及时检查的分区中的扇区将被跳过
  # 
  # 警告：将此值设置得太低可能会导致扇区被跳过，即使它们是可访问的，只是读取测试挑战花费的时间超过了此超时时间
  # 警告：将此值设置得太高可能会在与此分区相关的 IO 操作被阻塞或缓慢的情况下错过 PoSt 截止时间
  #
  # type: Duration
  #PartitionCheckTimeout = "20m0s"

  # 禁用 WindowPoSt 可证明扇区可读性检查。
  # 
  # 在正常操作中，当准备计算 WindowPoSt 时，lotus-miner 将执行一轮从所有扇区读取挑战的过程，以确认这些扇区可以被证明。在此过程中读取的挑战会被丢弃，因为我们只关心检查扇区数据是否可以被读取。
  # 
  # 当使用内置证明计算（没有 PoSt 工作者，且 DisableBuiltinWindowPoSt 设置为 false）时，如果某些扇区不可读，这个过程可以节省大量时间和计算资源 - 这是因为内置逻辑在需要跳过某些扇区时不会跳过 snark 计算。
  # 
  # 当使用 PoSt 工作者时，这个过程大部分是多余的，PoSt 工作者的挑战将只读取一次，如果某些扇区的挑战不可读，这些扇区将被跳过。
  # 
  # 禁用扇区预检将略微减少证明扇区时的 IO 负载，可能会缩短生成窗口 PoSt 的时间。在 IO 能力良好的设置中，此选项对证明时间的影响应该可以忽略不计。
  # 
  # 注意：在没有 PoSt 工作者的设置中禁用扇区预检可能是一个糟糕的主意。
  # 
  # 注意：即使启用此选项，在向链发送恢复声明消息之前，仍会检查恢复扇区
  # 
  # 更改此选项后，通过调用 'lotus-miner proving compute window-post 0' 确认新值在您的设置中是否有效
  #
  # type: bool
  #DisableWDPoStPreChecks = false

  # 在单个 SubmitWindowPoSt 消息中证明的最大分区数。0 = 网络限制（nv21 中为 3）
  # 
  # 单个分区可能包含最多 2349 个 32GiB 扇区，或 2300 个 64GiB 扇区。
  # //
  # 注意，将此值设置得更低可能会导致气体使用效率降低 - 将发送更多消息来证明每个截止时间，导致总气体使用量增加（但每条消息的气体限制会更低）
  # 
  # 将此值设置为高于网络限制没有效果
  #
  # type: int
  #MaxPartitionsPerPoStMessage = 0

  # 在某些情况下，提交 DeclareFaultsRecovered 消息时，
  # 可能有太多恢复无法适应 BlockGasLimit。
  # 在这些情况下，可能需要将此值设置为较低的值（例如 1）；
  # 注意，将此值设置得更低可能会导致气体使用效率降低 - 将发送比需要更多的消息，
  # 导致总气体使用量增加（但每条消息的气体限制会更低）
  #
  # type: int
  #MaxPartitionsPerRecoveryMessage = 0

  # 为包含恢复扇区的分区启用每个 PoSt 消息单个分区
  # 
  # 在提交包含恢复扇区的 PoSt 消息的情况下，默认网络限制可能仍然太高，无法适应区块气体限制。在这些情况下，将恢复扇区的单个分区放在 post 消息中变得有用
  # 
  # 注意，将此值设置得更低可能会导致气体使用效率降低 - 将发送更多消息来证明每个截止时间，导致总气体使用量增加（但每条消息的气体限制会更低）
  #
  # type: bool
  #SingleRecoveringPartitionPerPostMessage = false


[Ingest]
  # 可以排队等待交易开始处理的最大扇区数量。
  # 0 = 无限制
  # 注意：此机制将延迟从市场获取交易数据，为市场子系统提供背压。
  # DealSector 队列包括准备进入密封管道但尚未进入的交易 -
  # 此队列的大小也将影响可以同时运行的 ParkPiece 任务的最大数量。
  # DealSector 队列是密封管道中的第一个队列，这意味着它应该用作主要的背压机制。
  #
  # type: int
  #MaxQueueDealSector = 8

  # 可以排队等待 SDR 开始处理的最大扇区数量。
  # 0 = 无限制
  # 注意：此机制将延迟从市场获取交易数据，为市场子系统提供背压。
  # SDR 队列包括正在进入密封管道过程中的交易。对于 SDR 任务，
  # 可能会出现此队列增长超过此限制的情况（CC 扇区），背压仅应用于进入管道的扇区。
  #
  # type: int
  #MaxQueueSDR = 8
  
  # 可以排队等待 SDRTrees 开始处理的最大扇区数量。
  # 0 = 无限制
  # 注意：此机制将延迟从市场获取交易数据，为市场子系统提供背压。
  # 对于树任务，可能会出现此队列增长超过此限制的情况，背压仅应用于进入管道的扇区。
  #
  # type: int
  #MaxQueueTrees = 0

  # 可以排队等待 PoRep 开始处理的最大扇区数量。
  # 0 = 无限制
  # 注意：此机制将延迟从市场获取交易数据，为市场子系统提供背压。
  # 与树任务类似，可能会出现此队列增长超过此限制的情况，背压仅应用于进入管道的扇区。
  #
  # type: int
  #MaxQueuePoRep = 0

  # 开放的交易扇区在开始密封之前应等待更多交易的最长时间
  #
  # type: Duration
  #MaxDealWaitTime = "1h0m0s"

  # DoSnap 启用此实例摄取的交易的快照交易处理。与 lotus-miner 不同，当没有可用于快照的扇区时，不会回退到 porep。启用后，所有交易都将是快照交易。
  #
  # type: bool
  #DoSnap = false


[Seal]
  # BatchSealSectorSize 允许设置批量密封任务支持的扇区大小。
  # 可以是任何值，只要它是 "32GiB"。
  #
  # type: string
  #BatchSealSectorSize = "32GiB"

  # 密封批次中的扇区数量。取决于硬件和 supraseal 配置。
  #
  # type: int
  #BatchSealBatchSize = 32

  # 并行管道的数量。可以是 1 或 2。取决于可用的原始块存储
  #
  # type: int
  #BatchSealPipelines = 2

  # SingleHasherPerThread 是针对较旧 CPU 的兼容性标志。Zen3 及更高版本支持每个线程两个扇区。
  # 对于较旧的 CPU（Zen 2 及之前），设置为 false。
  #
  # type: bool
  #SingleHasherPerThread = false


[Apis]
  # 存储子系统的 RPC 密钥。
  # 如果与 lotus-miner 集成，这必须与以下命令的值匹配
  # cat ~/.lotusminer/keystore/MF2XI2BNNJ3XILLQOJUXMYLUMU | jq -r .PrivateKey
  #
  # type: string
  #StorageRPCSecret = ""


[Alerting]
  # MinimumWalletBalance 是所有活跃钱包的最低余额。如果余额低于此值，将为该钱包触发警报
  #
  # type: types.FIL
  #MinimumWalletBalance = "5 FIL"

  [Alerting.PagerDuty]
    # Enable 是启用或禁用 PagerDuty 集成的标志。
    #
    # type: bool
    #Enable = false

    # PagerDutyEventURL 是 PagerDuty.com Events API v2 URL。发送到此 API URL 的事件最终会路由到 PagerDuty.com 服务并进行处理。
    # 默认值足以与商业 PagerDuty.com 公司的标准服务集成。
    #
    # type: string
    #PagerDutyEventURL = "https://events.pagerduty.com/v2/enqueue"

    # PageDutyIntegrationKey 是 PagerDuty.com 服务的集成密钥。您可以在服务的集成页面中找到这个唯一的服务标识符。
    #
    # type: string
    #PageDutyIntegrationKey = ""

  [Alerting.PrometheusAlertManager]
    # Enable 是启用或禁用 Prometheus AlertManager 集成的标志。
    #
    # type: bool
    #Enable = false

    # AlertManagerURL 是 Prometheus AlertManager API v2 URL。
    #
    # type: string
    #AlertManagerURL = "http://localhost:9093/api/v2/alerts"

  [Alerting.SlackWebhook]
    # Enable 是启用或禁用 Prometheus AlertManager 集成的标志。
    #
    # type: bool
    #Enable = false

    # WebHookURL 是 Slack Webhook 的 URL。
    # 示例：https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX
    #
    # type: string
    #WebHookURL = ""

```
