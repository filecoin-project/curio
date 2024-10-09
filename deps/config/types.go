package config

import (
	"time"

	"github.com/filecoin-project/lotus/chain/types"
)

func DefaultCurioConfig() *CurioConfig {
	return &CurioConfig{
		Subsystems: CurioSubsystemsConfig{
			GuiAddress:                 "0.0.0.0:4701",
			BoostAdapters:              []string{},
			RequireActivationSuccess:   true,
			RequireNotificationSuccess: true,
		},
		Fees: CurioFees{
			DefaultMaxFee:      DefaultDefaultMaxFee(),
			MaxPreCommitGasFee: types.MustParseFIL("0.025"),
			MaxCommitGasFee:    types.MustParseFIL("0.05"),

			MaxPreCommitBatchGasFee: BatchFeeConfig{
				Base:      types.MustParseFIL("0"),
				PerSector: types.MustParseFIL("0.02"),
			},
			MaxCommitBatchGasFee: BatchFeeConfig{
				Base:      types.MustParseFIL("0"),
				PerSector: types.MustParseFIL("0.03"), // enough for 6 agg and 1nFIL base fee
			},

			MaxTerminateGasFee:         types.MustParseFIL("0.5"),
			MaxWindowPoStGasFee:        types.MustParseFIL("5"),
			MaxPublishDealsFee:         types.MustParseFIL("0.05"),
			CollateralFromMinerBalance: false,
			DisableCollateralFallback:  false,
		},
		Addresses: []CurioAddresses{{
			PreCommitControl: []string{},
			CommitControl:    []string{},
			TerminateControl: []string{},
			MinerAddresses:   []string{},
		}},
		Proving: CurioProvingConfig{
			ParallelCheckLimit:    32,
			PartitionCheckTimeout: Duration(20 * time.Minute),
			SingleCheckTimeout:    Duration(10 * time.Minute),
		},
		Seal: CurioSealConfig{
			BatchSealPipelines:  2,
			BatchSealBatchSize:  32,
			BatchSealSectorSize: "32GiB",
		},
		Ingest: CurioIngestConfig{
			MaxQueueDealSector: 8, // default to 8 sectors open(or in process of opening) for deals
			MaxQueueSDR:        8, // default to 8 (will cause backpressure even if deal sectors are 0)
			MaxQueueTrees:      0, // default don't use this limit
			MaxQueuePoRep:      0, // default don't use this limit

			MaxQueueSnapEncode: 16,
			MaxQueueSnapProve:  0,

			MaxDealWaitTime: Duration(1 * time.Hour),
		},
		Alerting: CurioAlertingConfig{
			MinimumWalletBalance: types.MustParseFIL("5"),
			PagerDuty: PagerDutyConfig{
				PagerDutyEventURL: "https://events.pagerduty.com/v2/enqueue",
			},
			PrometheusAlertManager: PrometheusAlertManagerConfig{
				AlertManagerURL: "http://localhost:9093/api/v2/alerts",
			},
		},
	}
}

type CurioConfig struct {
	Subsystems CurioSubsystemsConfig

	Fees CurioFees

	// Addresses of wallets per MinerAddress (one of the fields).
	Addresses []CurioAddresses
	Proving   CurioProvingConfig
	Ingest    CurioIngestConfig
	Seal      CurioSealConfig
	Apis      ApisConfig
	Alerting  CurioAlertingConfig
}

func DefaultDefaultMaxFee() types.FIL {
	return types.MustParseFIL("0.07")
}

type BatchFeeConfig struct {
	Base      types.FIL
	PerSector types.FIL
}

