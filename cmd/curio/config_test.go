package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
)

var baseText = `
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

  # EnableDealMarket
  #
  # type: bool
  #EnableDealMarket = false


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
  # 'lotus-miner proving compute window-post 0'
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

  # Disable WindowPoSt provable sector readability checks.
  # 
  # In normal operation, when preparing to compute WindowPoSt, lotus-miner will perform a round of reading challenges
  # from all sectors to confirm that those sectors can be proven. Challenges read in this process are discarded, as
  # we're only interested in checking that sector data can be read.
  # 
  # When using builtin proof computation (no PoSt workers, and DisableBuiltinWindowPoSt is set to false), this process
  # can save a lot of time and compute resources in the case that some sectors are not readable - this is caused by
  # the builtin logic not skipping snark computation when some sectors need to be skipped.
  # 
  # When using PoSt workers, this process is mostly redundant, with PoSt workers challenges will be read once, and
  # if challenges for some sectors aren't readable, those sectors will just get skipped.
  # 
  # Disabling sector pre-checks will slightly reduce IO load when proving sectors, possibly resulting in shorter
  # time to produce window PoSt. In setups with good IO capabilities the effect of this option on proving time should
  # be negligible.
  # 
  # NOTE: It likely is a bad idea to disable sector pre-checks in setups with no PoSt workers.
  # 
  # NOTE: Even when this option is enabled, recovering sectors will be checked before recovery declaration message is
  # sent to the chain
  # 
  # After changing this option, confirm that the new value works in your setup by invoking
  # 'lotus-miner proving compute window-post 0'
  #
  # type: bool
  #DisableWDPoStPreChecks = false

  # Maximum number of partitions to prove in a single SubmitWindowPoSt messace. 0 = network limit (3 in nv21)
  # 
  # A single partition may contain up to 2349 32GiB sectors, or 2300 64GiB sectors.
  # //
  # Note that setting this value lower may result in less efficient gas use - more messages will be sent,
  # to prove each deadline, resulting in more total gas use (but each message will have lower gas limit)
  # 
  # Setting this value above the network limit has no effect
  #
  # type: int
  #MaxPartitionsPerPoStMessage = 0

  # In some cases when submitting DeclareFaultsRecovered messages,
  # there may be too many recoveries to fit in a BlockGasLimit.
  # In those cases it may be necessary to set this value to something low (eg 1);
  # Note that setting this value lower may result in less efficient gas use - more messages will be sent than needed,
  # resulting in more total gas use (but each message will have lower gas limit)
  #
  # type: int
  #MaxPartitionsPerRecoveryMessage = 0

  # Enable single partition per PoSt Message for partitions containing recovery sectors
  # 
  # In cases when submitting PoSt messages which contain recovering sectors, the default network limit may still be
  # too high to fit in the block gas limit. In those cases, it becomes useful to only house the single partition
  # with recovering sectors in the post message
  # 
  # Note that setting this value lower may result in less efficient gas use - more messages will be sent,
  # to prove each deadline, resulting in more total gas use (but each message will have lower gas limit)
  #
  # type: bool
  #SingleRecoveringPartitionPerPostMessage = false


[Market]
  [Market.DealMarketConfig]

    [[Market.DealMarketConfig.PieceLocator]]
      #URL = "https://localhost:9999"

      [Market.DealMarketConfig.PieceLocator.Headers]
        #Authorization = ["Basic YWRtaW46c2VjcmV0"]

    [Market.DealMarketConfig.MK12]
      # Miners is a list of miner to enable MK12 deals for
      #
      # type: []string
      #Miners = ["t01000"]

      # When a deal is ready to publish, the amount of time to wait for more
      # deals to be ready to publish before publishing them all as a batch
      #
      # type: Duration
      #PublishMsgPeriod = "0s"

      # The maximum number of deals to include in a single PublishStorageDeals
      # message
      #
      # type: uint64
      #MaxDealsPerPublishMsg = 0

      # The maximum collateral that the provider will put up against a deal,
      # as a multiplier of the minimum collateral bound
      # The maximum fee to pay when sending the PublishStorageDeals message
      #
      # type: types.FIL
      #MaxPublishDealsFee = "0 FIL"

      # ExpectedSealDuration is the expected time it would take to seal the deal sector
      # This will be used to fail the deals which cannot be sealed on time.
      # Please make sure to update this to shorter duration for snap deals
      #
      # type: Duration
      #ExpectedSealDuration = "0s"


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
  #
  # type: int
  #MaxQueueSDR = 8

  # Maximum number of sectors that can be queued waiting for SDRTrees to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # In case of the trees tasks it is possible that this queue grows more than this limit, the backpressure is only
  # applied to sectors entering the pipeline.
  #
  # type: int
  #MaxQueueTrees = 0

  # Maximum number of sectors that can be queued waiting for PoRep to start processing.
  # 0 = unlimited
  # Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
  # Like with the trees tasks, it is possible that this queue grows more than this limit, the backpressure is only
  # applied to sectors entering the pipeline.
  #
  # type: int
  #MaxQueuePoRep = 0

  # Maximum time an open deal sector should wait for more deal before it starts sealing
  #
  # type: Duration
  #MaxDealWaitTime = "1h0m0s"

  # DoSnap enables the snap deal process for deals ingested by this instance. Unlike in lotus-miner there is no
  # fallback to porep when no sectors are available to snap into. When enabled all deals will be snap deals.
  #
  # type: bool
  #DoSnap = false


[Apis]
  # RPC Secret for the storage subsystem.
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
`

