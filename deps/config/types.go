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

			MaxTerminateGasFee:  types.MustParseFIL("0.5"),
			MaxWindowPoStGasFee: types.MustParseFIL("5"),
			MaxPublishDealsFee:  types.MustParseFIL("0.05"),
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
		Ingest: CurioIngestConfig{
			MaxQueueDealSector: 8, // default to 8 sectors open(or in process of opening) for deals
			MaxQueueSDR:        8, // default to 8 (will cause backpressure even if deal sectors are 0)
			MaxQueueTrees:      0, // default don't use this limit
			MaxQueuePoRep:      0, // default don't use this limit
			MaxDealWaitTime:    Duration(1 * time.Hour),
		},
		Alerting: CurioAlerting{
			PagerDutyEventURL:      "https://events.pagerduty.com/v2/enqueue",
			PageDutyIntegrationKey: "",
			MinimumWalletBalance:   types.MustParseFIL("5"),
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
	Journal   JournalConfig
	Apis      ApisConfig
	Alerting  CurioAlerting
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

	// The maximum amount of MoveStorage tasks that can run simultaneously. Note that the maximum number of tasks will
	// also be bounded by resources available on the machine. It is recommended that this value is set to a number which
	// uses all available network (or disk) bandwidth on the machine without causing bottlenecks.
	MoveStorageMaxTasks int

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
	MaxQueueSDR int

	// Maximum number of sectors that can be queued waiting for SDRTrees to start processing.
	// 0 = unlimited
	// Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
	// In case of the trees tasks it is possible that this queue grows more than this limit, the backpressure is only
	// applied to sectors entering the pipeline.
	MaxQueueTrees int

	// Maximum number of sectors that can be queued waiting for PoRep to start processing.
	// 0 = unlimited
	// Note: This mechanism will delay taking deal data from markets, providing backpressure to the market subsystem.
	// Like with the trees tasks, it is possible that this queue grows more than this limit, the backpressure is only
	// applied to sectors entering the pipeline.
	MaxQueuePoRep int

	// Maximum time an open deal sector should wait for more deal before it starts sealing
	MaxDealWaitTime Duration
}

type CurioAlerting struct {
	// PagerDutyEventURL is URL for PagerDuty.com Events API v2 URL. Events sent to this API URL are ultimately
	// routed to a PagerDuty.com service and processed.
	// The default is sufficient for integration with the stock commercial PagerDuty.com company's service.
	PagerDutyEventURL string

	// PageDutyIntegrationKey is the integration key for a PagerDuty.com service. You can find this unique service
	// identifier in the integration page for the service.
	PageDutyIntegrationKey string

	// MinimumWalletBalance is the minimum balance all active wallets. If the balance is below this value, an
	// alerts will be triggered for the wallet
	MinimumWalletBalance types.FIL
}

type JournalConfig struct {
	//Events of the form: "system1:event1,system1:event2[,...]"
	DisabledEvents string
}

type ApisConfig struct {
	// ChainApiInfo is the API endpoint for the Lotus daemon.
	ChainApiInfo []string

	// RPC Secret for the storage subsystem.
	// If integrating with lotus-miner this must match the value from
	// cat ~/.lotusminer/keystore/MF2XI2BNNJ3XILLQOJUXMYLUMU | jq -r .PrivateKey
	StorageRPCSecret string
}
