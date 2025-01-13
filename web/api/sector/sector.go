package sector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/docker/go-units"
	"github.com/gorilla/mux"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	miner2 "github.com/filecoin-project/go-state-types/builtin/v13/miner"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/web/api/apihelper"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

const verifiedPowerGainMul = 9

type cfg struct {
	*deps.Deps
}

type minerDetail struct {
	Addr address.Address
	ID   abi.ActorID
}
type sec struct {
	Sector    abi.SectorNumber
	Terminate bool
}

func Routes(r *mux.Router, deps *deps.Deps) {
	c := &cfg{deps}
	// At menu.html:
	r.Methods("POST").Path("/all").HandlerFunc(c.getSectors)
	r.Methods("POST").Path("/terminate").HandlerFunc(c.terminateSectors)
}

func (c *cfg) terminateSectors(w http.ResponseWriter, r *http.Request) {
	var in []struct {
		MinerAddress string
		Sector       uint64
	}
	apihelper.OrHTTPFail(w, json.NewDecoder(r.Body).Decode(&in))
	toDel := make(map[minerDetail][]sec)
	for _, s := range in {
		maddr, err := address.NewFromString(s.MinerAddress)
		apihelper.OrHTTPFail(w, err)
		mid, err := address.IDFromAddress(maddr)
		apihelper.OrHTTPFail(w, err)
		m := minerDetail{
			Addr: maddr,
			ID:   abi.ActorID(mid),
		}
		toDel[m] = append(toDel[m], sec{Sector: abi.SectorNumber(s.Sector), Terminate: false})
	}

	// We should context.Background to avoid cancellation due to page reload or other possible scenarios
	ctx := context.Background()
	err := c.terminate(ctx, toDel)
	apihelper.OrHTTPFail(w, err)

	// Remove sectors
	for m, sectorList := range toDel {
		for _, s := range sectorList {
			id := abi.SectorID{Miner: m.ID, Number: s.Sector}
			apihelper.OrHTTPFail(w, c.removeSector(ctx, id))
		}
	}
}

