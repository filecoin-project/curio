package config

import (
	"net/http"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/lotus/chain/types"
)

func DefaultCurioConfig() *CurioConfig {
	return &CurioConfig{
		Subsystems: CurioSubsystemsConfig{
			GuiAddress:                 "0.0.0.0:4701",
			RequireActivationSuccess:   true,
			RequireNotificationSuccess: true,
			IndexingMaxTasks:           8,
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
			MaxUpdateBatchGasFee: BatchFeeConfig{
				Base:      types.MustParseFIL("0"),
				PerSector: types.MustParseFIL("0.03"),
			},

			MaxTerminateGasFee:         types.MustParseFIL("0.5"),
			MaxWindowPoStGasFee:        types.MustParseFIL("5"),
			MaxPublishDealsFee:         types.MustParseFIL("0.05"),
			CollateralFromMinerBalance: false,
			DisableCollateralFallback:  false,
		},
		Addresses: []CurioAddresses{{
			PreCommitControl:   []string{},
			CommitControl:      []string{},
			DealPublishControl: []string{},
			TerminateControl:   []string{},
			MinerAddresses:     []string{},
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
			MaxMarketRunningPipelines: 64,
			MaxQueueDownload:          8,
			MaxQueueCommP:             8,

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
		Batching: CurioBatchingConfig{
			PreCommit: PreCommitBatchingConfig{
				BaseFeeThreshold: types.MustParseFIL("0.005"),
				Timeout:          Duration(4 * time.Hour),
				Slack:            Duration(6 * time.Hour),
			},
			Commit: CommitBatchingConfig{
				BaseFeeThreshold: types.MustParseFIL("0.005"),
				Timeout:          Duration(1 * time.Hour),
				Slack:            Duration(1 * time.Hour),
			},
			Update: UpdateBatchingConfig{
				BaseFeeThreshold: types.MustParseFIL("0.005"),
				Timeout:          Duration(1 * time.Hour),
				Slack:            Duration(1 * time.Hour),
			},
		},
		Market: MarketConfig{
			StorageMarketConfig: StorageMarketConfig{
				PieceLocator: []PieceLocatorConfig{},
				Indexing: IndexingConfig{
					InsertConcurrency: 10,
					InsertBatchSize:   1000,
				},
				MK12: MK12Config{
					PublishMsgPeriod:          Duration(5 * time.Minute),
					MaxDealsPerPublishMsg:     8,
					MaxPublishDealFee:         types.MustParseFIL("0.5 FIL"),
					ExpectedPoRepSealDuration: Duration(8 * time.Hour),
					ExpectedSnapSealDuration:  Duration(2 * time.Hour),
				},
				IPNI: IPNIConfig{
					ServiceURL:         []string{"https://cid.contact"},
					DirectAnnounceURLs: []string{"https://cid.contact/ingest/announce"},
				},
			},
		},
		HTTP: HTTPConfig{
			DomainName:        "",
			ListenAddress:     "0.0.0.0:12310",
			ReadTimeout:       time.Second * 10,
			IdleTimeout:       time.Minute * 2,
			ReadHeaderTimeout: time.Second * 5,
			EnableCORS:        true,
			CompressionLevels: CompressionConfig{
				GzipLevel:    6,
				BrotliLevel:  4,
				DeflateLevel: 6,
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
	HTTP      HTTPConfig
	Market    MarketConfig
	Ingest    CurioIngestConfig
	Seal      CurioSealConfig
	Apis      ApisConfig
	Alerting  CurioAlertingConfig
	Batching  CurioBatchingConfig
}

func DefaultDefaultMaxFee() types.FIL {
	return types.MustParseFIL("0.07")
}

type BatchFeeConfig struct {
	Base      types.FIL
	PerSector types.FIL
}

func (b *BatchFeeConfig) FeeForSectors(nSectors int) abi.TokenAmount {
	return big.Add(big.Int(b.Base), big.Mul(big.NewInt(int64(nSectors)), big.Int(b.PerSector)))
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

	// EnableDealMarket enabled the deal market on the node. This would also enable libp2p on the node, if configured.
	EnableDealMarket bool

	// EnableCommP enables the commP task on te node. CommP is calculated before sending PublishDealMessage for a Mk12 deal
	// Must have EnableDealMarket = True
	EnableCommP bool

	// The maximum amount of CommP tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine.
	CommPMaxTasks int

	// The maximum amount of indexing and IPNI tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine.
	IndexingMaxTasks int

	// EnableProofShare enables the proof share tasks on the node.
	EnableProofShare bool

	// The maximum amount of ProofShare tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine.
	ProofShareMaxTasks int
}
type CurioFees struct {
	DefaultMaxFee      types.FIL
	MaxPreCommitGasFee types.FIL
	MaxCommitGasFee    types.FIL

	// maxBatchFee = maxBase + maxPerSector * nSectors
	MaxPreCommitBatchGasFee BatchFeeConfig
	MaxCommitBatchGasFee    BatchFeeConfig
	MaxUpdateBatchGasFee    BatchFeeConfig

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
	CommitControl []string
	// Address to send the deal collateral from with PublishStorageDeal Message
	DealPublishControl []string
	TerminateControl   []string

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
	// MaxMarketRunningPipelines is the maximum number of market pipelines that can be actively running tasks.
	// A "running" pipeline is one that has at least one task currently assigned to a machine (owner_id is not null).
	// If this limit is exceeded, the system will apply backpressure to delay processing of new deals.
	// 0 means unlimited.
	MaxMarketRunningPipelines int

	// MaxQueueDownload is the maximum number of pipelines that can be queued at the downloading stage,
	// waiting for a machine to pick up their task (owner_id is null).
	// If this limit is exceeded, the system will apply backpressure to slow the ingestion of new deals.
	// 0 means unlimited.
	MaxQueueDownload int

	// MaxQueueCommP is the maximum number of pipelines that can be queued at the CommP (verify) stage,
	// waiting for a machine to pick up their verification task (owner_id is null).
	// If this limit is exceeded, the system will apply backpressure, delaying new deal processing.
	// 0 means unlimited.
	MaxQueueCommP int

	// Maximum number of sectors that can be queued waiting for deals to start processing.
	// 0 = unlimited
	// Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
	// The DealSector queue includes deals that are ready to enter the sealing pipeline but are not yet part of it.
	// DealSector queue is the first queue in the sealing pipeline, making it the primary backpressure mechanism.
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

	// MaxQueueSnapEncode is the maximum number of sectors that can be queued waiting for UpdateEncode tasks to start.
	// 0 means unlimited.
	// This applies backpressure to the market subsystem by delaying the ingestion of deal data.
	// Only applies to the Snap Deals pipeline (DoSnap = true).
	MaxQueueSnapEncode int

	// MaxQueueSnapProve is the maximum number of sectors that can be queued waiting for UpdateProve to start processing.
	// 0 means unlimited.
	// This applies backpressure in the Snap Deals pipeline (DoSnap = true) by delaying new deal ingestion.
	MaxQueueSnapProve int

	// Maximum time an open deal sector should wait for more deals before it starts sealing.
	// This ensures that sectors don't remain open indefinitely, consuming resources.
	MaxDealWaitTime Duration

	// DoSnap, when set to true, enables snap deal processing for deals ingested by this instance.
	// Unlike lotus-miner, there is no fallback to PoRep when no snap sectors are available.
	// When enabled, all deals will be processed as snap deals.
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

type CurioBatchingConfig struct {
	// Precommit Batching configuration
	PreCommit PreCommitBatchingConfig

	// Commit batching configuration
	Commit CommitBatchingConfig

	// Snap Deals batching configuration
	Update UpdateBatchingConfig
}

type PreCommitBatchingConfig struct {
	// Base fee value below which we should try to send Precommit messages immediately
	BaseFeeThreshold types.FIL

	// Maximum amount of time any given sector in the batch can wait for the batch to accumulate
	Timeout Duration

	// Time buffer for forceful batch submission before sectors/deal in batch would start expiring
	Slack Duration
}

type CommitBatchingConfig struct {
	// Base fee value below which we should try to send Commit messages immediately
	BaseFeeThreshold types.FIL

	// Maximum amount of time any given sector in the batch can wait for the batch to accumulate
	Timeout Duration

	// Time buffer for forceful batch submission before sectors/deals in batch would start expiring
	Slack Duration
}

type UpdateBatchingConfig struct {
	// Base fee value below which we should try to send Commit messages immediately
	BaseFeeThreshold types.FIL

	// Maximum amount of time any given sector in the batch can wait for the batch to accumulate
	Timeout Duration

	// Time buffer for forceful batch submission before sectors/deals in batch would start expiring
	Slack Duration
}

type MarketConfig struct {
	// StorageMarketConfig houses all the deal related market configuration
	StorageMarketConfig StorageMarketConfig
}

type StorageMarketConfig struct {
	// MK12 encompasses all configuration related to deal protocol mk1.2.0 and mk1.2.1 (i.e. Boost deals)
	MK12 MK12Config

	// IPNI configuration for ipni-provider
	IPNI IPNIConfig

	// Indexing configuration for deal indexing
	Indexing IndexingConfig

	// PieceLocator is a list of HTTP url and headers combination to query for a piece for offline deals
	// User can run a remote file server which can host all the pieces over the HTTP and supply a reader when requested.
	// The server must support "HEAD" request and "GET" request.
	// 	1. <URL>?id=pieceCID with "HEAD" request responds with 200 if found or 404 if not. Must send header "Content-Length" with file size as value
	//  2. <URL>?id=pieceCID must provide a reader for the requested piece along with header "Content-Length" with file size as value
	PieceLocator []PieceLocatorConfig
}

type MK12Config struct {
	// When a deal is ready to publish, the amount of time to wait for more
	// deals to be ready to publish before publishing them all as a batch
	PublishMsgPeriod Duration

	// The maximum number of deals to include in a single PublishStorageDeals
	// message
	MaxDealsPerPublishMsg uint64

	// The maximum fee to pay per deal when sending the PublishStorageDeals message
	MaxPublishDealFee types.FIL

	// ExpectedPoRepSealDuration is the expected time it would take to seal the deal sector
	// This will be used to fail the deals which cannot be sealed on time.
	ExpectedPoRepSealDuration Duration

	// ExpectedSnapSealDuration is the expected time it would take to snap the deal sector
	// This will be used to fail the deals which cannot be sealed on time.
	ExpectedSnapSealDuration Duration

	// SkipCommP can be used to skip doing a commP check before PublishDealMessage is sent on chain
	// Warning: If this check is skipped and there is a commP mismatch, all deals in the
	// sector will need to be sent again
	SkipCommP bool

	// DisabledMiners is a list of miner addresses that should be excluded from online deal making protocols
	DisabledMiners []string

	// MaxConcurrentDealSizeGiB is a sum of all size of all deals which are waiting to be added to a sector
	// When the cumulative size of all deals in process reaches this number, new deals will be rejected.
	// (Default: 0 = unlimited)
	MaxConcurrentDealSizeGiB int64

	// DenyUnknownClients determines the default behaviour for the deal of clients which are not in allow/deny list
	// If True then all deals coming from unknown clients will be rejected.
	DenyUnknownClients bool

	// DenyOnlineDeals determines if the storage provider will accept online deals
	DenyOnlineDeals bool

	// DenyOfflineDeals determines if the storage provider will accept offline deals
	DenyOfflineDeals bool
}

type PieceLocatorConfig struct {
	URL     string
	Headers http.Header
}

type IndexingConfig struct {
	// Number of records per insert batch
	InsertBatchSize int

	// Number of concurrent inserts to split AddIndex calls to
	InsertConcurrency int
}

type IPNIConfig struct {
	// Disable set whether to disable indexing announcement to the network and expose endpoints that
	// allow indexer nodes to process announcements. Default: False
	Disable bool

	// The network indexer web UI URL for viewing published announcements
	// TODO: should we use this for checking published heads before publishing? Later commit
	ServiceURL []string

	// The list of URLs of indexing nodes to announce to. This is a list of hosts we talk to tell them about new
	// heads.
	DirectAnnounceURLs []string
}

// HTTPConfig represents the configuration for an HTTP server.
type HTTPConfig struct {
	// Enable the HTTP server on the node
	Enable bool

	// DomainName specifies the domain name that the server uses to serve HTTP requests. DomainName cannot be empty and cannot be
	// an IP address
	DomainName string

	// ListenAddress is the address that the server listens for HTTP requests.
	ListenAddress string

	// DelegateTLS allows the server to delegate TLS to a reverse proxy. When enabled the listen address will serve
	// HTTP and the reverse proxy will handle TLS termination.
	DelegateTLS bool

	// ReadTimeout is the maximum duration for reading the entire or next request, including body, from the client.
	ReadTimeout time.Duration

	// IdleTimeout is the maximum duration of an idle session. If set, idle connections are closed after this duration.
	IdleTimeout time.Duration

	// ReadHeaderTimeout is amount of time allowed to read request headers
	ReadHeaderTimeout time.Duration

	// EnableCORS indicates whether Cross-Origin Resource Sharing (CORS) is enabled or not.
	EnableCORS bool

	// CompressionLevels hold the compression level for various compression methods supported by the server
	CompressionLevels CompressionConfig
}

// CompressionConfig holds the compression levels for supported types
type CompressionConfig struct {
	GzipLevel    int
	BrotliLevel  int
	DeflateLevel int
}
