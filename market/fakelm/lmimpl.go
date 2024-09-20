package fakelm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/gbrlsnchs/jwt/v3"
	"github.com/google/uuid"
	logging "github.com/ipfs/go-log/v2"
	typegen "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/paths"
	storiface "github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market"

	lapi "github.com/filecoin-project/lotus/api"
	market2 "github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	sealing "github.com/filecoin-project/lotus/storage/pipeline"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
)

var log = logging.Logger("lmrpc")

type LMRPCProvider struct {
	si   paths.SectorIndex
	full api.Chain

	maddr   address.Address // lotus-miner RPC is single-actor
	minerID abi.ActorID

	ssize abi.SectorSize

	pi   market.Ingester
	db   *harmonydb.DB
	conf *config.CurioConfig
}

func NewLMRPCProvider(si paths.SectorIndex, full api.Chain, maddr address.Address, minerID abi.ActorID, ssize abi.SectorSize, pi market.Ingester, db *harmonydb.DB, conf *config.CurioConfig) *LMRPCProvider {
	return &LMRPCProvider{
		si:      si,
		full:    full,
		maddr:   maddr,
		minerID: minerID,
		ssize:   ssize,
		pi:      pi,
		db:      db,
		conf:    conf,
	}
}

func (l *LMRPCProvider) ActorAddress(ctx context.Context) (address.Address, error) {
	return l.maddr, nil
}

func (l *LMRPCProvider) WorkerJobs(ctx context.Context) (map[uuid.UUID][]storiface.WorkerJob, error) {
	// correct enough
	return map[uuid.UUID][]storiface.WorkerJob{}, nil
}