func (c *cfg) getSectors(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	apihelper.OrHTTPFail(w, err)
	drawStr := r.FormValue("draw")
	startStr := r.FormValue("start")
	lengthStr := r.FormValue("length")
	var draw, start, length int
	if draw, err = strconv.Atoi(drawStr); err != nil {
		draw = 1
	}
	if start, err = strconv.Atoi(startStr); err != nil {
		start = 0
	}
	if length, err = strconv.Atoi(lengthStr); err != nil {
		length = 10
	}
	if length > 1000 {
		length = 1000
	}
	var totalCount int
	row := c.DB.QueryRow(r.Context(), `
SELECT COUNT(*) 
FROM (
  SELECT DISTINCT miner_id, sector_num 
  FROM sector_location 
  WHERE sector_filetype != 32
) AS t
`)
	err = row.Scan(&totalCount)
	apihelper.OrHTTPFail(w, err)
	type rowKey struct {
		MinerID   int64
		SectorNum int64
	}
	var baseRows []rowKey
	rows, err := c.DB.Query(r.Context(), `
SELECT miner_id, sector_num
FROM sector_location
WHERE sector_filetype != 32
GROUP BY miner_id, sector_num
ORDER BY miner_id, sector_num
OFFSET $1
LIMIT $2
`, start, length)
	apihelper.OrHTTPFail(w, err)
	for rows.Next() {
		var rk rowKey
		err := rows.Scan(&rk.MinerID, &rk.SectorNum)
		apihelper.OrHTTPFail(w, err)
		baseRows = append(baseRows, rk)
	}
	rows.Close()
	type aggregatorRow struct {
		MinerID        int64
		SectorNum      int64
		SectorFiletype int
	}
	var aggregatorRows []*aggregatorRow
	for _, br := range baseRows {
		var sumVal *int
		var sumFiletype int
		r2 := c.DB.QueryRow(r.Context(), `
SELECT COALESCE(SUM(sector_filetype), 0)
FROM sector_location
WHERE miner_id = $1 AND sector_num = $2
  AND sector_filetype != 32
`, br.MinerID, br.SectorNum)
		err := r2.Scan(&sumVal)
		apihelper.OrHTTPFail(w, err)
		if sumVal != nil {
			sumFiletype = *sumVal
		}
		aggregatorRows = append(aggregatorRows, &aggregatorRow{
			MinerID:        br.MinerID,
			SectorNum:      br.SectorNum,
			SectorFiletype: sumFiletype,
		})
	}
	type sector struct {
		MinerID      int64
		SectorNum    int64
		MinerAddress address.Address
		HasSealed    bool
		HasUnsealed  bool
		HasSnap      bool
		ExpiresAt    abi.ChainEpoch
		IsOnChain    bool
		IsFilPlus    bool
		SealInfo     string
		Proving      bool
		Flag         bool
		DealWeight   string
		Deals        string
	}
	sectors := make([]sector, len(aggregatorRows))
	minerToAddr := make(map[int64]address.Address)
	for i, ag := range aggregatorRows {
		var s sector
		s.MinerID = ag.MinerID
		s.SectorNum = ag.SectorNum
		s.HasSealed = ag.SectorFiletype&int(storiface.FTSealed) != 0 ||
			ag.SectorFiletype&int(storiface.FTUpdate) != 0
		s.HasUnsealed = ag.SectorFiletype&int(storiface.FTUnsealed) != 0
		s.HasSnap = ag.SectorFiletype&int(storiface.FTUpdate) != 0
		addr, err := address.NewIDAddress(uint64(ag.MinerID))
		apihelper.OrHTTPFail(w, err)
		s.MinerAddress = addr
		minerToAddr[ag.MinerID] = addr
		sectors[i] = s
	}
	head, err := c.Chain.ChainHead(r.Context())
	apihelper.OrHTTPFail(w, err)
	type sectorID struct {
		mID  int64
		sNum uint64
	}
	sectorIdx := make(map[sectorID]int)
	for i, s := range sectors {
		sectorIdx[sectorID{s.MinerID, uint64(s.SectorNum)}] = i
	}
	for mid, maddr := range minerToAddr {
		info, err := c.getCachedSectorInfo(w, r, maddr, head.Key())
		apihelper.OrHTTPFail(w, err)
		for _, chainy := range info {
			st := chainy.onChain
			key := sectorID{mID: mid, sNum: uint64(st.SectorNumber)}
			i, ok := sectorIdx[key]
			if !ok {
				continue
			}
			sectors[i].IsOnChain = true
			sectors[i].ExpiresAt = st.Expiration
			sectors[i].IsFilPlus = st.VerifiedDealWeight.GreaterThan(big.NewInt(0))
			sz, e2 := st.SealProof.SectorSize()
			if e2 == nil {
				sectors[i].SealInfo = sz.ShortString()
			}
			sectors[i].Proving = chainy.active
			if st.Expiration < head.Height() {
				sectors[i].Flag = true
			}
		}
	}
	if len(aggregatorRows) > 0 {
		type pieceRow1 struct {
			Miner    int64
			Sector   int64
			Size     int64
			DealID   uint64
			Proposal json.RawMessage
			Manifest json.RawMessage
		}
		var allPieces []pieceRow1
		for _, ag := range aggregatorRows {
			rows1, e1 := c.DB.Query(r.Context(), `
SELECT sp_id, sector_number, piece_size, COALESCE(f05_deal_id, 0), f05_deal_proposal, direct_piece_activation_manifest
FROM sectors_sdr_initial_pieces
WHERE sp_id = $1 AND sector_number = $2
`, ag.MinerID, ag.SectorNum)
			apihelper.OrHTTPFail(w, e1)
			for rows1.Next() {
				var pr pieceRow1
				var dealID uint64
				err := rows1.Scan(
					&pr.Miner,
					&pr.Sector,
					&pr.Size,
					&dealID,
					&pr.Proposal,
					&pr.Manifest,
				)
				apihelper.OrHTTPFail(w, err)
				pr.DealID = dealID
				allPieces = append(allPieces, pr)
			}
			rows1.Close()
			var p2 []struct {
				Miner    int64
				Sector   int64
				Size     int64
				DealID   uint64
				Proposal json.RawMessage
				Manifest json.RawMessage
			}
			rows2, e2 := c.DB.Query(r.Context(), `
SELECT sp_id, sector_num, piece_size, COALESCE(f05_deal_id, 0), f05_deal_proposal, ddo_pam
FROM sectors_meta_pieces
WHERE sp_id = $1 AND sector_num = $2
`, ag.MinerID, ag.SectorNum)
			apihelper.OrHTTPFail(w, e2)
			for rows2.Next() {
				var x struct {
					Miner    int64
					Sector   int64
					Size     int64
					DealID   uint64
					Proposal json.RawMessage
					Manifest json.RawMessage
				}
				err := rows2.Scan(
					&x.Miner,
					&x.Sector,
					&x.Size,
					&x.DealID,
					&x.Proposal,
					&x.Manifest,
				)
				apihelper.OrHTTPFail(w, err)
				p2 = append(p2, x)
			}
			rows2.Close()
			for _, row := range p2 {
				allPieces = append(allPieces, pieceRow1{
					Miner:    row.Miner,
					Sector:   row.Sector,
					Size:     row.Size,
					DealID:   row.DealID,
					Proposal: row.Proposal,
					Manifest: row.Manifest,
				})
			}
		}
		pieceMap := make(map[sectorID][]int)
		for i, ap := range allPieces {
			pieceMap[sectorID{mID: ap.Miner, sNum: uint64(ap.Sector)}] = append(pieceMap[sectorID{mID: ap.Miner, sNum: uint64(ap.Sector)}], i)
		}
		for i := range sectors {
			s := &sectors[i]
			sid := sectorID{s.MinerID, uint64(s.SectorNum)}
			idxs, found := pieceMap[sid]
			if !found {
				s.DealWeight = "CC"
				s.Deals = "Market: 0, DDO: 0"
				continue
			}
			var dw, vp float64
			var f05, ddo int
			for _, idx := range idxs {
				piece := allPieces[idx]
				if piece.Proposal != nil {
					var prop *market.DealProposal
					e := json.Unmarshal(piece.Proposal, &prop)
					if e == nil && prop != nil {
						dw += float64(prop.PieceSize)
						if prop.VerifiedDeal {
							vp += float64(prop.PieceSize) * verifiedPowerGainMul
						}
						f05++
					}
				}
				if piece.Manifest != nil {
					var pam *miner.PieceActivationManifest
					e := json.Unmarshal(piece.Manifest, &pam)
					if e == nil && pam != nil {
						dw += float64(pam.Size)
						if pam.VerifiedAllocationKey != nil {
							vp += float64(pam.Size) * verifiedPowerGainMul
						}
						ddo++
					}
				}
			}
			if vp > 0 {
				s.IsFilPlus = true
			}
			if dw <= 0 && vp <= 0 {
				s.DealWeight = "CC"
			} else {
				val := dw
				if vp > 0 {
					val = vp
				}
				s.DealWeight = units.BytesSize(val)
			}
			s.Deals = fmt.Sprintf("Market: %d, DDO: %d", f05, ddo)
		}
	}
	out := map[string]interface{}{
		"draw":            draw,
		"recordsTotal":    totalCount,
		"recordsFiltered": totalCount,
		"data":            sectors,
	}
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(out))
}