type CurioSubsystemsConfig struct {
	// EnableWindowPost enables window post to be executed on this curio instance. Each machine in the cluster
	// with WindowPoSt enabled will also participate in the window post scheduler. It is possible to have multiple
	// machines with WindowPoSt enabled which will provide redundancy, and in case of multiple partitions per deadline,
	// will allow for parallel processing of partitions.
	//
	// It is possible to have instances handling both WindowPoSt and WinningPoSt, which can provide redundancy without
	// the need for additional machines. In setups like this it is generally recommended to run
	// partitionsPerDeadline+1 machines.
	EnableWindowPost   bool
	WindowPostMaxTasks int

	// EnableWinningPost enables winning post to be executed on this curio instance.
	// Each machine in the cluster with WinningPoSt enabled will also participate in the winning post scheduler.
	// It is possible to mix machines with WindowPoSt and WinningPoSt enabled, for details see the EnableWindowPost
	// documentation.
	EnableWinningPost   bool
	WinningPostMaxTasks int

	// EnableParkPiece enables the "piece parking" task to run on this node. This task is responsible for fetching
	// pieces from the network and storing them in the storage subsystem until sectors are sealed. This task is
	// only applicable when integrating with boost, and should be enabled on nodes which will hold deal data
	// from boost until sectors containing the related pieces have the TreeD/TreeR constructed.
	// Note that future Curio implementations will have a separate task type for fetching pieces from the internet.
	EnableParkPiece   bool
	ParkPieceMaxTasks int

	// EnableSealSDR enables SDR tasks to run. SDR is the long sequential computation
	// creating 11 layer files in sector cache directory.
	//
	// SDR is the first task in the sealing pipeline. It's inputs are just the hash of the
	// unsealed data (CommD), sector number, miner id, and the seal proof type.
	// It's outputs are the 11 layer files in the sector cache directory.
	//
	// In lotus-miner this was run as part of PreCommit1.
	EnableSealSDR bool

	// The maximum amount of SDR tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine.
	SealSDRMaxTasks int

	// The maximum amount of SDR tasks that need to be queued before the system will start accepting new tasks.
	// The main purpose of this setting is to allow for enough tasks to accumulate for batch sealing. When batch sealing
	// nodes are present in the cluster, this value should be set to batch_size+1 to allow for the batch sealing node to
	// fill up the batch.
	// This setting can also be used to give priority to other nodes in the cluster by setting this value to a higher
	// value on the nodes which should have less priority.
	SealSDRMinTasks int

	// EnableSealSDRTrees enables the SDR pipeline tree-building task to run.
	// This task handles encoding of unsealed data into last sdr layer and building
	// of TreeR, TreeC and TreeD.
	//
	// This task runs after SDR
	// TreeD is first computed with optional input of unsealed data
	// TreeR is computed from replica, which is first computed as field
	//   addition of the last SDR layer and the bottom layer of TreeD (which is the unsealed data)
	// TreeC is computed from the 11 SDR layers
	// The 3 trees will later be used to compute the PoRep proof.
	//
	// In case of SyntheticPoRep challenges for PoRep will be pre-generated at this step, and trees and layers
	// will be dropped. SyntheticPoRep works by pre-generating a very large set of challenges (~30GiB on disk)
	// then using a small subset of them for the actual PoRep computation. This allows for significant scratch space
	// saving between PreCommit and PoRep generation at the expense of more computation (generating challenges in this step)
	//
	// In lotus-miner this was run as part of PreCommit2 (TreeD was run in PreCommit1).
	// Note that nodes with SDRTrees enabled will also answer to Finalize tasks,
	// which just remove unneeded tree data after PoRep is computed.
	EnableSealSDRTrees bool

	// The maximum amount of SealSDRTrees tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine.
	SealSDRTreesMaxTasks int

	// FinalizeMaxTasks is the maximum amount of finalize tasks that can run simultaneously.
	// The finalize task is enabled on all machines which also handle SDRTrees tasks. Finalize ALWAYS runs on whichever
	// machine holds sector cache files, as it removes unneeded tree data after PoRep is computed.
	// Finalize will run in parallel with the SubmitCommitMsg task.
	FinalizeMaxTasks int

	// EnableSendPrecommitMsg enables the sending of precommit messages to the chain
	// from this curio instance.
	// This runs after SDRTrees and uses the output CommD / CommR (roots of TreeD / TreeR) for the message
	EnableSendPrecommitMsg bool

	// EnablePoRepProof enables the computation of the porep proof
	//
	// This task runs after interactive-porep seed becomes available, which happens 150 epochs (75min) after the
	// precommit message lands on chain. This task should run on a machine with a GPU. Vanilla PoRep proofs are
	// requested from the machine which holds sector cache files which most likely is the machine which ran the SDRTrees
	// task.
	//
	// In lotus-miner this was Commit1 / Commit2
	EnablePoRepProof bool

	// The maximum amount of PoRepProof tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine.
	PoRepProofMaxTasks int

	// EnableSendCommitMsg enables the sending of commit messages to the chain
	// from this curio instance.
	EnableSendCommitMsg bool

	// Whether to abort if any sector activation in a batch fails (newly sealed sectors, only with ProveCommitSectors3).
	RequireActivationSuccess bool
	// Whether to abort if any sector activation in a batch fails (updating sectors, only with ProveReplicaUpdates3).
	RequireNotificationSuccess bool

	// EnableMoveStorage enables the move-into-long-term-storage task to run on this curio instance.
	// This tasks should only be enabled on nodes with long-term storage.
	//
	// The MoveStorage task is the last task in the sealing pipeline. It moves the sealed sector data from the
	// SDRTrees machine into long-term storage. This task runs after the Finalize task.
	EnableMoveStorage bool

	// NoUnsealedDecode disables the decoding sector data on this node. Normally data encoding is enabled by default on
	// storage nodes with the MoveStorage task enabled. Setting this option to true means that unsealed data for sectors
	// will not be stored on this node
	NoUnsealedDecode bool

	// The maximum amount of MoveStorage tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine. It is recommended that this value is set to a number which
	// uses all available network (or disk) bandwidth on the machine without causing bottlenecks.
	MoveStorageMaxTasks int

	// EnableUpdateEncode enables the encoding step of the SnapDeal process on this curio instance.
	// This step involves encoding the data into the sector and computing updated TreeR (uses gpu).
	EnableUpdateEncode bool

	// EnableUpdateProve enables the proving step of the SnapDeal process on this curio instance.
	// This step generates the snark proof for the updated sector.
	EnableUpdateProve bool

	// EnableUpdateSubmit enables the submission of SnapDeal proofs to the blockchain from this curio instance.
	// This step submits the generated proofs to the chain.
	EnableUpdateSubmit bool

	// UpdateEncodeMaxTasks sets the maximum number of concurrent SnapDeal encoding tasks that can run on this instance.
	UpdateEncodeMaxTasks int

	// UpdateProveMaxTasks sets the maximum number of concurrent SnapDeal proving tasks that can run on this instance.
	UpdateProveMaxTasks int

	// EnableCommP enabled the commP task on te node. CommP is calculated before sending PublishDealMessage for a Mk12
	// deal, and when checking sector data with 'curio unseal check'.
	EnableCommP bool

	// BoostAdapters is a list of tuples of miner address and port/ip to listen for market (e.g. boost) requests.
	// This interface is compatible with the lotus-miner RPC, implementing a subset needed for storage market operations.
	// Strings should be in the format "actor:ip:port". IP cannot be 0.0.0.0. We recommend using a private IP.
	// Example: "f0123:127.0.0.1:32100". Multiple addresses can be specified.
	//
	// When a market node like boost gives Curio's market RPC a deal to placing into a sector, Curio will first store the
	// deal data in a temporary location "Piece Park" before assigning it to a sector. This requires that at least one
	// node in the cluster has the EnableParkPiece option enabled and has sufficient scratch space to store the deal data.
	// This is different from lotus-miner which stored the deal data into an "unsealed" sector as soon as the deal was
	// received. Deal data in PiecePark is accessed when the sector TreeD and TreeR are computed, but isn't needed for
	// the initial SDR layers computation. Pieces in PiecePark are removed after all sectors referencing the piece are
	// sealed.
	//
	// To get API info for boost configuration run 'curio market rpc-info'
	//
	// NOTE: All deal data will flow through this service, so it should be placed on a machine running boost or on
	// a machine which handles ParkPiece tasks.
	BoostAdapters []string

	// EnableWebGui enables the web GUI on this curio instance. The UI has minimal local overhead, but it should
	// only need to be run on a single machine in the cluster.
	EnableWebGui bool

	// The address that should listen for Web GUI requests.
	GuiAddress string

	// UseSyntheticPoRep enables the synthetic PoRep for all new sectors. When set to true, will reduce the amount of
	// cache data held on disk after the completion of TreeRC task to 11GiB.
	UseSyntheticPoRep bool

	// The maximum amount of SyntheticPoRep tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine.
	SyntheticPoRepMaxTasks int

	// Batch Seal
	EnableBatchSeal bool
}
type CurioFees struct {
	DefaultMaxFee      types.FIL
	MaxPreCommitGasFee types.FIL
	MaxCommitGasFee    types.FIL

	// maxBatchFee = maxBase + maxPerSector * nSectors
	MaxPreCommitBatchGasFee BatchFeeConfig
	MaxCommitBatchGasFee    BatchFeeConfig

	MaxTerminateGasFee types.FIL
	// WindowPoSt is a high-value operation, so the default fee should be high.
	MaxWindowPoStGasFee types.FIL
	MaxPublishDealsFee  types.FIL
	// Whether to use available miner balance for sector collateral instead of sending it with each message
	CollateralFromMinerBalance bool
	// Don't send collateral with messages even if there is no available balance in the miner actor
	DisableCollateralFallback bool
}