func (l *LMRPCProvider) SectorsStatus(ctx context.Context, sid abi.SectorNumber, showOnChainInfo bool) (lapi.SectorInfo, error) {
	// TODO: Add snap, Add open_sector_pieces

	var ssip []struct {
		PieceCID        *string `db:"piece_cid"`
		DealID          *int64  `db:"f05_deal_id"`
		DDOPAM          *string `db:"ddo_pam"`
		Complete        bool    `db:"after_commit_msg_success"`
		Failed          bool    `db:"failed"`
		SDR             bool    `db:"after_sdr"`
		PoRep           bool    `db:"after_porep"`
		Tree            bool    `db:"after_tree_r"`
		IsSnap          bool    `db:"is_snap"`
		Encode          bool    `db:"after_encode"`
		SnapProve       bool    `db:"after_prove"`
		SnapCommit      bool    `db:"after_prove_msg_success"`
		SnapMoveStorage bool    `db:"after_move_storage"`
	}

	err := l.db.Select(ctx, &ssip, `
						WITH SectorMeta AS (
							SELECT
								sm.sp_id,
								sm.sector_num,
								sm.orig_sealed_cid,
								sm.cur_sealed_cid
							FROM
								sectors_meta sm
							WHERE
								sm.sp_id = $1 AND sm.sector_num = $2
						),
						SDRMeta AS (
							SELECT
								sp.sp_id,
								sp.sector_number,
								sp.after_commit_msg,
								sp.failed,
								sp.after_sdr,
								sp.after_porep,
								sp.after_tree_r,
								sp.after_commit_msg_success
							FROM
								sectors_sdr_pipeline sp
							WHERE
								sp.sp_id = $1 AND sp.sector_number = $2
						),
						CheckCommit AS (
							SELECT
								COALESCE(sp.sp_id, sm.sp_id) AS sp_id,
								COALESCE(sp.sector_number, sm.sector_num) AS sector_number,
								COALESCE(sp.after_commit_msg, TRUE) AS after_commit_msg,
								COALESCE(sp.failed, FALSE) AS failed,
								COALESCE(sp.after_sdr, TRUE) AS after_sdr,
								COALESCE(sp.after_porep, TRUE) AS after_porep,
								COALESCE(sp.after_tree_r, TRUE) AS after_tree_r,
								COALESCE(sp.after_commit_msg_success, TRUE) AS after_commit_msg_success,
								COALESCE(snap.after_prove_msg_success, snap.after_prove_msg_success is null) AS after_snap_msg_success,
								COALESCE(sm.orig_sealed_cid != sm.cur_sealed_cid, FALSE) AS is_snap,
								COALESCE(snap.after_encode, COALESCE(sm.orig_sealed_cid != sm.cur_sealed_cid, FALSE)) AS after_encode,
								COALESCE(snap.after_prove, COALESCE(sm.orig_sealed_cid != sm.cur_sealed_cid, FALSE)) AS after_prove,
								COALESCE(snap.after_prove_msg_success, COALESCE(sm.orig_sealed_cid != sm.cur_sealed_cid, FALSE)) AS after_prove_msg_success,
								COALESCE(snap.after_move_storage, COALESCE(sm.orig_sealed_cid != sm.cur_sealed_cid, FALSE)) AS after_move_storage
							FROM
								SDRMeta sp
									FULL OUTER JOIN SectorMeta sm ON sp.sp_id = sm.sp_id AND sp.sector_number = sm.sector_num
									LEFT JOIN sectors_snap_pipeline snap ON sm.sp_id = snap.sp_id AND sm.sector_num = snap.sector_number
							WHERE
								(sp.sp_id = $1 AND sp.sector_number = $2) OR (sm.sp_id = $1 AND sm.sector_num = $2)
						),
						MetaPieces AS (
							 SELECT
								 mp.piece_cid,
								 mp.f05_deal_id,
								 mp.ddo_pam as ddo_pam,
								 cc.after_commit_msg_success AND after_snap_msg_success as after_commit_msg_success,
								 cc.failed,
								 cc.after_sdr,
								 cc.after_tree_r,
								 cc.after_porep,
								 cc.is_snap,
								 cc.after_encode,
								 cc.after_prove,
								 cc.after_prove_msg_success,
								 cc.after_move_storage
							 FROM
								 sectors_meta_pieces mp
									 INNER JOIN
								 CheckCommit cc ON mp.sp_id = cc.sp_id AND mp.sector_num = cc.sector_number
							 WHERE
								 cc.after_commit_msg IS TRUE
						 ),
						 InitialPieces AS (
							 SELECT
								 ip.piece_cid,
								 ip.f05_deal_id,
								 ip.direct_piece_activation_manifest as ddo_pam,
								 cc.after_commit_msg_success,
								 cc.failed,
								 cc.after_sdr,
								 cc.after_tree_r,
								 cc.after_porep,
								 FALSE as is_snap,
								 FALSE as after_encode,
								 FALSE as after_prove,
								 FALSE as after_prove_msg_success,
								 FALSE as after_move_storage
							 FROM
								 sectors_sdr_initial_pieces ip
									 INNER JOIN
								 CheckCommit cc ON ip.sp_id = cc.sp_id AND ip.sector_number = cc.sector_number
							 WHERE
								 cc.after_commit_msg IS FALSE
						 ),
						 InitialPiecesSnap AS (
							 SELECT
								 ip.piece_cid,
								 NULL::bigint as f05_deal_id,
								 ip.direct_piece_activation_manifest as ddo_pam,
								 FALSE as after_commit_msg_success,
								 FALSE as failed,
								 FALSE AS after_sdr,
								 FALSE AS after_tree_r,
								 FALSE AS after_porep,
								 TRUE AS is_snap,
								 cc.after_encode,
								 cc.after_prove,
								 cc.after_prove_msg_success,
								 cc.after_move_storage
							 FROM
								 sectors_snap_initial_pieces ip
									 INNER JOIN
								 CheckCommit cc ON ip.sp_id = cc.sp_id AND ip.sector_number = cc.sector_number
							 WHERE
								 cc.after_commit_msg IS TRUE
						 ),
						 FallbackPieces AS (
							 SELECT
								 op.piece_cid,
								 op.f05_deal_id,
								 op.direct_piece_activation_manifest as ddo_pam,
								 FALSE as after_commit_msg_success,
								 FALSE as failed,
								 FALSE as after_sdr,
								 FALSE as after_tree_r,
								 FALSE as after_porep,
								 op.is_snap as is_snap,
								 FALSE as after_encode,
								 FALSE as after_prove,
								 FALSE as after_prove_msg_success,
								 FALSE as after_move_storage
							 FROM
								 open_sector_pieces op
							 WHERE
								 op.sp_id = $1 AND op.sector_number = $2
							   AND NOT EXISTS (SELECT 1 FROM sectors_sdr_pipeline sp WHERE sp.sp_id = op.sp_id AND sp.sector_number = op.sector_number)
						 )
					SELECT * FROM MetaPieces
					UNION ALL
					SELECT * FROM InitialPiecesSnap
					UNION ALL
					SELECT * FROM InitialPieces
					UNION ALL
					SELECT * FROM FallbackPieces;`, l.minerID, sid)
	if err != nil {
		return lapi.SectorInfo{}, err
	}

	var deals []abi.DealID
	var seenDealIDs = make(map[abi.DealID]struct{})
	var isSnap bool

	if len(ssip) > 0 {
		for _, d := range ssip {
			var dealID abi.DealID

			if d.DealID != nil {
				dealID = abi.DealID(*d.DealID)
			} else if d.DDOPAM != nil {
				var pam miner.PieceActivationManifest
				err := json.Unmarshal([]byte(*d.DDOPAM), &pam)
				if err != nil {
					return lapi.SectorInfo{}, err
				}
				if len(pam.Notify) != 1 {
					continue
				}
				if pam.Notify[0].Address != market2.Address {
					continue
				}
				maj, val, err := typegen.CborReadHeaderBuf(bytes.NewReader(pam.Notify[0].Payload), make([]byte, 9))
				if err != nil {
					return lapi.SectorInfo{}, err
				}
				if maj != typegen.MajUnsignedInt {
					log.Errorw("deal id not an unsigned int", "maj", maj)
					continue
				}
				dealID = abi.DealID(val)
			}

			if !isSnap && d.IsSnap {
				isSnap = true
			}

			if _, ok := seenDealIDs[dealID]; !ok {
				deals = append(deals, dealID)
				seenDealIDs[dealID] = struct{}{}
			}
		}
	}

	spt, err := miner.SealProofTypeFromSectorSize(l.ssize, network.Version20, miner.SealProofVariant_Standard) // good enough, just need this for ssize anyways
	if err != nil {
		return lapi.SectorInfo{}, err
	}

	ret := lapi.SectorInfo{
		SectorID:             sid,
		CommD:                nil,
		CommR:                nil,
		Proof:                nil,
		Deals:                deals,
		Pieces:               nil,
		Ticket:               lapi.SealTicket{},
		Seed:                 lapi.SealSeed{},
		PreCommitMsg:         nil,
		CommitMsg:            nil,
		Retries:              0,
		ToUpgrade:            false,
		ReplicaUpdateMessage: nil,
		LastErr:              "",
		Log:                  nil,
		SealProof:            spt,
		Activation:           0,
		Expiration:           0,
		DealWeight:           big.Zero(),
		VerifiedDealWeight:   big.Zero(),
		InitialPledge:        big.Zero(),
		OnTime:               0,
		Early:                0,
	}

	// If no rows found i.e. sector doesn't exist in DB

	if len(ssip) == 0 {
		ret.State = lapi.SectorState(sealing.UndefinedSectorState)
		return ret, nil
	}
	currentSSIP := ssip[0]

	switch {
	case isSnap && !currentSSIP.Encode:
		ret.State = lapi.SectorState(sealing.UpdateReplica)
	case currentSSIP.Encode && !currentSSIP.SnapProve:
		ret.State = lapi.SectorState(sealing.ProveReplicaUpdate)
	case currentSSIP.SnapProve && !currentSSIP.SnapCommit:
		ret.State = lapi.SectorState(sealing.SubmitReplicaUpdate)
	case currentSSIP.SnapCommit && !currentSSIP.SnapMoveStorage:
		ret.State = lapi.SectorState(sealing.FinalizeReplicaUpdate)
	case currentSSIP.SnapMoveStorage:
		ret.State = lapi.SectorState(sealing.Proving)
	case currentSSIP.Failed:
		ret.State = lapi.SectorState(sealing.FailedUnrecoverable)
	case !isSnap && !currentSSIP.SDR:
		ret.State = lapi.SectorState(sealing.PreCommit1)
	case currentSSIP.SDR && !currentSSIP.Tree:
		ret.State = lapi.SectorState(sealing.PreCommit2)
	case currentSSIP.SDR && currentSSIP.Tree && !currentSSIP.PoRep:
		ret.State = lapi.SectorState(sealing.Committing)
	case currentSSIP.SDR && currentSSIP.Tree && currentSSIP.PoRep && !currentSSIP.Complete:
		ret.State = lapi.SectorState(sealing.FinalizeSector)
	case currentSSIP.Complete:
		ret.State = lapi.SectorState(sealing.Proving)
	default:
		return lapi.SectorInfo{}, nil
	}
	return ret, nil
}