func TestConfig(t *testing.T) {
	baseCfg := config.DefaultCurioConfig()

	addr1 := config.CurioAddresses{
		PreCommitControl:      []string{},
		CommitControl:         []string{},
		DealPublishControl:    []string{},
		TerminateControl:      []string{"t3qroiebizgkz7pvj26reg5r5mqiftrt5hjdske2jzjmlacqr2qj7ytjncreih2mvujxoypwpfusmwpipvxncq"},
		DisableOwnerFallback:  false,
		DisableWorkerFallback: false,
		MinerAddresses:        []string{"t01000"},
		BalanceManager:        config.DefaultBalanceManager(),
	}

	addr2 := config.CurioAddresses{
		MinerAddresses: []string{"t01001"},
		BalanceManager: config.DefaultBalanceManager(),
	}

	_, err := deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	baseCfg.Addresses = append(baseCfg.Addresses, addr1)
	baseCfg.Addresses = lo.Filter(baseCfg.Addresses, func(a config.CurioAddresses, _ int) bool {
		return len(a.MinerAddresses) > 0
	})

	_, err = config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)

	baseCfg.Addresses = append(baseCfg.Addresses, addr2)
	baseCfg.Addresses = lo.Filter(baseCfg.Addresses, func(a config.CurioAddresses, _ int) bool {
		return len(a.MinerAddresses) > 0
	})

	_, err = config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)

}

func TestCustomConfigDurationJson(t *testing.T) {
	ref := new(jsonschema.Reflector)
	ref.Mapper = func(i reflect.Type) *jsonschema.Schema {
		if i == reflect.TypeOf(time.Second) {
			return &jsonschema.Schema{
				Type:   "string",
				Format: "duration",
			}
		}
		return nil
	}

	sch := ref.Reflect(config.CurioConfig{})
	definitions := sch.Definitions["CurioProvingConfig"]
	prop, ok := definitions.Properties.Get("SingleCheckTimeout")
	require.True(t, ok)
	require.Equal(t, prop.Type, "string")
}

func TestTOMLDecoding(t *testing.T) {

	def1 := config.DefaultCurioConfig()

	text := `
[[Addresses]]
  MinerAddresses = ["t01002"]
  [Addresses.BalanceManager]
	[Addresses.BalanceManager.MK12Collateral]
	  CollateralLowThreshold = "100 FIL"

[[Addresses]]
  MinerAddresses = ["t01007"]
  [Addresses.BalanceManager]
	[Addresses.BalanceManager.MK12Collateral]
	  CollateralLowThreshold = "50 fil"

[Alerting]
  [Alerting.PagerDuty]
  [Alerting.PrometheusAlertManager]
  [Alerting.SlackWebhook]

[Apis]
  ChainApiInfo = ["eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.Er4BD7iisZ6KwdkjbXrxRlgvOnf_KClzo9Q6V7fvUYs:/dns/lotus/tcp/1234/http"]
  StorageRPCSecret = "E/RAavP4YYHzKqa+eGXE6LtYTrT5YpToCEHugbTXOoI="

[Batching]
  [Batching.Commit]
  [Batching.PreCommit]
    Timeout = "1h0m0s"
  [Batching.Update]

[Fees]
  [Fees.MaxCommitBatchGasFee]
  [Fees.MaxPreCommitBatchGasFee]
  [Fees.MaxUpdateBatchGasFee]

[HTTP]
  [HTTP.CompressionLevels]

[Ingest]

[Market]
  [Market.StorageMarketConfig]
	[Market.StorageMarketConfig.IPNI]
	[Market.StorageMarketConfig.Indexing]
	[Market.StorageMarketConfig.MK12]

[Proving]

[Seal]

[Subsystems]
`

	_, err := deps.LoadConfigWithUpgrades(text, def1)
	require.NoError(t, err)

	cb, err := config.ConfigUpdate(def1, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)

	fmt.Println(string(cb))
}
