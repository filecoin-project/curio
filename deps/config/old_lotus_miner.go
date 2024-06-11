package config

import (
	"time"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/network"
	miner5 "github.com/filecoin-project/specs-actors/v5/actors/builtin/miner"

	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

type HarmonyDB struct {
	// HOSTS is a list of hostnames to nodes running YugabyteDB
	// in a cluster. Only 1 is required
	Hosts []string

	// The Yugabyte server's username with full credentials to operate on Lotus' Database. Blank for default.
	Username string

	// The password for the related username. Blank for default.
	Password string

	// The database (logical partition) within Yugabyte. Blank for default.
	Database string

	// The port to find Yugabyte. Blank for default.
	Port string
}

// StorageMiner is a miner config
type StorageMiner struct {
	Common

	Subsystems    MinerSubsystemConfig
	Dealmaking    DealmakingConfig
	IndexProvider IndexProviderConfig
	Proving       ProvingConfig
	Sealing       SealingConfig
	Storage       SealerConfig
	Fees          MinerFeeConfig
	Addresses     MinerAddressConfig
	DAGStore      DAGStoreConfig

	HarmonyDB HarmonyDB
}

type DAGStoreConfig struct {
	// Path to the dagstore root directory. This directory contains three
	// subdirectories, which can be symlinked to alternative locations if
	// need be:
	//  - ./transients: caches unsealed deals that have been fetched from the
	//    storage subsystem for serving retrievals.
	//  - ./indices: stores shard indices.
	//  - ./datastore: holds the KV store tracking the state of every shard
	//    known to the DAG store.
	// Default value: <LOTUS_MARKETS_PATH>/dagstore (split deployment) or
	// <LOTUS_MINER_PATH>/dagstore (monolith deployment)
	RootDir string

	// The maximum amount of indexing jobs that can run simultaneously.
	// 0 means unlimited.
	// Default value: 5.
	MaxConcurrentIndex int

	// The maximum amount of unsealed deals that can be fetched simultaneously
	// from the storage subsystem. 0 means unlimited.
	// Default value: 0 (unlimited).
	MaxConcurrentReadyFetches int

	// The maximum amount of unseals that can be processed simultaneously
	// from the storage subsystem. 0 means unlimited.
	// Default value: 0 (unlimited).
	MaxConcurrentUnseals int

	// The maximum number of simultaneous inflight API calls to the storage
	// subsystem.
	// Default value: 100.
	MaxConcurrencyStorageCalls int

	// The time between calls to periodic dagstore GC, in time.Duration string
	// representation, e.g. 1m, 5m, 1h.
	// Default value: 1 minute.
	GCInterval Duration
}

type MinerAddressConfig struct {
	// Addresses to send PreCommit messages from
	PreCommitControl []string
	// Addresses to send Commit messages from
	CommitControl      []string
	TerminateControl   []string
	DealPublishControl []string

	// DisableOwnerFallback disables usage of the owner address for messages
	// sent automatically
	DisableOwnerFallback bool
	// DisableWorkerFallback disables usage of the worker address for messages
	// sent automatically, if control addresses are configured.
	// A control address that doesn't have enough funds will still be chosen
	// over the worker address if this flag is set.
	DisableWorkerFallback bool
}
type MinerFeeConfig struct {
	MaxPreCommitGasFee types.FIL
	MaxCommitGasFee    types.FIL

	// maxBatchFee = maxBase + maxPerSector * nSectors
	MaxPreCommitBatchGasFee BatchFeeConfig
	MaxCommitBatchGasFee    BatchFeeConfig

	MaxTerminateGasFee types.FIL
	// WindowPoSt is a high-value operation, so the default fee should be high.
	MaxWindowPoStGasFee    types.FIL
	MaxPublishDealsFee     types.FIL
	MaxMarketBalanceAddFee types.FIL

	MaximizeWindowPoStFeeCap bool
}

type SealerConfig struct {
	ParallelFetchLimit int

	AllowSectorDownload      bool
	AllowAddPiece            bool
	AllowPreCommit1          bool
	AllowPreCommit2          bool
	AllowCommit              bool
	AllowUnseal              bool
	AllowReplicaUpdate       bool
	AllowProveReplicaUpdate2 bool
	AllowRegenSectorKey      bool

	// LocalWorkerName specifies a custom name for the builtin worker.
	// If set to an empty string (default) os hostname will be used
	LocalWorkerName string

	// Assigner specifies the worker assigner to use when scheduling tasks.
	// "utilization" (default) - assign tasks to workers with lowest utilization.
	// "spread" - assign tasks to as many distinct workers as possible.
	Assigner string

	// DisallowRemoteFinalize when set to true will force all Finalize tasks to
	// run on workers with local access to both long-term storage and the sealing
	// path containing the sector.
	// --
	// WARNING: Only set this if all workers have access to long-term storage
	// paths. If this flag is enabled, and there are workers without long-term
	// storage access, sectors will not be moved from them, and Finalize tasks
	// will appear to be stuck.
	// --
	// If you see stuck Finalize tasks after enabling this setting, check
	// 'lotus-miner sealing sched-diag' and 'lotus-miner storage find [sector num]'
	DisallowRemoteFinalize bool

	// ResourceFiltering instructs the system which resource filtering strategy
	// to use when evaluating tasks against this worker. An empty value defaults
	// to "hardware".
	ResourceFiltering ResourceFilteringStrategy
}

// ResourceFilteringStrategy is an enum indicating the kinds of resource
// filtering strategies that can be configured for workers.
type ResourceFilteringStrategy string

type SealingConfig struct {
	// Upper bound on how many sectors can be waiting for more deals to be packed in it before it begins sealing at any given time.
	// If the miner is accepting multiple deals in parallel, up to MaxWaitDealsSectors of new sectors will be created.
	// If more than MaxWaitDealsSectors deals are accepted in parallel, only MaxWaitDealsSectors deals will be processed in parallel
	// Note that setting this number too high in relation to deal ingestion rate may result in poor sector packing efficiency
	// 0 = no limit
	MaxWaitDealsSectors uint64

	// Upper bound on how many sectors can be sealing+upgrading at the same time when creating new CC sectors (0 = unlimited)
	MaxSealingSectors uint64

	// Upper bound on how many sectors can be sealing+upgrading at the same time when creating new sectors with deals (0 = unlimited)
	MaxSealingSectorsForDeals uint64

	// Prefer creating new sectors even if there are sectors Available for upgrading.
	// This setting combined with MaxUpgradingSectors set to a value higher than MaxSealingSectorsForDeals makes it
	// possible to use fast sector upgrades to handle high volumes of storage deals, while still using the simple sealing
	// flow when the volume of storage deals is lower.
	PreferNewSectorsForDeals bool

	// Upper bound on how many sectors can be sealing+upgrading at the same time when upgrading CC sectors with deals (0 = MaxSealingSectorsForDeals)
	MaxUpgradingSectors uint64

	// When set to a non-zero value, minimum number of epochs until sector expiration required for sectors to be considered
	// for upgrades (0 = DealMinDuration = 180 days = 518400 epochs)
	//
	// Note that if all deals waiting in the input queue have lifetimes longer than this value, upgrade sectors will be
	// required to have expiration of at least the soonest-ending deal
	MinUpgradeSectorExpiration uint64

	// DEPRECATED: Target expiration is no longer used
	MinTargetUpgradeSectorExpiration uint64

	// CommittedCapacitySectorLifetime is the duration a Committed Capacity (CC) sector will
	// live before it must be extended or converted into sector containing deals before it is
	// terminated. Value must be between 180-1278 days (1278 in nv21, 540 before nv21).
	CommittedCapacitySectorLifetime Duration

	// Period of time that a newly created sector will wait for more deals to be packed in to before it starts to seal.
	// Sectors which are fully filled will start sealing immediately
	WaitDealsDelay Duration

	// Whether to keep unsealed copies of deal data regardless of whether the client requested that. This lets the miner
	// avoid the relatively high cost of unsealing the data later, at the cost of more storage space
	AlwaysKeepUnsealedCopy bool

	// Run sector finalization before submitting sector proof to the chain
	FinalizeEarly bool

	// Whether new sectors are created to pack incoming deals
	// When this is set to false no new sectors will be created for sealing incoming deals
	// This is useful for forcing all deals to be assigned as snap deals to sectors marked for upgrade
	MakeNewSectorForDeals bool

	// After sealing CC sectors, make them available for upgrading with deals
	MakeCCSectorsAvailable bool

	// Whether to use available miner balance for sector collateral instead of sending it with each message
	CollateralFromMinerBalance bool
	// Minimum available balance to keep in the miner actor before sending it with messages
	AvailableBalanceBuffer types.FIL
	// Don't send collateral with messages even if there is no available balance in the miner actor
	DisableCollateralFallback bool

	// maximum precommit batch size - batches will be sent immediately above this size
	MaxPreCommitBatch int
	// how long to wait before submitting a batch after crossing the minimum batch size
	PreCommitBatchWait Duration
	// time buffer for forceful batch submission before sectors/deal in batch would start expiring
	PreCommitBatchSlack Duration

	// enable / disable commit aggregation (takes effect after nv13)
	AggregateCommits bool
	// minimum batched commit size - batches above this size will eventually be sent on a timeout
	MinCommitBatch int
	// maximum batched commit size - batches will be sent immediately above this size
	MaxCommitBatch int
	// how long to wait before submitting a batch after crossing the minimum batch size
	CommitBatchWait Duration
	// time buffer for forceful batch submission before sectors/deals in batch would start expiring
	CommitBatchSlack Duration

	// network BaseFee below which to stop doing precommit batching, instead
	// sending precommit messages to the chain individually. When the basefee is
	// below this threshold, precommit messages will get sent out immediately.
	BatchPreCommitAboveBaseFee types.FIL

	// network BaseFee below which to stop doing commit aggregation, instead
	// submitting proofs to the chain individually
	AggregateAboveBaseFee types.FIL

	// When submitting several sector prove commit messages simultaneously, this option allows you to
	// stagger the number of prove commits submitted per epoch
	// This is done because gas estimates for ProveCommits are non deterministic and increasing as a large
	// number of sectors get committed within the same epoch resulting in occasionally failed msgs.
	// Submitting a smaller number of prove commits per epoch would reduce the possibility of failed msgs
	MaxSectorProveCommitsSubmittedPerEpoch uint64

	TerminateBatchMax  uint64
	TerminateBatchMin  uint64
	TerminateBatchWait Duration

	// Keep this many sectors in sealing pipeline, start CC if needed
	// todo TargetSealingSectors uint64

	// todo TargetSectors - stop auto-pleding new sectors after this many sectors are sealed, default CC upgrade for deals sectors if above

	// UseSyntheticPoRep, when set to true, will reduce the amount of cache data held on disk after the completion of PreCommit 2 to 11GiB.
	UseSyntheticPoRep bool

	// Whether to abort if any sector activation in a batch fails (newly sealed sectors, only with ProveCommitSectors3).
	RequireActivationSuccess bool
	// Whether to abort if any piece activation notification returns a non-zero exit code (newly sealed sectors, only with ProveCommitSectors3).
	RequireActivationSuccessUpdate bool
	// Whether to abort if any sector activation in a batch fails (updating sectors, only with ProveReplicaUpdates3).
	RequireNotificationSuccess bool
	// Whether to abort if any piece activation notification returns a non-zero exit code (updating sectors, only with ProveReplicaUpdates3).
	RequireNotificationSuccessUpdate bool
}

type ProvingConfig struct {
	// Maximum number of sector checks to run in parallel. (0 = unlimited)
	//
	// WARNING: Setting this value too high may make the node crash by running out of stack
	// WARNING: Setting this value too low may make sector challenge reading much slower, resulting in failed PoSt due
	// to late submission.
	//
	// After changing this option, confirm that the new value works in your setup by invoking
	// 'lotus-miner proving compute window-post 0'
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

	// Disable Window PoSt computation on the lotus-miner process even if no window PoSt workers are present.
	//
	// WARNING: If no windowPoSt workers are connected, window PoSt WILL FAIL resulting in faulty sectors which will need
	// to be recovered. Before enabling this option, make sure your PoSt workers work correctly.
	//
	// After changing this option, confirm that the new value works in your setup by invoking
	// 'lotus-miner proving compute window-post 0'
	DisableBuiltinWindowPoSt bool

	// Disable Winning PoSt computation on the lotus-miner process even if no winning PoSt workers are present.
	//
	// WARNING: If no WinningPoSt workers are connected, Winning PoSt WILL FAIL resulting in lost block rewards.
	// Before enabling this option, make sure your PoSt workers work correctly.
	DisableBuiltinWinningPoSt bool

	// Disable WindowPoSt provable sector readability checks.
	//
	// In normal operation, when preparing to compute WindowPoSt, lotus-miner will perform a round of reading challenges
	// from all sectors to confirm that those sectors can be proven. Challenges read in this process are discarded, as
	// we're only interested in checking that sector data can be read.
	//
	// When using builtin proof computation (no PoSt workers, and DisableBuiltinWindowPoSt is set to false), this process
	// can save a lot of time and compute resources in the case that some sectors are not readable - this is caused by
	// the builtin logic not skipping snark computation when some sectors need to be skipped.
	//
	// When using PoSt workers, this process is mostly redundant, with PoSt workers challenges will be read once, and
	// if challenges for some sectors aren't readable, those sectors will just get skipped.
	//
	// Disabling sector pre-checks will slightly reduce IO load when proving sectors, possibly resulting in shorter
	// time to produce window PoSt. In setups with good IO capabilities the effect of this option on proving time should
	// be negligible.
	//
	// NOTE: It likely is a bad idea to disable sector pre-checks in setups with no PoSt workers.
	//
	// NOTE: Even when this option is enabled, recovering sectors will be checked before recovery declaration message is
	// sent to the chain
	//
	// After changing this option, confirm that the new value works in your setup by invoking
	// 'lotus-miner proving compute window-post 0'
	DisableWDPoStPreChecks bool

	// Maximum number of partitions to prove in a single SubmitWindowPoSt messace. 0 = network limit (3 in nv21)
	//
	// A single partition may contain up to 2349 32GiB sectors, or 2300 64GiB sectors.
	//	//
	// Note that setting this value lower may result in less efficient gas use - more messages will be sent,
	// to prove each deadline, resulting in more total gas use (but each message will have lower gas limit)
	//
	// Setting this value above the network limit has no effect
	MaxPartitionsPerPoStMessage int

	// Maximum number of partitions to declare in a single DeclareFaultsRecovered message. 0 = no limit.

	// In some cases when submitting DeclareFaultsRecovered messages,
	// there may be too many recoveries to fit in a BlockGasLimit.
	// In those cases it may be necessary to set this value to something low (eg 1);
	// Note that setting this value lower may result in less efficient gas use - more messages will be sent than needed,
	// resulting in more total gas use (but each message will have lower gas limit)
	MaxPartitionsPerRecoveryMessage int

	// Enable single partition per PoSt Message for partitions containing recovery sectors
	//
	// In cases when submitting PoSt messages which contain recovering sectors, the default network limit may still be
	// too high to fit in the block gas limit. In those cases, it becomes useful to only house the single partition
	// with recovering sectors in the post message
	//
	// Note that setting this value lower may result in less efficient gas use - more messages will be sent,
	// to prove each deadline, resulting in more total gas use (but each message will have lower gas limit)
	SingleRecoveringPartitionPerPostMessage bool
}

type IndexProviderConfig struct {
	// Enable set whether to enable indexing announcement to the network and expose endpoints that
	// allow indexer nodes to process announcements. Enabled by default.
	Enable bool

	// EntriesCacheCapacity sets the maximum capacity to use for caching the indexing advertisement
	// entries. Defaults to 1024 if not specified. The cache is evicted using LRU policy. The
	// maximum storage used by the cache is a factor of EntriesCacheCapacity, EntriesChunkSize and
	// the length of multihashes being advertised. For example, advertising 128-bit long multihashes
	// with the default EntriesCacheCapacity, and EntriesChunkSize means the cache size can grow to
	// 256MiB when full.
	EntriesCacheCapacity int

	// EntriesChunkSize sets the maximum number of multihashes to include in a single entries chunk.
	// Defaults to 16384 if not specified. Note that chunks are chained together for indexing
	// advertisements that include more multihashes than the configured EntriesChunkSize.
	EntriesChunkSize int

	// TopicName sets the topic name on which the changes to the advertised content are announced.
	// If not explicitly specified, the topic name is automatically inferred from the network name
	// in following format: '/indexer/ingest/<network-name>'
	// Defaults to empty, which implies the topic name is inferred from network name.
	TopicName string

	// PurgeCacheOnStart sets whether to clear any cached entries chunks when the provider engine
	// starts. By default, the cache is rehydrated from previously cached entries stored in
	// datastore if any is present.
	PurgeCacheOnStart bool
}

type DealmakingConfig struct {
	// When enabled, the miner can accept online deals
	ConsiderOnlineStorageDeals bool
	// When enabled, the miner can accept offline deals
	ConsiderOfflineStorageDeals bool
	// When enabled, the miner can accept retrieval deals
	ConsiderOnlineRetrievalDeals bool
	// When enabled, the miner can accept offline retrieval deals
	ConsiderOfflineRetrievalDeals bool
	// When enabled, the miner can accept verified deals
	ConsiderVerifiedStorageDeals bool
	// When enabled, the miner can accept unverified deals
	ConsiderUnverifiedStorageDeals bool
	// A list of Data CIDs to reject when making deals
	PieceCidBlocklist []cid.Cid
	// Maximum expected amount of time getting the deal into a sealed sector will take
	// This includes the time the deal will need to get transferred and published
	// before being assigned to a sector
	ExpectedSealDuration Duration
	// Maximum amount of time proposed deal StartEpoch can be in future
	MaxDealStartDelay Duration
	// When a deal is ready to publish, the amount of time to wait for more
	// deals to be ready to publish before publishing them all as a batch
	PublishMsgPeriod Duration
	// The maximum number of deals to include in a single PublishStorageDeals
	// message
	MaxDealsPerPublishMsg uint64
	// The maximum collateral that the provider will put up against a deal,
	// as a multiplier of the minimum collateral bound
	MaxProviderCollateralMultiplier uint64
	// The maximum allowed disk usage size in bytes of staging deals not yet
	// passed to the sealing node by the markets service. 0 is unlimited.
	MaxStagingDealsBytes int64
	// The maximum number of parallel online data transfers for storage deals
	SimultaneousTransfersForStorage uint64
	// The maximum number of simultaneous data transfers from any single client
	// for storage deals.
	// Unset by default (0), and values higher than SimultaneousTransfersForStorage
	// will have no effect; i.e. the total number of simultaneous data transfers
	// across all storage clients is bound by SimultaneousTransfersForStorage
	// regardless of this number.
	SimultaneousTransfersForStoragePerClient uint64
	// The maximum number of parallel online data transfers for retrieval deals
	SimultaneousTransfersForRetrieval uint64
	// Minimum start epoch buffer to give time for sealing of sector with deal.
	StartEpochSealingBuffer uint64

	// A command used for fine-grained evaluation of storage deals
	// see https://lotus.filecoin.io/storage-providers/advanced-configurations/market/#using-filters-for-fine-grained-storage-and-retrieval-deal-acceptance for more details
	Filter string
	// A command used for fine-grained evaluation of retrieval deals
	// see https://lotus.filecoin.io/storage-providers/advanced-configurations/market/#using-filters-for-fine-grained-storage-and-retrieval-deal-acceptance for more details
	RetrievalFilter string

	RetrievalPricing *RetrievalPricing
}

type RetrievalPricing struct {
	Strategy string // possible values: "default", "external"

	Default  *RetrievalPricingDefault
	External *RetrievalPricingExternal
}

type RetrievalPricingExternal struct {
	// Path of the external script that will be run to price a retrieval deal.
	// This parameter is ONLY applicable if the retrieval pricing policy strategy has been configured to "external".
	Path string
}

type RetrievalPricingDefault struct {
	// VerifiedDealsFreeTransfer configures zero fees for data transfer for a retrieval deal
	// of a payloadCid that belongs to a verified storage deal.
	// This parameter is ONLY applicable if the retrieval pricing policy strategy has been configured to "default".
	// default value is true
	VerifiedDealsFreeTransfer bool
}
type MinerSubsystemConfig struct {
	EnableMining        bool
	EnableSealing       bool
	EnableSectorStorage bool
	EnableMarkets       bool

	// When enabled, the sector index will reside in an external database
	// as opposed to the local KV store in the miner process
	// This is useful to allow workers to bypass the lotus miner to access sector information
	EnableSectorIndexDB bool

	SealerApiInfo      string // if EnableSealing == false
	SectorIndexApiInfo string // if EnableSectorStorage == false

	// When window post is enabled, the miner will automatically submit window post proofs
	// for all sectors that are eligible for window post
	// IF WINDOW POST IS DISABLED, THE MINER WILL NOT SUBMIT WINDOW POST PROOFS
	// THIS WILL RESULT IN FAULTS AND PENALTIES IF NO OTHER MECHANISM IS RUNNING
	// TO SUBMIT WINDOW POST PROOFS.
	// Note: This option is entirely disabling the window post scheduler,
	//   not just the builtin PoSt computation like Proving.DisableBuiltinWindowPoSt.
	//   This option will stop lotus-miner from performing any actions related
	//   to window post, including scheduling, submitting proofs, and recovering
	//   sectors.
	DisableWindowPoSt bool

	// When winning post is disabled, the miner process will NOT attempt to mine
	// blocks. This should only be set when there's an external process mining
	// blocks on behalf of the miner.
	// When disabled and no external block producers are configured, all potential
	// block rewards will be missed!
	DisableWinningPoSt bool
}

func DefaultStorageMiner() *StorageMiner {
	// TODO: Should we increase this to nv21, which would push it to 3.5 years?
	maxSectorExtentsion, _ := policy.GetMaxSectorExpirationExtension(network.Version20)
	cfg := &StorageMiner{
		Common: defCommon(),

		Sealing: SealingConfig{
			MaxWaitDealsSectors:       2, // 64G with 32G sectors
			MaxSealingSectors:         0,
			MaxSealingSectorsForDeals: 0,
			WaitDealsDelay:            Duration(time.Hour * 6),
			AlwaysKeepUnsealedCopy:    true,
			FinalizeEarly:             false,
			MakeNewSectorForDeals:     true,

			CollateralFromMinerBalance: false,
			AvailableBalanceBuffer:     types.FIL(big.Zero()),
			DisableCollateralFallback:  false,

			MaxPreCommitBatch:  miner5.PreCommitSectorBatchMaxSize, // up to 256 sectors
			PreCommitBatchWait: Duration(24 * time.Hour),           // this should be less than 31.5 hours, which is the expiration of a precommit ticket
			// XXX snap deals wait deals slack if first
			PreCommitBatchSlack: Duration(3 * time.Hour), // time buffer for forceful batch submission before sectors/deals in batch would start expiring, higher value will lower the chances for message fail due to expiration

			CommittedCapacitySectorLifetime: Duration(builtin.EpochDurationSeconds * uint64(maxSectorExtentsion) * uint64(time.Second)),

			AggregateCommits: true,
			MinCommitBatch:   miner5.MinAggregatedSectors, // per FIP13, we must have at least four proofs to aggregate, where 4 is the cross over point where aggregation wins out on single provecommit gas costs
			MaxCommitBatch:   miner5.MaxAggregatedSectors, // maximum 819 sectors, this is the maximum aggregation per FIP13
			CommitBatchWait:  Duration(24 * time.Hour),    // this can be up to 30 days
			CommitBatchSlack: Duration(1 * time.Hour),     // time buffer for forceful batch submission before sectors/deals in batch would start expiring, higher value will lower the chances for message fail due to expiration

			BatchPreCommitAboveBaseFee: types.FIL(types.BigMul(types.PicoFil, types.NewInt(320))), // 0.32 nFIL
			AggregateAboveBaseFee:      types.FIL(types.BigMul(types.PicoFil, types.NewInt(320))), // 0.32 nFIL

			TerminateBatchMin:                      1,
			TerminateBatchMax:                      100,
			TerminateBatchWait:                     Duration(5 * time.Minute),
			MaxSectorProveCommitsSubmittedPerEpoch: 20,
			UseSyntheticPoRep:                      false,
		},

		Proving: ProvingConfig{
			ParallelCheckLimit:    32,
			PartitionCheckTimeout: Duration(20 * time.Minute),
			SingleCheckTimeout:    Duration(10 * time.Minute),
		},

		Storage: SealerConfig{
			AllowSectorDownload:      true,
			AllowAddPiece:            true,
			AllowPreCommit1:          true,
			AllowPreCommit2:          true,
			AllowCommit:              true,
			AllowUnseal:              true,
			AllowReplicaUpdate:       true,
			AllowProveReplicaUpdate2: true,
			AllowRegenSectorKey:      true,

			// Default to 10 - tcp should still be able to figure this out, and
			// it's the ratio between 10gbit / 1gbit
			ParallelFetchLimit: 10,

			Assigner: "utilization",

			// By default use the hardware resource filtering strategy.
			ResourceFiltering: ResourceFilteringHardware,
		},

		Dealmaking: DealmakingConfig{
			ConsiderOnlineStorageDeals:     true,
			ConsiderOfflineStorageDeals:    true,
			ConsiderOnlineRetrievalDeals:   true,
			ConsiderOfflineRetrievalDeals:  true,
			ConsiderVerifiedStorageDeals:   true,
			ConsiderUnverifiedStorageDeals: true,
			PieceCidBlocklist:              []cid.Cid{},
			// TODO: It'd be nice to set this based on sector size
			MaxDealStartDelay:               Duration(time.Hour * 24 * 14),
			ExpectedSealDuration:            Duration(time.Hour * 24),
			PublishMsgPeriod:                Duration(time.Hour),
			MaxDealsPerPublishMsg:           8,
			MaxProviderCollateralMultiplier: 2,

			SimultaneousTransfersForStorage:          DefaultSimultaneousTransfers,
			SimultaneousTransfersForStoragePerClient: 0,
			SimultaneousTransfersForRetrieval:        DefaultSimultaneousTransfers,

			StartEpochSealingBuffer: 480, // 480 epochs buffer == 4 hours from adding deal to sector to sector being sealed

			RetrievalPricing: &RetrievalPricing{
				Strategy: RetrievalPricingDefaultMode,
				Default: &RetrievalPricingDefault{
					VerifiedDealsFreeTransfer: true,
				},
				External: &RetrievalPricingExternal{
					Path: "",
				},
			},
		},

		IndexProvider: IndexProviderConfig{
			Enable:               true,
			EntriesCacheCapacity: 1024,
			EntriesChunkSize:     16384,
			// The default empty TopicName means it is inferred from network name, in the following
			// format: "/indexer/ingest/<network-name>"
			TopicName:         "",
			PurgeCacheOnStart: false,
		},

		Subsystems: MinerSubsystemConfig{
			EnableMining:        true,
			EnableSealing:       true,
			EnableSectorStorage: true,
			EnableMarkets:       false,
			EnableSectorIndexDB: false,
		},

		Fees: MinerFeeConfig{
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

			MaxTerminateGasFee:     types.MustParseFIL("0.5"),
			MaxWindowPoStGasFee:    types.MustParseFIL("5"),
			MaxPublishDealsFee:     types.MustParseFIL("0.05"),
			MaxMarketBalanceAddFee: types.MustParseFIL("0.007"),

			MaximizeWindowPoStFeeCap: true,
		},

		Addresses: MinerAddressConfig{
			PreCommitControl:   []string{},
			CommitControl:      []string{},
			TerminateControl:   []string{},
			DealPublishControl: []string{},
		},

		DAGStore: DAGStoreConfig{
			MaxConcurrentIndex:         5,
			MaxConcurrencyStorageCalls: 100,
			MaxConcurrentUnseals:       5,
			GCInterval:                 Duration(1 * time.Minute),
		},
		HarmonyDB: HarmonyDB{
			Hosts:    []string{"127.0.0.1"},
			Username: "yugabyte",
			Password: "yugabyte",
			Database: "yugabyte",
			Port:     "5433",
		},
	}

	cfg.Common.API.ListenAddress = "/ip4/127.0.0.1/tcp/2345/http"
	cfg.Common.API.RemoteListenAddress = "127.0.0.1:2345"
	return cfg
}

func defCommon() Common {
	return Common{
		API: API{
			ListenAddress: "/ip4/127.0.0.1/tcp/1234/http",
			Timeout:       Duration(30 * time.Second),
		},
		Logging: Logging{
			SubsystemLevels: map[string]string{
				"example-subsystem": "INFO",
			},
		},
		Backup: Backup{
			DisableMetadataLog: true,
		},
		Libp2p: Libp2p{
			ListenAddresses: []string{
				"/ip4/0.0.0.0/tcp/0",
				"/ip6/::/tcp/0",
				"/ip4/0.0.0.0/udp/0/quic-v1",
				"/ip6/::/udp/0/quic-v1",
				"/ip4/0.0.0.0/udp/0/quic-v1/webtransport",
				"/ip6/::/udp/0/quic-v1/webtransport",
			},
			AnnounceAddresses:   []string{},
			NoAnnounceAddresses: []string{},

			ConnMgrLow:   150,
			ConnMgrHigh:  180,
			ConnMgrGrace: Duration(20 * time.Second),
		},
		Pubsub: Pubsub{
			Bootstrapper: false,
			DirectPeers:  nil,
		},
	}
}

const (
	// ResourceFilteringHardware specifies that available hardware resources
	// should be evaluated when scheduling a task against the worker.
	ResourceFilteringHardware = ResourceFilteringStrategy("hardware")

	// ResourceFilteringDisabled disables resource filtering against this
	// worker. The scheduler may assign any task to this worker.
	ResourceFilteringDisabled = ResourceFilteringStrategy("disabled")
)

var DefaultSimultaneousTransfers = uint64(20)

const (
	// RetrievalPricingDefault configures the node to use the default retrieval pricing policy.
	RetrievalPricingDefaultMode = "default"
	// RetrievalPricingExternal configures the node to use the external retrieval pricing script
	// configured by the user.
	RetrievalPricingExternalMode = "external"
)