func (l *LMRPCProvider) SectorsList(ctx context.Context) ([]abi.SectorNumber, error) {
	decls, err := l.si.StorageList(ctx)
	if err != nil {
		return nil, err
	}

	var out []abi.SectorNumber
	for _, decl := range decls {
		for _, s := range decl {
			if s.Miner != l.minerID {
				continue
			}

			out = append(out, s.SectorID.Number)
		}
	}

	return out, nil
}

type sectorParts struct {
	sealed, unsealed, cache bool
	inStorage               bool
}

func (l *LMRPCProvider) SectorsSummary(ctx context.Context) (map[lapi.SectorState]int, error) {
	decls, err := l.si.StorageList(ctx)
	if err != nil {
		return nil, err
	}

	states := map[abi.SectorID]sectorParts{}
	for si, decll := range decls {
		sinfo, err := l.si.StorageInfo(ctx, si)
		if err != nil {
			return nil, err
		}

		for _, decl := range decll {
			if decl.Miner != l.minerID {
				continue
			}

			state := states[abi.SectorID{Miner: decl.Miner, Number: decl.SectorID.Number}]
			state.sealed = state.sealed || decl.Has(storiface.FTSealed)
			state.unsealed = state.unsealed || decl.Has(storiface.FTUnsealed)
			state.cache = state.cache || decl.Has(storiface.FTCache)
			state.inStorage = state.inStorage || sinfo.CanStore
			states[abi.SectorID{Miner: decl.Miner, Number: decl.SectorID.Number}] = state
		}
	}

	out := map[lapi.SectorState]int{}
	for _, state := range states {
		switch {
		case state.sealed && state.inStorage:
			out[lapi.SectorState(sealing.Proving)]++
		default:
			// not even close to correct, but good enough for now
			out[lapi.SectorState(sealing.PreCommit1)]++
		}
	}

	return out, nil
}