type CurioAddresses struct {
	// Addresses to send PreCommit messages from
	PreCommitControl []string
	// Addresses to send Commit messages from
	CommitControl    []string
	TerminateControl []string

	// DisableOwnerFallback disables usage of the owner address for messages
	// sent automatically
	DisableOwnerFallback bool
	// DisableWorkerFallback disables usage of the worker address for messages
	// sent automatically, if control addresses are configured.
	// A control address that doesn't have enough funds will still be chosen
	// over the worker address if this flag is set.
	DisableWorkerFallback bool

	// MinerAddresses are the addresses of the miner actors to use for sending messages
	MinerAddresses []string
}

type CurioProvingConfig struct {
	// Maximum number of sector checks to run in parallel. (0 = unlimited)
	//
	// WARNING: Setting this value too high may make the node crash by running out of stack
	// WARNING: Setting this value too low may make sector challenge reading much slower, resulting in failed PoSt due
	// to late submission.
	//
	// After changing this option, confirm that the new value works in your setup by invoking
	// 'curio test wd task 0'
	ParallelCheckLimit int

	// Maximum amount of time a proving pre-check can take for a sector. If the check times out the sector will be skipped
	//
	// WARNING: Setting this value too low risks in sectors being skipped even though they are accessible, just reading the
	// test challenge took longer than this timeout
	// WARNING: Setting this value too high risks missing PoSt deadline in case IO operations related to this sector are
	// blocked (e.g. in case of disconnected NFS mount)
	SingleCheckTimeout Duration

	// Maximum amount of time a proving pre-check can take for an entire partition. If the check times out, sectors in
	// the partition which didn't get checked on time will be skipped
	//
	// WARNING: Setting this value too low risks in sectors being skipped even though they are accessible, just reading the
	// test challenge took longer than this timeout
	// WARNING: Setting this value too high risks missing PoSt deadline in case IO operations related to this partition are
	// blocked or slow
	PartitionCheckTimeout Duration
}