type sectorInfo struct {
	onChain *miner.SectorOnChainInfo
	active  bool
}

type sectorCacheEntry struct {
	sectors []sectorInfo
	loading chan struct{}
	time.Time
}

const cacheTimeout = 30 * time.Minute

var mx sync.Mutex
var sectorInfoCache = map[address.Address]sectorCacheEntry{}

// getCachedSectorInfo returns the sector info for the given miner address,
// either from the cache or by querying the chain.
// Cache can be invalidated by setting the "sector_refresh" cookie to "true".
// This is thread-safe.
// Parallel requests share the chain's first response.
func (c *cfg) getCachedSectorInfo(w http.ResponseWriter, r *http.Request, maddr address.Address, headKey types.TipSetKey) ([]sectorInfo, error) {
	mx.Lock()
	v, ok := sectorInfoCache[maddr]
	mx.Unlock()

	if ok && v.loading != nil {
		<-v.loading
		mx.Lock()
		v, ok = sectorInfoCache[maddr]
		mx.Unlock()
	}

	shouldRefreshCookie, found := lo.Find(r.Cookies(), func(item *http.Cookie) bool { return item.Name == "sector_refresh" })
	shouldRefresh := found && shouldRefreshCookie.Value == "true"
	w.Header().Set("Set-Cookie", "sector_refresh=; Max-Age=0; Path=/")

	if !ok || time.Since(v.Time) > cacheTimeout || shouldRefresh {
		v = sectorCacheEntry{nil, make(chan struct{}), time.Now()}
		mx.Lock()
		sectorInfoCache[maddr] = v
		mx.Unlock()

		// Intentionally not using the context from the request, as this is a cache
		onChainInfo, err := c.Chain.StateMinerSectors(context.Background(), maddr, nil, headKey)
		if err != nil {
			mx.Lock()
			delete(sectorInfoCache, maddr)
			close(v.loading)
			mx.Unlock()
			return nil, err
		}
		active, err := c.Chain.StateMinerActiveSectors(context.Background(), maddr, headKey)
		if err != nil {
			mx.Lock()
			delete(sectorInfoCache, maddr)
			close(v.loading)
			mx.Unlock()
			return nil, err
		}
		activebf := bitfield.New()
		for i := range active {
			activebf.Set(uint64(active[i].SectorNumber))
		}
		infos := make([]sectorInfo, len(onChainInfo))
		for i, info := range onChainInfo {
			info := info
			set, err := activebf.IsSet(uint64(info.SectorNumber))
			if err != nil {
				mx.Lock()
				delete(sectorInfoCache, maddr)
				close(v.loading)
				mx.Unlock()
				return nil, err
			}
			infos[i] = sectorInfo{
				onChain: info,
				active:  set,
			}
		}
		mx.Lock()
		sectorInfoCache[maddr] = sectorCacheEntry{infos, nil, time.Now()}
		close(v.loading)
		mx.Unlock()
		return infos, nil
	}
	return v.sectors, nil
}

