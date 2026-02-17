package seal

import (
	"bytes"
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	storiface "github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

var IsDevnet = build.BlockDelaySecs < 30

func SetDevnet(value bool) {
	IsDevnet = value
}

func GetDevnet() bool {
	return IsDevnet
}

type SDRAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateGetRandomnessFromTickets(context.Context, crypto.DomainSeparationTag, abi.ChainEpoch, []byte, types.TipSetKey) (abi.Randomness, error)
}

// ProviderPollerSDR is an interface that allows registering the SDR task's
// AddTaskFunc with the remote seal provider poller. This enables the provider
// poller to schedule SDR tasks for rseal_provider_pipeline rows.
type ProviderPollerSDR interface {
	SetPollerSDR(harmonytask.AddTaskFunc)
}

type SDRTask struct {
	api   SDRAPI
	ccAPI CCSchedulerAPI // optional, nil disables CC scheduling
	db    *harmonydb.DB
	sp    *SealPoller

	sc *ffi2.SealCalls

	provPoller ProviderPollerSDR // optional, nil when remote seal provider is not enabled

	max taskhelp.Limiter
	min int
}

func NewSDRTask(api SDRAPI, db *harmonydb.DB, sp *SealPoller, sc *ffi2.SealCalls, maxSDR taskhelp.Limiter, minSDR int, provPoller ProviderPollerSDR) *SDRTask {
	// If the API also satisfies CCSchedulerAPI, enable CC scheduling.
	// This is the case when the full chain API is passed (normal operation).
	var ccAPI CCSchedulerAPI
	if ca, ok := api.(CCSchedulerAPI); ok {
		ccAPI = ca
	}

	return &SDRTask{
		api:        api,
		ccAPI:      ccAPI,
		db:         db,
		sp:         sp,
		sc:         sc,
		provPoller: provPoller,
		max:        maxSDR,
		min:        minSDR,
	}
}

func (s *SDRTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64                   `db:"sp_id"`
		SectorNumber int64                   `db:"sector_number"`
		RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`
		Pipeline     string                  `db:"pipeline"`
	}

	err = s.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof, 'local' as pipeline
		FROM sectors_sdr_pipeline
		WHERE task_id_sdr = $1
		UNION ALL
		SELECT sp_id, sector_number, reg_seal_proof, 'remote' as pipeline
		FROM rseal_provider_pipeline
		WHERE task_id_sdr = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(sectorParamsArr))
	}
	sectorParams := sectorParamsArr[0]

	dealData, err := dealdata.DealDataSDRPoRep(ctx, s.db, s.sc, sectorParams.SpID, sectorParams.SectorNumber, sectorParams.RegSealProof, true)
	if err != nil {
		return false, xerrors.Errorf("getting deal data: %w", err)
	}

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: sectorParams.RegSealProof,
	}

	// get ticket
	maddr, err := address.NewIDAddress(uint64(sectorParams.SpID))
	if err != nil {
		return false, xerrors.Errorf("getting miner address: %w", err)
	}

	// FAIL: api may be down
	// FAIL-RESP: rely on harmony retry
	ticket, ticketEpoch, err := GetTicket(ctx, s.api, maddr)
	if err != nil {
		return false, xerrors.Errorf("getting ticket: %w", err)
	}

	// do the SDR!!

	// FAIL: storage may not have enough space
	// FAIL-RESP: rely on harmony retry

	// LATEFAIL: compute error in sdr
	// LATEFAIL-RESP: Check in Trees task should catch this; Will retry computing
	//                Trees; After one retry, it should return the sector to the
	// 			      SDR stage; max number of retries should be configurable

	err = s.sc.GenerateSDR(ctx, taskID, storiface.FTCache, sref, ticket, dealData.CommD)
	if err != nil {
		return false, xerrors.Errorf("generating sdr: %w", err)
	}

	// store success!
	var n int
	if sectorParams.Pipeline == "remote" {
		n, err = s.db.Exec(ctx, `UPDATE rseal_provider_pipeline
			SET after_sdr = true, ticket_epoch = $3, ticket_value = $4, task_id_sdr = NULL
			WHERE sp_id = $1 AND sector_number = $2`,
			sectorParams.SpID, sectorParams.SectorNumber, ticketEpoch, []byte(ticket))
	} else {
		n, err = s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
			SET after_sdr = true, ticket_epoch = $3, ticket_value = $4, task_id_sdr = NULL
			WHERE sp_id = $1 AND sector_number = $2`,
			sectorParams.SpID, sectorParams.SectorNumber, ticketEpoch, []byte(ticket))
	}
	if err != nil {
		return false, xerrors.Errorf("store sdr success: updating pipeline: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("store sdr success: updated %d rows", n)
	}

	// Record metric
	if err := stats.RecordWithTags(ctx, []tag.Mutator{
		tag.Upsert(MinerTag, maddr.String()),
	}, SealMeasures.SDRCompleted.M(1)); err != nil {
		log.Errorf("recording metric: %s", err)
	}

	return true, nil
}

type TicketNodeAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateGetRandomnessFromTickets(context.Context, crypto.DomainSeparationTag, abi.ChainEpoch, []byte, types.TipSetKey) (abi.Randomness, error)
}

func GetTicket(ctx context.Context, api TicketNodeAPI, maddr address.Address) (abi.SealRandomness, abi.ChainEpoch, error) {
	ts, err := api.ChainHead(ctx)
	if err != nil {
		return nil, 0, xerrors.Errorf("getting chain head: %w", err)
	}

	ticketEpoch := ts.Height() - policy.SealRandomnessLookback
	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		return nil, 0, xerrors.Errorf("marshaling miner address: %w", err)
	}

	rand, err := api.StateGetRandomnessFromTickets(ctx, crypto.DomainSeparationTag_SealRandomness, ticketEpoch, buf.Bytes(), ts.Key())
	if err != nil {
		return nil, 0, xerrors.Errorf("getting randomness from tickets: %w", err)
	}

	return abi.SealRandomness(rand), ticketEpoch, nil
}

func (s *SDRTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if s.min > len(ids) {
		log.Debugw("did not accept task", "name", "SDR", "reason", "below min", "min", s.min, "count", len(ids))
		return []harmonytask.TaskID{}, nil
	}

	return ids, nil
}

func (s *SDRTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	res := harmonytask.TaskTypeDetails{
		Max:  s.max,
		Name: "SDR",
		Cost: resources.Resources{
			Cpu:     4, // todo multicore sdr
			Gpu:     0,
			Ram:     (64 << 30) + (256 << 20),
			Storage: s.sc.Storage(s.taskToSector, storiface.FTCache, storiface.FTNone, ssize, storiface.PathSealing, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 2,
		Follows:     nil,
		IAmBored:    s.newScheduleCCFunc(),
	}

	if IsDevnet {
		res.Cost.Ram = 1 << 30
		res.Cost.Cpu = 1
	}

	return res
}

func (s *SDRTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	s.sp.pollers[pollerSDR].Set(taskFunc)
	if s.provPoller != nil {
		s.provPoller.SetPollerSDR(taskFunc)
	}
}

func (s *SDRTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := s.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (s *SDRTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id, sector_number FROM (
		SELECT sp_id, sector_number FROM sectors_sdr_pipeline WHERE task_id_sdr = $1
		UNION ALL
		SELECT sp_id, sector_number FROM rseal_provider_pipeline WHERE task_id_sdr = $1
	) s`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&SDRTask{})

func (s *SDRTask) taskToSector(id harmonytask.TaskID) (ffi2.SectorRef, error) {
	var refs []ffi2.SectorRef

	err := s.db.Select(context.Background(), &refs, `
		SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_sdr = $1
		UNION ALL
		SELECT sp_id, sector_number, reg_seal_proof FROM rseal_provider_pipeline WHERE task_id_sdr = $1`, id)
	if err != nil {
		return ffi2.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi2.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

var _ harmonytask.TaskInterface = &SDRTask{}