// Duration is a wrapper type for time.Duration
// for decoding and encoding from/to TOML
type Duration time.Duration

func (dur Duration) MarshalText() ([]byte, error) {
	d := time.Duration(dur)
	return []byte(d.String()), nil
}

// UnmarshalText implements interface for TOML decoding
func (dur *Duration) UnmarshalText(text []byte) error {
	d, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*dur = Duration(d)
	return err
}

type CurioIngestConfig struct {
	// Maximum number of sectors that can be queued waiting for deals to start processing.
	// 0 = unlimited
	// Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
	// The DealSector queue includes deals which are ready to enter the sealing pipeline but are not yet part of it -
	// size of this queue will also impact the maximum number of ParkPiece tasks which can run concurrently.
	// DealSector queue is the first queue in the sealing pipeline, meaning that it should be used as the primary backpressure mechanism.
	MaxQueueDealSector int

	// Maximum number of sectors that can be queued waiting for SDR to start processing.
	// 0 = unlimited
	// Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
	// The SDR queue includes deals which are in the process of entering the sealing pipeline. In case of the SDR tasks it is
	// possible that this queue grows more than this limit(CC sectors), the backpressure is only applied to sectors
	// entering the pipeline.
	// Only applies to PoRep pipeline (DoSnap = false)
	MaxQueueSDR int

	// Maximum number of sectors that can be queued waiting for SDRTrees to start processing.
	// 0 = unlimited
	// Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
	// In case of the trees tasks it is possible that this queue grows more than this limit, the backpressure is only
	// applied to sectors entering the pipeline.
	// Only applies to PoRep pipeline (DoSnap = false)
	MaxQueueTrees int

	// Maximum number of sectors that can be queued waiting for PoRep to start processing.
	// 0 = unlimited
	// Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
	// Like with the trees tasks, it is possible that this queue grows more than this limit, the backpressure is only
	// applied to sectors entering the pipeline.
	// Only applies to PoRep pipeline (DoSnap = false)
	MaxQueuePoRep int

	// MaxQueueSnapEncode is the maximum number of sectors that can be queued waiting for UpdateEncode to start processing.
	// 0 means unlimited.
	// This applies backpressure to the market subsystem by delaying the ingestion of deal data.
	// Only applies to the Snap Deals pipeline (DoSnap = true).
	MaxQueueSnapEncode int

	// MaxQueueSnapProve is the maximum number of sectors that can be queued waiting for UpdateProve to start processing.
	// 0 means unlimited.
	// This applies backpressure to the market subsystem by delaying the ingestion of deal data.
	// Only applies to the Snap Deals pipeline (DoSnap = true).
	MaxQueueSnapProve int

	// Maximum time an open deal sector should wait for more deal before it starts sealing
	MaxDealWaitTime Duration

	// DoSnap enables the snap deal process for deals ingested by this instance. Unlike in lotus-miner there is no
	// fallback to porep when no sectors are available to snap into. When enabled all deals will be snap deals.
	DoSnap bool
}