func (l *LMRPCProvider) SectorsListInStates(ctx context.Context, want []lapi.SectorState) ([]abi.SectorNumber, error) {
	decls, err := l.si.StorageList(ctx)
	if err != nil {
		return nil, err
	}

	wantProving, wantPrecommit1 := false, false
	for _, s := range want {
		switch s {
		case lapi.SectorState(sealing.Proving):
			wantProving = true
		case lapi.SectorState(sealing.PreCommit1):
			wantPrecommit1 = true
		}
	}

	states := map[abi.SectorID]sectorParts{}

	for si, decll := range decls {
		sinfo, err := l.si.StorageInfo(ctx, si)
		if err != nil {
			return nil, err
		}

		for _, decl := range decll {
			if decl.Miner != l.minerID {
				continue
			}

			state := states[abi.SectorID{Miner: decl.Miner, Number: decl.SectorID.Number}]
			state.sealed = state.sealed || decl.Has(storiface.FTSealed)
			state.unsealed = state.unsealed || decl.Has(storiface.FTUnsealed)
			state.cache = state.cache || decl.Has(storiface.FTCache)
			state.inStorage = state.inStorage || sinfo.CanStore
			states[abi.SectorID{Miner: decl.Miner, Number: decl.SectorID.Number}] = state
		}
	}
	var out []abi.SectorNumber

	for id, state := range states {
		switch {
		case state.sealed && state.inStorage:
			if wantProving {
				out = append(out, id.Number)
			}
		default:
			// not even close to correct, but good enough for now
			if wantPrecommit1 {
				out = append(out, id.Number)
			}
		}
	}

	return out, nil
}

func (l *LMRPCProvider) StorageRedeclareLocal(ctx context.Context, id *storiface.ID, b bool) error {
	// so this rescans and redeclares sectors on lotus-miner; whyyy is boost even calling this?

	return nil
}

func (l *LMRPCProvider) IsUnsealed(ctx context.Context, sectorNum abi.SectorNumber, offset abi.UnpaddedPieceSize, length abi.UnpaddedPieceSize) (bool, error) {
	sectorID := abi.SectorID{Miner: l.minerID, Number: sectorNum}

	si, err := l.si.StorageFindSector(ctx, sectorID, storiface.FTUnsealed, 0, false)
	if err != nil {
		return false, err
	}

	// yes, yes, technically sectors can be partially unsealed, but that is never done in practice
	// and can't even be easily done with the current implementation
	return len(si) > 0, nil
}

func (l *LMRPCProvider) ComputeDataCid(ctx context.Context, pieceSize abi.UnpaddedPieceSize, pieceData storiface.Data) (abi.PieceInfo, error) {
	return abi.PieceInfo{}, xerrors.Errorf("not supported")
}

func (l *LMRPCProvider) SectorAddPieceToAny(ctx context.Context, size abi.UnpaddedPieceSize, r storiface.Data, d lapi.PieceDealInfo) (lapi.SectorOffset, error) {
	if d.DealProposal.PieceSize != abi.PaddedPieceSize(l.ssize) {
		return lapi.SectorOffset{}, xerrors.Errorf("only full-sector pieces are supported")
	}

	return lapi.SectorOffset{}, xerrors.Errorf("not supported, use AllocatePieceToSector")
}

func (l *LMRPCProvider) AllocatePieceToSector(ctx context.Context, maddr address.Address, piece lpiece.PieceDealInfo, rawSize int64, source url.URL, header http.Header) (lapi.SectorOffset, error) {
	return l.pi.AllocatePieceToSector(ctx, maddr, piece, rawSize, source, header)
}

func (l *LMRPCProvider) AuthNew(ctx context.Context, perms []auth.Permission) ([]byte, error) {
	type jwtPayload struct {
		Allow []auth.Permission
	}

	p := jwtPayload{
		Allow: perms,
	}

	sk, err := base64.StdEncoding.DecodeString(l.conf.Apis.StorageRPCSecret)
	if err != nil {
		return nil, xerrors.Errorf("decode secret: %w", err)
	}

	return jwt.Sign(&p, jwt.NewHS256(sk))
}

var _ MinimalLMApi = &LMRPCProvider{}