func (c *cfg) shouldTerminate(ctx context.Context, smap map[minerDetail][]sec) (map[minerDetail][]sec, error) {
	ret := make(map[minerDetail][]sec)

	head, err := c.Chain.ChainHead(ctx)
	if err != nil {
		return nil, err
	}

	for m, sectors := range smap {
		mact, err := c.Chain.StateGetActor(ctx, m.Addr, head.Key())
		if err != nil {
			return nil, err
		}
		tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(c.Chain), blockstore.NewMemory())
		mas, err := miner.Load(adt.WrapStore(ctx, cbor.NewCborStore(tbs)), mact)
		if err != nil {
			return nil, err
		}

		liveSectors, err := miner.AllPartSectors(mas, miner.Partition.LiveSectors)
		if err != nil {
			return nil, fmt.Errorf("getting live sector sets for miner %s: %w", m, err)
		}

		for i := range sectors {
			ok, err := liveSectors.IsSet(uint64(sectors[i].Sector))
			if err != nil {
				return nil, err
			}

			if ok {
				ret[m] = append(smap[m], sec{
					Sector:    sectors[i].Sector,
					Terminate: true,
				})
				continue
			}
			ret[m] = append(smap[m], sec{
				Sector:    sectors[i].Sector,
				Terminate: false,
			})
		}
	}

	return ret, nil
}