type CurioAlertingConfig struct {
	// MinimumWalletBalance is the minimum balance all active wallets. If the balance is below this value, an
	// alerts will be triggered for the wallet
	MinimumWalletBalance types.FIL

	// PagerDutyConfig is the configuration for the PagerDuty alerting integration.
	PagerDuty PagerDutyConfig

	// PrometheusAlertManagerConfig is the configuration for the Prometheus AlertManager alerting integration.
	PrometheusAlertManager PrometheusAlertManagerConfig

	// SlackWebhookConfig is a configuration type for Slack webhook integration.
	SlackWebhook SlackWebhookConfig
}

type CurioSealConfig struct {
	// BatchSealSectorSize Allows setting the sector size supported by the batch seal task.
	// Can be any value as long as it is "32GiB".
	BatchSealSectorSize string

	// Number of sectors in a seal batch. Depends on hardware and supraseal configuration.
	BatchSealBatchSize int

	// Number of parallel pipelines. Can be 1 or 2. Depends on available raw block storage
	BatchSealPipelines int

	// SingleHasherPerThread is a compatibility flag for older CPUs. Zen3 and later supports two sectors per thread.
	// Set to false for older CPUs (Zen 2 and before).
	SingleHasherPerThread bool

	// LayerNVMEDevices is a list of pcie device addresses that should be used for SDR layer storage.
	// The required storage is 11 * BatchSealBatchSize * BatchSealSectorSize * BatchSealPipelines
	// Total Read IOPS for optimal performance should be 10M+.
	// The devices MUST be NVMe devices, not used for anything else. Any data on the devices will be lost!
	//
	// It's recommend to define these settings in a per-machine layer, as the devices are machine-specific.
	//
	// Example: ["0000:01:00.0", "0000:01:00.1"]
	LayerNVMEDevices []string
}

type PagerDutyConfig struct {
	// Enable is a flag to enable or disable the PagerDuty integration.
	Enable bool

	// PagerDutyEventURL is URL for PagerDuty.com Events API v2 URL. Events sent to this API URL are ultimately
	// routed to a PagerDuty.com service and processed.
	// The default is sufficient for integration with the stock commercial PagerDuty.com company's service.
	PagerDutyEventURL string

	// PageDutyIntegrationKey is the integration key for a PagerDuty.com service. You can find this unique service
	// identifier in the integration page for the service.
	PageDutyIntegrationKey string
}

type PrometheusAlertManagerConfig struct {
	// Enable is a flag to enable or disable the Prometheus AlertManager integration.
	Enable bool

	// AlertManagerURL is the URL for the Prometheus AlertManager API v2 URL.
	AlertManagerURL string
}

type SlackWebhookConfig struct {
	// Enable is a flag to enable or disable the Prometheus AlertManager integration.
	Enable bool

	// WebHookURL is the URL for the URL for slack Webhook.
	// Example: https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX
	WebHookURL string
}

type ApisConfig struct {
	// ChainApiInfo is the API endpoint for the Lotus daemon.
	ChainApiInfo []string

	// Chain API auth secret for the Curio nodes to use.
	StorageRPCSecret string
}
