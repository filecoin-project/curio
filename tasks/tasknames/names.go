// Package tasknames holds harmony task name strings used in harmony_task.name and alert filters.
// It must not import application packages to avoid cycles (e.g. tasks/pdpv0 → pdp → alertmanager).
package tasknames

const (
	// Sealing pipeline
	SDR             = "SDR"
	TreeD           = "TreeD"
	TreeRC          = "TreeRC"
	SyntheticProofs = "SyntheticProofs"
	PreCommitBatch  = "PreCommitBatch"
	PoRep           = "PoRep"
	Finalize        = "Finalize"
	MoveStorage     = "MoveStorage"
	CommitBatch     = "CommitBatch"

	// Snap / update pipeline
	UpdateEncode = "UpdateEncode"
	UpdateProve  = "UpdateProve"
	UpdateBatch  = "UpdateBatch"
	UpdateStore  = "UpdateStore"

	// PoSt
	WdPost        = "WdPost"
	WdPostSubmit  = "WdPostSubmit"
	WdPostRecover = "WdPostRecover"
	WinPost       = "WinPost"
	WinInclCheck  = "WinInclCheck"

	// Piece / storage-market pipeline
	ParkPiece     = "ParkPiece"
	StorePiece    = "StorePiece"
	DropPiece     = "DropPiece"
	FixParkPiece  = "FixParkPiece"
	AggregateChunks = "AggregateChunks"
	CommP         = "CommP"
	PSD           = "PSD"
	FindDeal      = "FindDeal"
	AggregateDeals = "AggregateDeals"
	FixRawSize    = "FixRawSize"

	// Messaging
	SendMessage     = "SendMessage"
	SendTransaction = "SendTransaction"

	// Indexing / IPNI
	Indexing        = "Indexing"
	IPNI            = "IPNI"
	CheckIndex      = "CheckIndex"
	PDPIndexing     = "PDPIndexing"
	PDPIpni         = "PDPIpni"
	PDPv0_Indexing  = "PDPv0_Indexing"
	PDPv0_IPNI      = "PDPv0_IPNI"

	// GC & housekeeping
	StorageMetaGC  = "StorageMetaGC"
	StorageGCSweep = "StorageGCSweep"
	StorageGCMark  = "StorageGCMark"
	PipelineGC     = "PipelineGC"
	PieceCleanup   = "PieceCleanup"

	// Misc bookkeeping
	SectorMetadata = "SectorMetadata"
	ExpMgr         = "ExpMgr"
	BalanceMgr     = "BalanceMgr"
	Settle         = "Settle"

	// Unseal
	UnsealDecode = "UnsealDecode"
	SDRKeyRegen  = "SDRKeyRegen"

	// Scrub
	ScrubCommDCheck = "ScrubCommDCheck"
	ScrubCommRCheck = "ScrubCommRCheck"

	// ProofShare
	PSProve      = "PSProve"
	PShareSubmit = "PShareSubmit"
	PSAutoSettle = "PSAutoSettle"
	PSClientPoll = "PSClientPoll"
	PSPutVanilla = "PSPutVanilla"

	// PDP v1
	PDPProve         = "PDPProve"
	PDPAddPiece      = "PDPAddPiece"
	PDPDeletePiece   = "PDPDeletePiece"
	PDPAddDataSet    = "PDPAddDataSet"
	PDPDelDataSet    = "PDPDelDataSet"
	PDPInitPP        = "PDPInitPP"
	PDPProvingPeriod = "PDPProvingPeriod"
	PDPNotify        = "PDPNotify"
	PDPCommP         = "PDPCommP"
	PDPSaveCache     = "PDPSaveCache"
	AggregatePDPDeal = "AggregatePDPDeal"
	PDPSync          = "PDPSync"

	// PDP v0
	PDPv0_Prove      = "PDPv0_Prove"
	PDPv0_PullPiece  = "PDPv0_PullPiece"
	PDPv0_SaveCache  = "PDPv0_SaveCache"
	PDPv0_InitPP     = "PDPv0_InitPP"
	PDPv0_ProvPeriod = "PDPv0_ProvPeriod"
	PDPv0_Notify     = "PDPv0_Notify"
	PDPv0_DelDataSet = "PDPv0_DelDataSet"
	PDPv0_TermFWSS   = "PDPv0_TermFWSS"
)