func (c *cfg) removeSector(ctx context.Context, sector abi.SectorID) error {
	var err error

	if rerr := c.Stor.Remove(ctx, sector, storiface.FTSealed, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (sealed): %w", rerr))
	}
	if rerr := c.Stor.Remove(ctx, sector, storiface.FTCache, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (cache): %w", rerr))
	}
	if rerr := c.Stor.Remove(ctx, sector, storiface.FTUnsealed, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (unsealed): %w", rerr))
	}
	if rerr := c.Stor.Remove(ctx, sector, storiface.FTUpdate, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (unsealed): %w", rerr))
	}
	if rerr := c.Stor.Remove(ctx, sector, storiface.FTUpdateCache, true, nil); rerr != nil {
		err = multierror.Append(err, xerrors.Errorf("removing sector (unsealed): %w", rerr))
	}

	return err
}

const batchSize = 100

func (c *cfg) terminate(ctx context.Context, toDel map[minerDetail][]sec) error {
	del, err := c.shouldTerminate(ctx, toDel)
	if err != nil {
		return err
	}

	var msgs []cid.Cid

	for m, sectorNumbers := range del {
		maddr := m.Addr
		mi, err := c.Chain.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		var terminationDeclarationParams []miner2.TerminationDeclaration

		// Get Deadline/Partition for all sectors.
		for _, sector := range sectorNumbers {
			sectorNum := sector.Sector
			sectorbit := bitfield.New()
			sectorbit.Set(uint64(sectorNum))

			loca, err := c.Chain.StateSectorPartition(ctx, maddr, sectorNum, types.EmptyTSK)
			if err != nil {
				return fmt.Errorf("get state sector partition %s", err)
			}

			para := miner2.TerminationDeclaration{
				Deadline:  loca.Deadline,
				Partition: loca.Partition,
				Sectors:   sectorbit,
			}

			terminationDeclarationParams = append(terminationDeclarationParams, para)
		}

		// Batch message for batchSize
		var batches [][]miner2.TerminationDeclaration
		for i := 0; i < len(terminationDeclarationParams); i += batchSize {
			batch := terminationDeclarationParams[i:min(i+batchSize, len(terminationDeclarationParams))]
			batches = append(batches, batch)
		}

		// Send messages for all batches
		for _, batch := range batches {
			terminateSectorParams := &miner2.TerminateSectorsParams{
				Terminations: batch,
			}

			sp, errA := actors.SerializeParams(terminateSectorParams)
			if errA != nil {
				return xerrors.Errorf("serializing params: %w", errA)
			}

			smsg, err := c.Chain.MpoolPushMessage(ctx, &types.Message{
				From:   mi.Worker,
				To:     maddr,
				Method: builtin.MethodsMiner.TerminateSectors,

				Value:  big.Zero(),
				Params: sp,
			}, nil)
			if err != nil {
				return xerrors.Errorf("mpool push message: %w", err)
			}

			msgs = append(msgs, smsg.Cid())

			log.Infof("sent termination message: %s", smsg.Cid())
		}
	}

	// wait for msgs to get mined into a block for all minerID
	eg := errgroup.Group{}
	eg.SetLimit(10)
	for _, msg := range msgs {
		m := msg
		eg.Go(func() error {
			wait, err := c.Chain.StateWaitMsg(ctx, m, 2, 2000, true)
			if err != nil {
				log.Errorf("timeout waiting for message to land on chain %s", wait.Message)
				return fmt.Errorf("timeout waiting for message to land on chain %s", wait.Message)
			}

			if wait.Receipt.ExitCode.IsError() {
				log.Errorf("failed to execute message %s: %d", wait.Message, wait.Receipt.ExitCode)
				return fmt.Errorf("failed to execute message %s: %w", wait.Message, wait.Receipt.ExitCode)
			}
			return nil
		})
	}
	return eg.Wait()
}
