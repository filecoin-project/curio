---
description: The default curio configuration
---

# Default Curio Configuration

```toml
[Subsystems]
  # EnableWindowPost enables window post to be executed on this curio instance. Each machine in the cluster
  # with WindowPoSt enabled will also participate in the window post scheduler. It is possible to have multiple
  # machines with WindowPoSt enabled which will provide redundancy, and in case of multiple partitions per deadline,
  # will allow for parallel processing of partitions.
  # 
  # It is possible to have instances handling both WindowPoSt and WinningPoSt, which can provide redundancy without
  # the need for additional machines. In setups like this it is generally recommended to run
  # partitionsPerDeadline+1 machines.
  #
  # type: bool
  #EnableWindowPost = false

  # type: int
  #WindowPostMaxTasks = 0

  # EnableWinningPost enables winning post to be executed on this curio instance.
  # Each machine in the cluster with WinningPoSt enabled will also participate in the winning post scheduler.
  # It is possible to mix machines with WindowPoSt and WinningPoSt enabled, for details see the EnableWindowPost
  # documentation.
  #
  # type: bool
  #EnableWinningPost = false

  # type: int
  #WinningPostMaxTasks = 0

  # EnableParkPiece enables the "piece parking" task to run on this node. This task is responsible for fetching
  # pieces from the network and storing them in the storage subsystem until sectors are sealed. This task is
  # only applicable when integrating with boost, and should be enabled on nodes which will hold deal data
  # from boost until sectors containing the related pieces have the TreeD/TreeR constructed.
  # Note that future Curio implementations will have a separate task type for fetching pieces from the internet.
  #
  # type: bool
  #EnableParkPiece = false

  # type: int
  #ParkPieceMaxTasks = 0

  # EnableSealSDR enables SDR tasks to run. SDR is the long sequential computation
  # creating 11 layer files in sector cache directory.
  # 
  # SDR is the first task in the sealing pipeline. It's inputs are just the hash of the
  # unsealed data (CommD), sector number, miner id, and the seal proof type.
  # It's outputs are the 11 layer files in the sector cache directory.
  # 
  # In lotus-miner this was run as part of PreCommit1.
  #
  # type: bool
  #EnableSealSDR = false

  # The maximum amount of SDR tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine.
  #
  # type: int
  #SealSDRMaxTasks = 0

  # The maximum amount of SDR tasks that need to be queued before the system will start accepting new tasks.
  # The main purpose of this setting is to allow for enough tasks to accumulate for batch sealing. When batch sealing
  # nodes are present in the cluster, this value should be set to batch_size+1 to allow for the batch sealing node to
  # fill up the batch.
  # This setting can also be used to give priority to other nodes in the cluster by setting this value to a higher
  # value on the nodes which should have less priority.
  #
  # type: int
  #SealSDRMinTasks = 0

  # EnableSealSDRTrees enables the SDR pipeline tree-building task to run.
  # This task handles encoding of unsealed data into last sdr layer and building
  # of TreeR, TreeC and TreeD.
  # 
  # This task runs after SDR
  # TreeD is first computed with optional input of unsealed data
  # TreeR is computed from replica, which is first computed as field
  # addition of the last SDR layer and the bottom layer of TreeD (which is the unsealed data)
  # TreeC is computed from the 11 SDR layers
  # The 3 trees will later be used to compute the PoRep proof.
  # 
  # In case of SyntheticPoRep challenges for PoRep will be pre-generated at this step, and trees and layers
  # will be dropped. SyntheticPoRep works by pre-generating a very large set of challenges (~30GiB on disk)
  # then using a small subset of them for the actual PoRep computation. This allows for significant scratch space
  # saving between PreCommit and PoRep generation at the expense of more computation (generating challenges in this step)
  # 
  # In lotus-miner this was run as part of PreCommit2 (TreeD was run in PreCommit1).
  # Note that nodes with SDRTrees enabled will also answer to Finalize tasks,
  # which just remove unneeded tree data after PoRep is computed.
  #
  # type: bool
  #EnableSealSDRTrees = false

  # The maximum amount of SealSDRTrees tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine.
  #
  # type: int
  #SealSDRTreesMaxTasks = 0

  # FinalizeMaxTasks is the maximum amount of finalize tasks that can run simultaneously.
  # The finalize task is enabled on all machines which also handle SDRTrees tasks. Finalize ALWAYS runs on whichever
  # machine holds sector cache files, as it removes unneeded tree data after PoRep is computed.
  # Finalize will run in parallel with the SubmitCommitMsg task.
  #
  # type: int
  #FinalizeMaxTasks = 0

  # EnableSendPrecommitMsg enables the sending of precommit messages to the chain
  # from this curio instance.
  # This runs after SDRTrees and uses the output CommD / CommR (roots of TreeD / TreeR) for the message
  #
  # type: bool
  #EnableSendPrecommitMsg = false

  # EnablePoRepProof enables the computation of the porep proof
  # 
  # This task runs after interactive-porep seed becomes available, which happens 150 epochs (75min) after the
  # precommit message lands on chain. This task should run on a machine with a GPU. Vanilla PoRep proofs are
  # requested from the machine which holds sector cache files which most likely is the machine which ran the SDRTrees
  # task.
  # 
  # In lotus-miner this was Commit1 / Commit2
  #
  # type: bool
  #EnablePoRepProof = false

  # The maximum amount of PoRepProof tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine.
  #
  # type: int
  #PoRepProofMaxTasks = 0

  # EnableSendCommitMsg enables the sending of commit messages to the chain
  # from this curio instance.
  #
  # type: bool
  #EnableSendCommitMsg = false

  # Whether to abort if any sector activation in a batch fails (newly sealed sectors, only with ProveCommitSectors3).
  #
  # type: bool
  #RequireActivationSuccess = true

  # Whether to abort if any sector activation in a batch fails (updating sectors, only with ProveReplicaUpdates3).
  #
  # type: bool
  #RequireNotificationSuccess = true

  # EnableMoveStorage enables the move-into-long-term-storage task to run on this curio instance.
  # This tasks should only be enabled on nodes with long-term storage.
  # 
  # The MoveStorage task is the last task in the sealing pipeline. It moves the sealed sector data from the
  # SDRTrees machine into long-term storage. This task runs after the Finalize task.
  #
  # type: bool
  #EnableMoveStorage = false

  # NoUnsealedDecode disables the decoding sector data on this node. Normally data encoding is enabled by default on
  # storage nodes with the MoveStorage task enabled. Setting this option to true means that unsealed data for sectors
  # will not be stored on this node
  #
  # type: bool
  #NoUnsealedDecode = false

  # The maximum amount of MoveStorage tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. It is recommended that this value is set to a number which
  # uses all available network (or disk) bandwidth on the machine without causing bottlenecks.
  #
  # type: int
  #MoveStorageMaxTasks = 0

  # EnableUpdateEncode enables the encoding step of the SnapDeal process on this curio instance.
  # This step involves encoding the data into the sector and computing updated TreeR (uses gpu).
  #
  # type: bool
  #EnableUpdateEncode = false

  # EnableUpdateProve enables the proving step of the SnapDeal process on this curio instance.
  # This step generates the snark proof for the updated sector.
  #
  # type: bool
  #EnableUpdateProve = false

  # EnableUpdateSubmit enables the submission of SnapDeal proofs to the blockchain from this curio instance.
  # This step submits the generated proofs to the chain.
  #
  # type: bool
  #EnableUpdateSubmit = false

  # UpdateEncodeMaxTasks sets the maximum number of concurrent SnapDeal encoding tasks that can run on this instance.
  #
  # type: int
  #UpdateEncodeMaxTasks = 0

  # UpdateProveMaxTasks sets the maximum number of concurrent SnapDeal proving tasks that can run on this instance.
  #
  # type: int
  #UpdateProveMaxTasks = 0

  # EnableCommP enabled the commP task on te node. CommP is calculated before sending PublishDealMessage for a Mk12
  # deal, and when checking sector data with 'curio unseal check'.
  #
  # type: bool
  #EnableCommP = false

  # BoostAdapters is a list of tuples of miner address and port/ip to listen for market (e.g. boost) requests.
  # This interface is compatible with the lotus-miner RPC, implementing a subset needed for storage market operations.
  # Strings should be in the format "actor:ip:port". IP cannot be 0.0.0.0. We recommend using a private IP.
  # Example: "f0123:127.0.0.1:32100". Multiple addresses can be specified.
  # 
  # When a market node like boost gives Curio's market RPC a deal to placing into a sector, Curio will first store the
  # deal data in a temporary location "Piece Park" before assigning it to a sector. This requires that at least one
  # node in the cluster has the EnableParkPiece option enabled and has sufficient scratch space to store the deal data.
  # This is different from lotus-miner which stored the deal data into an "unsealed" sector as soon as the deal was
  # received. Deal data in PiecePark is accessed when the sector TreeD and TreeR are computed, but isn't needed for
  # the initial SDR layers computation. Pieces in PiecePark are removed after all sectors referencing the piece are
  # sealed.
  # 
  # To get API info for boost configuration run 'curio market rpc-info'
  # 
  # NOTE: All deal data will flow through this service, so it should be placed on a machine running boost or on
  # a machine which handles ParkPiece tasks.
  #
  # type: []string
  #BoostAdapters = []

  # EnableWebGui enables the web GUI on this curio instance. The UI has minimal local overhead, but it should
  # only need to be run on a single machine in the cluster.
  #
  # type: bool
  #EnableWebGui = false

  # The address that should listen for Web GUI requests.
  #
  # type: string
  #GuiAddress = "0.0.0.0:4701"

  # UseSyntheticPoRep enables the synthetic PoRep for all new sectors. When set to true, will reduce the amount of
  # cache data held on disk after the completion of TreeRC task to 11GiB.
  #
  # type: bool
  #UseSyntheticPoRep = false

  # The maximum amount of SyntheticPoRep tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine.
  #
  # type: int
  #SyntheticPoRepMaxTasks = 0

  # Batch Seal
  #
  # type: bool
  #EnableBatchSeal = false


[Fees]
  # type: types.FIL
  #DefaultMaxFee = "0.07 FIL"

  # type: types.FIL
  #MaxPreCommitGasFee = "0.025 FIL"

  # type: types.FIL
  #MaxCommitGasFee = "0.05 FIL"

  # type: types.FIL
  #MaxTerminateGasFee = "0.5 FIL"

  # WindowPoSt is a high-value operation, so the default fee should be high.
  #
  # type: types.FIL
  #MaxWindowPoStGasFee = "5 FIL"

  # type: types.FIL
  #MaxPublishDealsFee = "0.05 FIL"

  # Whether to use available miner balance for sector collateral instead of sending it with each message
  #
  # type: bool
  #CollateralFromMinerBalance = false

  # Don't send collateral with messages even if there is no available balance in the miner actor
  #
  # type: bool
  #DisableCollateralFallback = false

  [Fees.MaxPreCommitBatchGasFee]
    # type: types.FIL
    #Base = "0 FIL"

    # type: types.FIL
    #PerSector = "0.02 FIL"

  [Fees.MaxCommitBatchGasFee]
    # type: types.FIL
    #Base = "0 FIL"

    # type: types.FIL
    #PerSector = "0.03 FIL"


[[Addresses]]
  #PreCommitControl = []

  #CommitControl = []

  #TerminateControl = []

  #DisableOwnerFallback = false

  #DisableWorkerFallback = false

  #MinerAddresses = []


[Proving]
  # Maximum number of sector checks to run in parallel. (0 = unlimited)
  # 
  # WARNING: Setting this value too high may make the node crash by running out of stack
  # WARNING: Setting this value too low may make sector challenge reading much slower, resulting in failed PoSt due
  # to late submission.
  # 
  # After changing this option, confirm that the new value works in your setup by invoking
  # 'curio test wd task 0'
  #
  # type: int
  #ParallelCheckLimit = 32

  # Maximum amount of time a proving pre-check can take for a sector. If the check times out the sector will be skipped
  # 
  # WARNING: Setting this value too low risks in sectors being skipped even though they are accessible, just reading the
  # test challenge took longer than this timeout
  # WARNING: Setting this value too high risks missing PoSt deadline in case IO operations related to this sector are
  # blocked (e.g. in case of disconnected NFS mount)
  #
  # type: Duration
  #SingleCheckTimeout = "10m0s"

  # Maximum amount of time a proving pre-check can take for an entire partition. If the check times out, sectors in
  # the partition which didn't get checked on time will be skipped
  # 
  # WARNING: Setting this value too low risks in sectors being skipped even though they are accessible, just reading the
  # test challenge took longer than this timeout
  # WARNING: Setting this value too high risks missing PoSt deadline in case IO operations related to this partition are
  # blocked or slow
  #
  # type: Duration
  #PartitionCheckTimeout = "20m0s"


[Ingest]
  # Maximum number of sectors that can be queued waiting for deals to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # The DealSector queue includes deals which are ready to enter the sealing pipeline but are not yet part of it -
  # size of this queue will also impact the maximum number of ParkPiece tasks which can run concurrently.
  # DealSector queue is the first queue in the sealing pipeline, meaning that it should be used as the primary backpressure mechanism.
  #
  # type: int
  #MaxQueueDealSector = 8

  # Maximum number of sectors that can be queued waiting for SDR to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # The SDR queue includes deals which are in the process of entering the sealing pipeline. In case of the SDR tasks it is
  # possible that this queue grows more than this limit(CC sectors), the backpressure is only applied to sectors
  # entering the pipeline.
  # Only applies to PoRep pipeline (DoSnap = false)
  #
  # type: int
  #MaxQueueSDR = 8

  # Maximum number of sectors that can be queued waiting for SDRTrees to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # In case of the trees tasks it is possible that this queue grows more than this limit, the backpressure is only
  # applied to sectors entering the pipeline.
  # Only applies to PoRep pipeline (DoSnap = false)
  #
  # type: int
  #MaxQueueTrees = 0

  # Maximum number of sectors that can be queued waiting for PoRep to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # Like with the trees tasks, it is possible that this queue grows more than this limit, the backpressure is only
  # applied to sectors entering the pipeline.
  # Only applies to PoRep pipeline (DoSnap = false)
  #
  # type: int
  #MaxQueuePoRep = 0

  # MaxQueueSnapEncode is the maximum number of sectors that can be queued waiting for UpdateEncode to start processing.
  # 0 means unlimited.
  # This applies backpressure to the market subsystem by delaying the ingestion of deal data.
  # Only applies to the Snap Deals pipeline (DoSnap = true).
  #
  # type: int
  #MaxQueueSnapEncode = 16

  # MaxQueueSnapProve is the maximum number of sectors that can be queued waiting for UpdateProve to start processing.
  # 0 means unlimited.
  # This applies backpressure to the market subsystem by delaying the ingestion of deal data.
  # Only applies to the Snap Deals pipeline (DoSnap = true).
  #
  # type: int
  #MaxQueueSnapProve = 0

  # Maximum time an open deal sector should wait for more deal before it starts sealing
  #
  # type: Duration
  #MaxDealWaitTime = "1h0m0s"

  # DoSnap enables the snap deal process for deals ingested by this instance. Unlike in lotus-miner there is no
  # fallback to porep when no sectors are available to snap into. When enabled all deals will be snap deals.
  #
  # type: bool
  #DoSnap = false


[Seal]
  # BatchSealSectorSize Allows setting the sector size supported by the batch seal task.
  # Can be any value as long as it is "32GiB".
  #
  # type: string
  #BatchSealSectorSize = "32GiB"

  # Number of sectors in a seal batch. Depends on hardware and supraseal configuration.
  #
  # type: int
  #BatchSealBatchSize = 32

  # Number of parallel pipelines. Can be 1 or 2. Depends on available raw block storage
  #
  # type: int
  #BatchSealPipelines = 2

  # SingleHasherPerThread is a compatibility flag for older CPUs. Zen3 and later supports two sectors per thread.
  # Set to false for older CPUs (Zen 2 and before).
  #
  # type: bool
  #SingleHasherPerThread = false


[Apis]
  # Chain API auth secret for the Curio nodes to use.
  #
  # type: string
  #StorageRPCSecret = ""


[Alerting]
  # MinimumWalletBalance is the minimum balance all active wallets. If the balance is below this value, an
  # alerts will be triggered for the wallet
  #
  # type: types.FIL
  #MinimumWalletBalance = "5 FIL"

  [Alerting.PagerDuty]
    # Enable is a flag to enable or disable the PagerDuty integration.
    #
    # type: bool
    #Enable = false

    # PagerDutyEventURL is URL for PagerDuty.com Events API v2 URL. Events sent to this API URL are ultimately
    # routed to a PagerDuty.com service and processed.
    # The default is sufficient for integration with the stock commercial PagerDuty.com company's service.
    #
    # type: string
    #PagerDutyEventURL = "https://events.pagerduty.com/v2/enqueue"

    # PageDutyIntegrationKey is the integration key for a PagerDuty.com service. You can find this unique service
    # identifier in the integration page for the service.
    #
    # type: string
    #PageDutyIntegrationKey = ""

  [Alerting.PrometheusAlertManager]
    # Enable is a flag to enable or disable the Prometheus AlertManager integration.
    #
    # type: bool
    #Enable = false

    # AlertManagerURL is the URL for the Prometheus AlertManager API v2 URL.
    #
    # type: string
    #AlertManagerURL = "http://localhost:9093/api/v2/alerts"

  [Alerting.SlackWebhook]
    # Enable is a flag to enable or disable the Prometheus AlertManager integration.
    #
    # type: bool
    #Enable = false

    # WebHookURL is the URL for the URL for slack Webhook.
    # Example: https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX
    #
    # type: string
    #WebHookURL = ""

```
