---
description: The default curio configuration
---

# Default Curio Configuration

```toml
# Subsystems defines configuration settings for various subsystems within the Curio node.
#
# type: CurioSubsystemsConfig
[Subsystems]

  # EnableWindowPost enables window post to be executed on this curio instance. Each machine in the cluster
  # with WindowPoSt enabled will also participate in the window post scheduler. It is possible to have multiple
  # machines with WindowPoSt enabled which will provide redundancy, and in case of multiple partitions per deadline,
  # will allow for parallel processing of partitions.
  # 
  # It is possible to have instances handling both WindowPoSt and WinningPoSt, which can provide redundancy without
  # the need for additional machines. In setups like this it is generally recommended to run
  # partitionsPerDeadline+1 machines. (Default: false)
  #
  # type: bool
  #EnableWindowPost = false

  # The maximum amount of WindowPostMaxTasks tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. We do not recommend setting this value and let system resources determine
  # the maximum tasks (Default: 0 - unlimited)
  #
  # type: int
  #WindowPostMaxTasks = 0

  # EnableWinningPost enables winning post to be executed on this curio instance.
  # Each machine in the cluster with WinningPoSt enabled will also participate in the winning post scheduler.
  # It is possible to mix machines with WindowPoSt and WinningPoSt enabled, for details see the EnableWindowPost
  # documentation. (Default: false)
  #
  # type: bool
  #EnableWinningPost = false

  # The maximum amount of WinningPostMaxTasks tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. We do not recommend setting this value and let system resources determine
  # the maximum tasks (Default: 0 - unlimited)
  #
  # type: int
  #WinningPostMaxTasks = 0

  # EnableParkPiece enables the "piece parking" task to run on this node. This task is responsible for fetching
  # pieces from the network and storing them in the storage subsystem until sectors are sealed. This task is
  # only applicable when integrating with boost, and should be enabled on nodes which will hold deal data
  # from boost until sectors containing the related pieces have the TreeD/TreeR constructed.
  # Note that future Curio implementations will have a separate task type for fetching pieces from the internet. (Default: false)
  #
  # type: bool
  #EnableParkPiece = false

  # The maximum amount of ParkPieceMaxTasks tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine (Default: 0 - unlimited)
  #
  # type: int
  #ParkPieceMaxTasks = 0

  # EnableSealSDR enables SDR tasks to run. SDR is the long sequential computation
  # creating 11 layer files in sector cache directory.
  # 
  # SDR is the first task in the sealing pipeline. It's inputs are just the hash of the
  # unsealed data (CommD), sector number, miner id, and the seal proof type.
  # It's outputs are the 11 layer files in the sector cache directory.
  # 
  # In lotus-miner this was run as part of PreCommit1. (Default: false)
  #
  # type: bool
  #EnableSealSDR = false

  # The maximum amount of SDR tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. (Default: 0 - unlimited)
  #
  # type: int
  #SealSDRMaxTasks = 0

  # The maximum amount of SDR tasks that need to be queued before the system will start accepting new tasks.
  # The main purpose of this setting is to allow for enough tasks to accumulate for batch sealing. When batch sealing
  # nodes are present in the cluster, this value should be set to batch_size+1 to allow for the batch sealing node to
  # fill up the batch.
  # This setting can also be used to give priority to other nodes in the cluster by setting this value to a higher
  # value on the nodes which should have less priority. (Default: 0 - unlimited)
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
  # which just remove unneeded tree data after PoRep is computed. (Default: false)
  #
  # type: bool
  #EnableSealSDRTrees = false

  # The maximum amount of SealSDRTrees tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. (Default: 0 - unlimited)
  #
  # type: int
  #SealSDRTreesMaxTasks = 0

  # FinalizeMaxTasks is the maximum amount of finalize tasks that can run simultaneously.
  # The finalize task is enabled on all machines which also handle SDRTrees tasks. Finalize ALWAYS runs on whichever
  # machine holds sector cache files, as it removes unneeded tree data after PoRep is computed.
  # Finalize will run in parallel with the SubmitCommitMsg task. (Default: 0 - unlimited)
  #
  # type: int
  #FinalizeMaxTasks = 0

  # EnableSendPrecommitMsg enables the sending of precommit messages to the chain
  # from this curio instance.
  # This runs after SDRTrees and uses the output CommD / CommR (roots of TreeD / TreeR) for the message (Default: false)
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
  # In lotus-miner this was Commit1 / Commit2 (Default: false)
  #
  # type: bool
  #EnablePoRepProof = false

  # The maximum amount of PoRepProof tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. (Default: 0 - unlimited)
  #
  # type: int
  #PoRepProofMaxTasks = 0

  # EnableSendCommitMsg enables the sending of commit messages to the chain
  # from this curio instance. (Default: false)
  #
  # type: bool
  #EnableSendCommitMsg = false

  # Whether to abort if any sector activation in a batch fails (newly sealed sectors, only with ProveCommitSectors3). (Default: true)
  #
  # type: bool
  #RequireActivationSuccess = true

  # Whether to abort if any sector activation in a batch fails (updating sectors, only with ProveReplicaUpdates3). (Default: true)
  #
  # type: bool
  #RequireNotificationSuccess = true

  # EnableMoveStorage enables the move-into-long-term-storage task to run on this curio instance.
  # This tasks should only be enabled on nodes with long-term storage.
  # 
  # The MoveStorage task is the last task in the sealing pipeline. It moves the sealed sector data from the
  # SDRTrees machine into long-term storage. This task runs after the Finalize task. (Default: false)
  #
  # type: bool
  #EnableMoveStorage = false

  # NoUnsealedDecode disables the decoding sector data on this node. Normally data encoding is enabled by default on
  # storage nodes with the MoveStorage task enabled. Setting this option to true means that unsealed data for sectors
  # will not be stored on this node (Default: false)
  #
  # type: bool
  #NoUnsealedDecode = false

  # The maximum amount of MoveStorage tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. It is recommended that this value is set to a number which
  # uses all available network (or disk) bandwidth on the machine without causing bottlenecks. NOTE: unlike most other
  # tasks, when this value is set the maximum number of concurrent tasks will not be bounded by CPU core count (Default: 0 - unlimited)
  #
  # type: int
  #MoveStorageMaxTasks = 0

  # EnableUpdateEncode enables the encoding step of the SnapDeal process on this curio instance.
  # This step involves encoding the data into the sector and computing updated TreeR (uses gpu). (Default: false)
  #
  # type: bool
  #EnableUpdateEncode = false

  # EnableUpdateProve enables the proving step of the SnapDeal process on this curio instance.
  # This step generates the snark proof for the updated sector. (Default: false)
  #
  # type: bool
  #EnableUpdateProve = false

  # EnableUpdateSubmit enables the submission of SnapDeal proofs to the blockchain from this curio instance.
  # This step submits the generated proofs to the chain. (Default: false)
  #
  # type: bool
  #EnableUpdateSubmit = false

  # UpdateEncodeMaxTasks sets the maximum number of concurrent SnapDeal encoding tasks that can run on this instance. (Default: 0 - unlimited)
  #
  # type: int
  #UpdateEncodeMaxTasks = 0

  # UpdateProveMaxTasks sets the maximum number of concurrent SnapDeal proving tasks that can run on this instance. (Default: 0 - unlimited)
  #
  # type: int
  #UpdateProveMaxTasks = 0

  # EnableWebGui enables the web GUI on this curio instance. The UI has minimal local overhead, but it should
  # only need to be run on a single machine in the cluster. (Default: false)
  #
  # type: bool
  #EnableWebGui = false

  # The address that should listen for Web GUI requests. It should be in form "x.x.x.x:1234" (Default: 0.0.0.0:4701)
  #
  # type: string
  #GuiAddress = "0.0.0.0:4701"

  # UseSyntheticPoRep enables the synthetic PoRep for all new sectors. When set to true, will reduce the amount of
  # cache data held on disk after the completion of TreeRC task to 11GiB. (Default: false)
  #
  # type: bool
  #UseSyntheticPoRep = false

  # The maximum amount of SyntheticPoRep tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. (Default: 0 - unlimited)
  #
  # type: int
  #SyntheticPoRepMaxTasks = 0

  # EnableBatchSeal enabled SupraSeal batch sealing on the node.  (Default: false)
  #
  # type: bool
  #EnableBatchSeal = false

  # EnableDealMarket enabled the deal market on the node. This would also enable libp2p on the node, if configured. (Default: false)
  #
  # type: bool
  #EnableDealMarket = false

  # Enable handling for PDP (proof-of-data possession) deals / proving on this node.
  # PDP deals allow the node to directly store and prove unsealed data with "PDP Services" like Storacha.
  # This feature is BETA and should only be enabled on nodes which are part of a PDP network.
  #
  # type: bool
  #EnablePDP = false

  # EnableCommP enables the commP task on te node. CommP is calculated before sending PublishDealMessage for a Mk12 deal
  # Must have EnableDealMarket = True (Default: false)
  #
  # type: bool
  #EnableCommP = false

  # The maximum amount of CommP tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. (Default: 0 - unlimited)
  #
  # type: int
  #CommPMaxTasks = 0

  # The maximum amount of indexing and IPNI tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. (Default: 8)
  #
  # type: int
  #IndexingMaxTasks = 8

  # EnableBalanceManager enables the task to automatically manage the market balance of the miner's market actor (Default: false)
  #
  # type: bool
  #EnableBalanceManager = false

  # BindSDRTreeToNode forces the TreeD and TreeRC tasks to be executed on the same node where SDR task was executed
  # for the sector. Please ensure that TreeD and TreeRC task are enabled and relevant resources are available before
  # enabling this option. (Default: false)
  #
  # type: bool
  #BindSDRTreeToNode = false

  # EnableProofShare enables the ProofShare tasks on the node. This subsystem will request proof work from a marketplace
  # whenever local machine can take on more Snark work. ProofShare tasks have priority over local snark tasks, but new
  # ProofShare work will only be requested if there is no local work to do.
  # 
  # This feature is currently experimental and may change in the future. (Default: false)
  #
  # type: bool
  #EnableProofShare = false

  # The maximum amount of ProofShare tasks that can run simultaneously. Note that the maximum number of tasks will
  # also be bounded by resources available on the machine. (Default: 0 - unlimited)
  #
  # type: int
  #ProofShareMaxTasks = 0

  # EnableRemoteProofs enables the remote proof tasks on the node. Local snark tasks will be transformed into remote
  # proving tasks when this option is enabled. Details on which SP IDs are allowed to request remote proofs are managed
  # via Client Settings on the Proofshare webui page. Buy delay can also be set in the Client Settings page. (Default: false)
  #
  # type: bool
  #EnableRemoteProofs = false

  # The maximum number of remote proofs that can be uploaded simultaneously by each node. (Default: 15)
  #
  # type: int
  #RemoteProofMaxUploads = 15

  # EnableWalletExporter enables the wallet exporter on the node. This will export wallet stats to prometheus.
  # NOTE: THIS MUST BE ENABLED ONLY ON A SINGLE NODE IN THE CLUSTER TO BE USEFUL (Default: false)
  #
  # type: bool
  #EnableWalletExporter = false


# Fees holds the fee-related configuration parameters for various operations in the Curio node.
#
# type: CurioFees
[Fees]

  # WindowPoSt is a high-value operation, so the default fee should be high.
  # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix. (Default: "5 fil")
  #
  # type: types.FIL
  #MaxWindowPoStGasFee = "5 FIL"

  # Whether to use available miner balance for sector collateral instead of sending it with each message (Default: false)
  #
  # type: bool
  #CollateralFromMinerBalance = false

  # Don't send collateral with messages even if there is no available balance in the miner actor (Default: false)
  #
  # type: bool
  #DisableCollateralFallback = false

  # MaximizeFeeCap makes the sender set maximum allowed FeeCap on all sent messages.
  # This generally doesn't increase message cost, but in highly congested network messages
  # are much less likely to get stuck in mempool. (Default: true)
  #
  # type: bool
  #MaximizeFeeCap = true

  # maxBatchFee = maxBase + maxPerSector * nSectors
  # (Default: #Base = "0 FIL" and #PerSector = "0.02 FIL")
  #
  # type: BatchFeeConfig
  [Fees.MaxPreCommitBatchGasFee]

    # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix.
    #
    # type: types.FIL
    #Base = "0 FIL"

    # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix.
    #
    # type: types.FIL
    #PerSector = "0.02 FIL"

  # maxBatchFee = maxBase + maxPerSector * nSectors
  # (Default: #Base = "0 FIL" and #PerSector = "0.03 FIL")
  #
  # type: BatchFeeConfig
  [Fees.MaxCommitBatchGasFee]

    # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix.
    #
    # type: types.FIL
    #Base = "0 FIL"

    # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix.
    #
    # type: types.FIL
    #PerSector = "0.03 FIL"

  # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix.
  # (Default: #Base = "0 FIL" and #PerSector = "0.03 FIL")
  #
  # type: BatchFeeConfig
  [Fees.MaxUpdateBatchGasFee]

    # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix.
    #
    # type: types.FIL
    #Base = "0 FIL"

    # Accepts a decimal string (e.g., "123.45") with optional "fil" or "attofil" suffix.
    #
    # type: types.FIL
    #PerSector = "0.03 FIL"


# Addresses specifies the list of miner addresses and their related wallet addresses.
#
# type: []CurioAddresses
[[Addresses]]

  # PreCommitControl is an array of Addresses to send PreCommit messages from
  #
  # type: []string
  #PreCommitControl = []

  # CommitControl is an array of Addresses to send Commit messages from
  #
  # type: []string
  #CommitControl = []

  # DealPublishControl is an array of Address to send the deal collateral from with PublishStorageDeal Message
  #
  # type: []string
  #DealPublishControl = []

  # TerminateControl is a list of addresses used to send Terminate messages.
  #
  # type: []string
  #TerminateControl = []

  # DisableOwnerFallback disables usage of the owner address for messages
  # sent automatically
  #
  # type: bool
  #DisableOwnerFallback = false

  # DisableWorkerFallback disables usage of the worker address for messages
  # sent automatically, if control addresses are configured.
  # A control address that doesn't have enough funds will still be chosen
  # over the worker address if this flag is set.
  #
  # type: bool
  #DisableWorkerFallback = false

  # MinerAddresses are the addresses of the miner actors
  #
  # type: []string
  #MinerAddresses = []

  # BalanceManagerConfig specifies the configuration parameters for managing wallet balances and actor-related funds,
  # including collateral and other operational resources.
  #
  # type: BalanceManagerConfig
  [Addresses.BalanceManager]

    # MK12Collateral defines the configuration for managing collateral and related balance thresholds in the miner's market.
    #
    # type: MK12CollateralConfig
    [Addresses.BalanceManager.MK12Collateral]

      # DealCollateralWallet is the wallet used to add balance to Miner's market balance. This balance is
      # utilized for deal collateral in market (f05) deals.
      #
      # type: string
      #DealCollateralWallet = ""

      # CollateralLowThreshold is the balance below which more balance will be added to miner's market balance
      # Accepts a decimal string (e.g., "123.45" or "123 fil") with optional "fil" or "attofil" suffix. (Default: "5 FIL")
      #
      # type: types.FIL
      #CollateralLowThreshold = "5 FIL"

      # CollateralHighThreshold is the target balance to which the miner's market balance will be topped up
      # when it drops below CollateralLowThreshold.
      # Accepts a decimal string (e.g., "123.45" or "123 fil") with optional "fil" or "attofil" suffix. (Default: "20 FIL")
      #
      # type: types.FIL
      #CollateralHighThreshold = "20 FIL"


# Proving defines the configuration settings related to proving functionality within the Curio node.
#
# type: CurioProvingConfig
[Proving]

  # Maximum number of sector checks to run in parallel. (0 = unlimited)
  # 
  # WARNING: Setting this value too high may make the node crash by running out of stack
  # WARNING: Setting this value too low may make sector challenge reading much slower, resulting in failed PoSt due
  # to late submission.
  # 
  # After changing this option, confirm that the new value works in your setup by invoking
  # 'curio test wd task 0' (Default: 32)
  #
  # type: int
  #ParallelCheckLimit = 32

  # Maximum amount of time a proving pre-check can take for a sector. If the check times out the sector will be skipped
  # 
  # WARNING: Setting this value too low risks in sectors being skipped even though they are accessible, just reading the
  # test challenge took longer than this timeout
  # WARNING: Setting this value too high risks missing PoSt deadline in case IO operations related to this sector are
  # blocked (e.g. in case of disconnected NFS mount)
  # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "10m0s")
  #
  # type: time.Duration
  #SingleCheckTimeout = "10m0s"

  # Maximum amount of time a proving pre-check can take for an entire partition. If the check times out, sectors in
  # the partition which didn't get checked on time will be skipped
  # 
  # WARNING: Setting this value too low risks in sectors being skipped even though they are accessible, just reading the
  # test challenge took longer than this timeout
  # WARNING: Setting this value too high risks missing PoSt deadline in case IO operations related to this partition are
  # blocked or slow. Time duration string (e.g., "1h2m3s") in TOML format.  (Default: "20m0s")
  #
  # type: time.Duration
  #PartitionCheckTimeout = "20m0s"


# HTTP represents the configuration for the HTTP server settings in the Curio node.
#
# type: HTTPConfig
[HTTP]

  # Enable the HTTP server on the node
  #
  # type: bool
  #Enable = false

  # DomainName specifies the domain name that the server uses to serve HTTP requests. DomainName cannot be empty and cannot be
  # an IP address
  #
  # type: string
  #DomainName = ""

  # ListenAddress is the address that the server listens for HTTP requests. It should be in form "x.x.x.x:1234" (Default: 0.0.0.0:12310)
  #
  # type: string
  #ListenAddress = "0.0.0.0:12310"

  # DelegateTLS allows the server to delegate TLS to a reverse proxy. When enabled the listen address will serve
  # HTTP and the reverse proxy will handle TLS termination.
  #
  # type: bool
  #DelegateTLS = false

  # ReadTimeout is the maximum duration for reading the entire or next request, including body, from the client.
  # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "5m0s")
  #
  # type: time.Duration
  #ReadTimeout = "10s"

  # IdleTimeout is the maximum duration of an idle session. If set, idle connections are closed after this duration.
  # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "5m0s")
  #
  # type: time.Duration
  #IdleTimeout = "1h0m0s"

  # ReadHeaderTimeout is amount of time allowed to read request headers
  # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "5m0s")
  #
  # type: time.Duration
  #ReadHeaderTimeout = "5s"

  # EnableCORS indicates whether Cross-Origin Resource Sharing (CORS) is enabled or not.
  #
  # type: bool
  #EnableCORS = true

  # CSP sets the Content Security Policy for content served via the /piece/ retrieval endpoint.
  # Valid values: "off", "self", "inline" (Default: "inline")
  # 
  # Since storage providers serve user-uploaded content on their domain, CSP helps control
  # what these files can do when rendered in browsers. Choose based on your use case:
  # 
  # - "off": No CSP headers. Content can load any external resources and execute any scripts.
  # Use only if you fully trust all stored content or need maximum compatibility.
  # 
  # - "self": Restricts content to only load resources from your domain. Prevents external
  # resource loading but allows stored HTML/JS/CSS to interact with each other.
  # Good for semi-trusted content that needs internal functionality.
  # 
  # - "inline": (Default) Allows inline scripts/styles and same-origin resources. Provides
  # basic protection while maintaining compatibility with most web content.
  # Suitable for general-purpose content hosting.
  # 
  # Note: Stricter policies may prevent some HTML content from displaying as intended.
  # Consider the trust level of your users and whether you need to support interactive content.
  #
  # type: string
  #CSP = "inline"

  # CompressionLevels hold the compression level for various compression methods supported by the server
  #
  # type: CompressionConfig
  [HTTP.CompressionLevels]

    # type: int
    #GzipLevel = 6

    # type: int
    #BrotliLevel = 4

    # type: int
    #DeflateLevel = 6


# Market specifies configuration options for the Market subsystem within the Curio node.
#
# type: MarketConfig
[Market]

  # StorageMarketConfig houses all the deal related market configuration
  #
  # type: StorageMarketConfig
  [Market.StorageMarketConfig]

    # PieceLocator is a list of HTTP url and headers combination to query for a piece for offline deals
    # User can run a remote file server which can host all the pieces over the HTTP and supply a reader when requested.
    # The server must support "HEAD" request and "GET" request.
    # 1. <URL>?id=pieceCID with "HEAD" request responds with 200 if found or 404 if not. Must send header "Content-Length" with file size as value
    # 2. <URL>?id=pieceCID must provide a reader for the requested piece along with header "Content-Length" with file size as value
    #
    # type: []PieceLocatorConfig
    #PieceLocator = []

    # MK12 encompasses all configuration related to deal protocol mk1.2.0 and mk1.2.1 (i.e. Boost deals)
    #
    # type: MK12Config
    [Market.StorageMarketConfig.MK12]

      # When a deal is ready to publish, the amount of time to wait for more
      # deals to be ready to publish before publishing them all as a batch
      # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "5m0s")
      #
      # type: time.Duration
      #PublishMsgPeriod = "5m0s"

      # The maximum number of deals to include in a single PublishStorageDeals
      # message (Default: 8)
      #
      # type: uint64
      #MaxDealsPerPublishMsg = 8

      # The maximum fee to pay per deal when sending the PublishStorageDeals message
      # Accepts a decimal string (e.g., "123.45" or "123 fil") with optional "fil" or "attofil" suffix. (Default: "0.5 FIL")
      #
      # type: types.FIL
      #MaxPublishDealFee = "0.5 FIL"

      # ExpectedPoRepSealDuration is the expected time it would take to seal the deal sector
      # This will be used to fail the deals which cannot be sealed on time.
      # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "8h0m0s")
      #
      # type: time.Duration
      #ExpectedPoRepSealDuration = "8h0m0s"

      # ExpectedSnapSealDuration is the expected time it would take to snap the deal sector
      # This will be used to fail the deals which cannot be sealed on time.
      # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "2h0m0s")
      #
      # type: time.Duration
      #ExpectedSnapSealDuration = "2h0m0s"

      # SkipCommP can be used to skip doing a commP check before PublishDealMessage is sent on chain
      # Warning: If this check is skipped and there is a commP mismatch, all deals in the
      # sector will need to be sent again (Default: false)
      #
      # type: bool
      #SkipCommP = false

      # MaxConcurrentDealSizeGiB is a sum of all size of all deals which are waiting to be added to a sector
      # When the cumulative size of all deals in process reaches this number, new deals will be rejected.
      # (Default: 0 = unlimited)
      #
      # type: int64
      #MaxConcurrentDealSizeGiB = 0

      # DenyUnknownClients determines the default behaviour for the deal of clients which are not in allow/deny list
      # If True then all deals coming from unknown clients will be rejected. (Default: false)
      #
      # type: bool
      #DenyUnknownClients = false

      # DenyOnlineDeals determines if the storage provider will accept online deals (Default: false)
      #
      # type: bool
      #DenyOnlineDeals = false

      # DenyOfflineDeals determines if the storage provider will accept offline deals (Default: false)
      #
      # type: bool
      #DenyOfflineDeals = false

      # CIDGravityTokens is the list of authorization token to use for CIDGravity filters. These should be in format
      # "minerID1:Token1", "minerID2:Token2". If a token for a minerID within the cluster is not provided,
      # CIDGravity filters will not be applied to deals associated with that miner ID.
      #
      # type: []string
      #CIDGravityTokens = []

      # DefaultCIDGravityAccept when set to true till accept deals when CIDGravity service is not available.
      # Default behaviors is to reject the deals (Default: false)
      #
      # type: bool
      #DefaultCIDGravityAccept = false

    # MK20 encompasses all configuration related to deal protocol mk2.0 i.e. market 2.0
    #
    # type: MK20Config
    [Market.StorageMarketConfig.MK20]

      # ExpectedPoRepSealDuration is the expected time it would take to seal the deal sector
      # This will be used to fail the deals which cannot be sealed on time.
      # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "8h0m0s")
      #
      # type: time.Duration
      #ExpectedPoRepSealDuration = "8h0m0s"

      # ExpectedSnapSealDuration is the expected time it would take to snap the deal sector
      # This will be used to fail the deals which cannot be sealed on time.
      # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "2h0m0s")
      #
      # type: time.Duration
      #ExpectedSnapSealDuration = "2h0m0s"

      # SkipCommP can be used to skip doing a commP check before PublishDealMessage is sent on chain
      # Warning: If this check is skipped and there is a commP mismatch, all deals in the
      # sector will need to be sent again (Default: false)
      #
      # type: bool
      #SkipCommP = false

      # MaxConcurrentDealSizeGiB is a sum of all size of all deals which are waiting to be added to a sector
      # When the cumulative size of all deals in process reaches this number, new deals will be rejected.
      # (Default: 0 = unlimited)
      #
      # type: int64
      #MaxConcurrentDealSizeGiB = 0

      # DenyUnknownClients determines the default behaviour for the deal of clients which are not in allow/deny list
      # If True then all deals coming from unknown clients will be rejected. (Default: false)
      #
      # type: bool
      #DenyUnknownClients = false

      # MaxParallelChunkUploads defines the maximum number of upload operations that can run in parallel. (Default: 512)
      #
      # type: int
      #MaxParallelChunkUploads = 512

      # MinimumChunkSize defines the smallest size of a chunk allowed for processing, expressed in bytes. Must be a power of 2. (Default: 16 MiB)
      #
      # type: int64
      #MinimumChunkSize = 16777216

      # MaximumChunkSize defines the maximum size of a chunk allowed for processing, expressed in bytes. Must be a power of 2. (Default: 256 MiB)
      #
      # type: int64
      #MaximumChunkSize = 268435456

    # IPNI configuration for ipni-provider
    #
    # type: IPNIConfig
    [Market.StorageMarketConfig.IPNI]

      # Disable set whether to disable indexing announcement to the network and expose endpoints that
      # allow indexer nodes to process announcements. Default: False
      #
      # type: bool
      #Disable = false

      # The network indexer web UI URL for viewing published announcements
      #
      # type: []string
      #ServiceURL = ["https://cid.contact"]

      # The list of URLs of indexing nodes to announce to. This is a list of hosts we talk to tell them about new
      # heads.
      #
      # type: []string
      #DirectAnnounceURLs = ["https://cid.contact/ingest/announce"]

    # Indexing configuration for deal indexing
    #
    # type: IndexingConfig
    [Market.StorageMarketConfig.Indexing]

      # Number of records per insert batch
      #
      # type: int
      #InsertBatchSize = 1000

      # Number of concurrent inserts to split AddIndex calls to
      #
      # type: int
      #InsertConcurrency = 10


# Ingest defines configuration parameters for handling and limiting deal ingestion pipelines within the Curio node.
#
# type: CurioIngestConfig
[Ingest]

  # MaxMarketRunningPipelines is the maximum number of market pipelines that can be actively running tasks.
  # A "running" pipeline is one that has at least one task currently assigned to a machine (owner_id is not null).
  # If this limit is exceeded, the system will apply backpressure to delay processing of new deals.
  # 0 means unlimited. (Default: 64)
  # Updates will affect running instances.
  #
  # type: int
  #MaxMarketRunningPipelines = 64

  # MaxQueueDownload is the maximum number of pipelines that can be queued at the downloading stage,
  # waiting for a machine to pick up their task (owner_id is null).
  # If this limit is exceeded, the system will apply backpressure to slow the ingestion of new deals.
  # 0 means unlimited. (Default: 8)
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueueDownload = 8

  # MaxQueueCommP is the maximum number of pipelines that can be queued at the CommP (verify) stage,
  # waiting for a machine to pick up their verification task (owner_id is null).
  # If this limit is exceeded, the system will apply backpressure, delaying new deal processing.
  # 0 means unlimited. (Default: 8)
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueueCommP = 8

  # Maximum number of sectors that can be queued waiting for deals to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # The DealSector queue includes deals that are ready to enter the sealing pipeline but are not yet part of it.
  # DealSector queue is the first queue in the sealing pipeline, making it the primary backpressure mechanism. (Default: 8)
  # Updates will affect running instances.
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueueDealSector = 8

  # Maximum number of sectors that can be queued waiting for SDR to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # The SDR queue includes deals which are in the process of entering the sealing pipeline. In case of the SDR tasks it is
  # possible that this queue grows more than this limit(CC sectors), the backpressure is only applied to sectors
  # entering the pipeline.
  # Only applies to PoRep pipeline (DoSnap = false) (Default: 8)
  # Updates will affect running instances.
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueueSDR = 8

  # Maximum number of sectors that can be queued waiting for SDRTrees to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # In case of the trees tasks it is possible that this queue grows more than this limit, the backpressure is only
  # applied to sectors entering the pipeline.
  # Only applies to PoRep pipeline (DoSnap = false) (Default: 0)
  # Updates will affect running instances.
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueueTrees = 0

  # Maximum number of sectors that can be queued waiting for PoRep to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # Like with the trees tasks, it is possible that this queue grows more than this limit, the backpressure is only
  # applied to sectors entering the pipeline.
  # Only applies to PoRep pipeline (DoSnap = false) (Default: 0)
  # Updates will affect running instances.
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueuePoRep = 0

  # MaxQueueSnapEncode is the maximum number of sectors that can be queued waiting for UpdateEncode tasks to start.
  # 0 means unlimited.
  # This applies backpressure to the market subsystem by delaying the ingestion of deal data.
  # Only applies to the Snap Deals pipeline (DoSnap = true). (Default: 16)
  # Updates will affect running instances.
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueueSnapEncode = 16

  # MaxQueueSnapProve is the maximum number of sectors that can be queued waiting for UpdateProve to start processing.
  # 0 means unlimited.
  # This applies backpressure in the Snap Deals pipeline (DoSnap = true) by delaying new deal ingestion. (Default: 0)
  # Updates will affect running instances.
  # Updates will affect running instances.
  #
  # type: int
  #MaxQueueSnapProve = 0

  # Maximum time an open deal sector should wait for more deals before it starts sealing.
  # This ensures that sectors don't remain open indefinitely, consuming resources.
  # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "1h0m0s")
  # Updates will affect running instances.
  # Updates will affect running instances.
  #
  # type: time.Duration
  #MaxDealWaitTime = "1h0m0s"

  # DoSnap, when set to true, enables snap deal processing for deals ingested by this instance.
  # Unlike lotus-miner, there is no fallback to PoRep when no snap sectors are available.
  # When enabled, all deals will be processed as snap deals. (Default: false)
  # Updates will affect running instances.
  #
  # type: bool
  #DoSnap = false


# Seal defines the configuration related to the sealing process in Curio.
#
# type: CurioSealConfig
[Seal]

  # BatchSealSectorSize Allows setting the sector size supported by the batch seal task.
  # Can be any value as long as it is "32GiB". (Default: "32GiB")
  #
  # type: string
  #BatchSealSectorSize = "32GiB"

  # Number of sectors in a seal batch. Depends on hardware and supraseal configuration. (Default: 32)
  #
  # type: int
  #BatchSealBatchSize = 32

  # Number of parallel pipelines. Can be 1 or 2. Depends on available raw block storage (Default: 2)
  #
  # type: int
  #BatchSealPipelines = 2

  # SingleHasherPerThread is a compatibility flag for older CPUs. Zen3 and later supports two sectors per thread.
  # Set to false for older CPUs (Zen 2 and before). (Default: false)
  #
  # type: bool
  #SingleHasherPerThread = false


# Apis defines the configuration for API-related settings in the Curio system.
#
# type: ApisConfig
[Apis]

  # API auth secret for the Curio nodes to use. This value should only be set on the bade layer.
  #
  # type: string
  #StorageRPCSecret = ""


# Alerting specifies configuration settings for alerting mechanisms, including thresholds and external integrations.
#
# type: CurioAlertingConfig
[Alerting]

  # MinimumWalletBalance is the minimum balance all active wallets. If the balance is below this value, an
  # alerts will be triggered for the wallet
  # Accepts a decimal string (e.g., "123.45" or "123 fil") with optional "fil" or "attofil" suffix. (Default: "5 FIL")
  #
  # type: types.FIL
  #MinimumWalletBalance = "5 FIL"

  # PagerDutyConfig is the configuration for the PagerDuty alerting integration.
  #
  # type: PagerDutyConfig
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

  # PrometheusAlertManagerConfig is the configuration for the Prometheus AlertManager alerting integration.
  #
  # type: PrometheusAlertManagerConfig
  [Alerting.PrometheusAlertManager]

    # Enable is a flag to enable or disable the Prometheus AlertManager integration.
    #
    # type: bool
    #Enable = false

    # AlertManagerURL is the URL for the Prometheus AlertManager API v2 URL.
    #
    # type: string
    #AlertManagerURL = "http://localhost:9093/api/v2/alerts"

  # SlackWebhookConfig is a configuration type for Slack webhook integration.
  #
  # type: SlackWebhookConfig
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


# Batching represents the batching configuration for pre-commit, commit, and update operations.
#
# type: CurioBatchingConfig
[Batching]

  # Precommit Batching configuration
  #
  # type: PreCommitBatchingConfig
  [Batching.PreCommit]

    # Base fee value below which we should try to send Precommit messages immediately
    # Accepts a decimal string (e.g., "123.45" or "123 fil") with optional "fil" or "attofil" suffix. (Default: "0.005 FIL")
    #
    # type: types.FIL
    #BaseFeeThreshold = "0.005 FIL"

    # Maximum amount of time any given sector in the batch can wait for the batch to accumulate
    # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "4h0m0s")
    #
    # type: time.Duration
    #Timeout = "4h0m0s"

    # Time buffer for forceful batch submission before sectors/deal in batch would start expiring
    # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "6h0m0s")
    #
    # type: time.Duration
    #Slack = "6h0m0s"

  # Commit batching configuration
  #
  # type: CommitBatchingConfig
  [Batching.Commit]

    # Base fee value below which we should try to send Commit messages immediately
    # Accepts a decimal string (e.g., "123.45" or "123 fil") with optional "fil" or "attofil" suffix. (Default: "0.005 FIL")
    #
    # type: types.FIL
    #BaseFeeThreshold = "0.005 FIL"

    # Maximum amount of time any given sector in the batch can wait for the batch to accumulate
    # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "1h0m0s")
    #
    # type: time.Duration
    #Timeout = "1h0m0s"

    # Time buffer for forceful batch submission before sectors/deals in batch would start expiring
    # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "1h0m0s")
    #
    # type: time.Duration
    #Slack = "1h0m0s"

  # Snap Deals batching configuration
  #
  # type: UpdateBatchingConfig
  [Batching.Update]

    # Base fee value below which we should try to send Commit messages immediately
    # Accepts a decimal string (e.g., "123.45" or "123 fil") with optional "fil" or "attofil" suffix. (Default: "0.005 FIL")
    #
    # type: types.FIL
    #BaseFeeThreshold = "0.005 FIL"

    # Maximum amount of time any given sector in the batch can wait for the batch to accumulate
    # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "1h0m0s")
    #
    # type: time.Duration
    #Timeout = "1h0m0s"

    # Time buffer for forceful batch submission before sectors/deals in batch would start expiring
    # Time duration string (e.g., "1h2m3s") in TOML format. (Default: "1h0m0s")
    #
    # type: time.Duration
    #Slack = "1h0m0s"

```
