package piecesunseal

import (
	"context"
	"sort"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"
)

// SectorID identifies a sector by storage provider and sector number.
type SectorID struct {
	SpID      int64
	SectorNum int64
}

// Plan is the result of PiecesToSectorsBatch.
type Plan struct {
	// NoDeal: piece CIDs (v1 strings) not found in market_piece_deal.
	NoDeal []string
	// AlreadyTargeted: sectors with target_unseal_state=true, mapped to requested pieces they contain.
	AlreadyTargeted map[SectorID][]string
	// SectorIdToPieces: sectors chosen to unseal, mapped to pieces they will cover.
	SectorIdToPieces map[SectorID][]string
	// SpIdToSectorNum: sectors to unseal grouped by SP for the UPDATE query.
	SpIdToSectorNum map[int64][]int64
}

// TotalSectors returns the number of sectors in SpIdToSectorNum.
func (p *Plan) TotalSectors() int {
	n := 0
	for _, secs := range p.SpIdToSectorNum {
		n += len(secs)
	}
	return n
}

// PiecesToSectorsBatch resolves all piece CIDs to sectors in batch, then returns a minimal set of
// sectors to unseal so every piece is covered at least once, preferring sectors that contain the
// most requested pieces (fewer sectors to unseal).
func PiecesToSectorsBatch(ctx context.Context, db *harmonydb.DB, pieceCids []cid.Cid) (*Plan, error) {
	if len(pieceCids) == 0 {
		return &Plan{}, nil
	}

	// sizeByV1Cid: piece CID v1 string -> padded size.
	// v2 CIDs encode the raw size directly; v1 CIDs need a DB lookup.
	sizeByV1Cid := make(map[string]int64, len(pieceCids))
	var v1CidsForSize []string

	for _, pc := range pieceCids {
		if commcidv2.IsPieceCidV2(pc) {
			pcid1, rawSize, err := commcid.PieceCidV1FromV2(pc)
			if err != nil {
				return nil, xerrors.Errorf("piece CID v2 to v1 %s: %w", pc, err)
			}
			sizeByV1Cid[pcid1.String()] = int64(padreader.PaddedSize(rawSize).Padded())
		} else if commcidv2.IsCidV1PieceCid(pc) {
			if _, seen := sizeByV1Cid[pc.String()]; !seen {
				sizeByV1Cid[pc.String()] = 0
				v1CidsForSize = append(v1CidsForSize, pc.String())
			}
		} else {
			return nil, xerrors.Errorf("unsupported piece CID format %s (only v1 and v2 supported)", pc)
		}
	}

	// Batch lookup sizes for v1 CIDs whose size is not encoded in the CID.
	if len(v1CidsForSize) > 0 {
		var sizeRows []struct {
			PieceCid string `db:"piece_cid"`
			Size     int64  `db:"piece_size"`
		}
		err := db.Select(ctx, &sizeRows, `
			SELECT c.piece_cid,
			       COALESCE(m.piece_size, p.piece_padded_size, 0) AS piece_size
			FROM unnest($1::text[]) AS c(piece_cid)
			LEFT JOIN LATERAL (
			    SELECT piece_size
			    FROM market_piece_metadata
			    WHERE piece_cid = c.piece_cid
			    ORDER BY piece_size DESC LIMIT 1
			) m ON true
			LEFT JOIN LATERAL (
			    SELECT piece_padded_size
			    FROM parked_pieces
			    WHERE piece_cid = c.piece_cid
			    ORDER BY piece_padded_size DESC LIMIT 1
			) p ON true
		`, v1CidsForSize)
		if err != nil {
			return nil, xerrors.Errorf("piece size lookup: %w", err)
		}
		for _, r := range sizeRows {
			if r.Size <= 0 {
				return nil, xerrors.Errorf("piece size not found for %s (not in market_piece_metadata or parked_pieces)", r.PieceCid)
			}
			sizeByV1Cid[r.PieceCid] = r.Size
		}
	}

	// Build parallel arrays for the SQL JOIN.
	cidsArr := make([]string, 0, len(sizeByV1Cid))
	sizesArr := make([]int64, 0, len(sizeByV1Cid))
	for c, s := range sizeByV1Cid {
		cidsArr = append(cidsArr, c)
		sizesArr = append(sizesArr, s)
	}

	// Fetch all (sector, piece) rows for the requested pieces, and whether each sector is already
	// targeted for unseal — one query instead of two.
	var rows []struct {
		SpID              int64  `db:"sp_id"`
		SectorNum         int64  `db:"sector_num"`
		PieceCid          string `db:"piece_cid"`
		TargetUnsealState *bool  `db:"target_unseal_state"`
	}
	err := db.Select(ctx, &rows, `
		SELECT mpd.sp_id,
		       mpd.sector_num,
		       mpd.piece_cid,
		       sm.target_unseal_state
		FROM market_piece_deal mpd
		JOIN unnest($1::text[], $2::bigint[]) AS p(piece_cid, piece_length)
		  ON mpd.piece_cid = p.piece_cid
		 AND mpd.piece_length = p.piece_length
		LEFT JOIN sectors_meta sm
		  ON sm.sp_id = mpd.sp_id
		 AND sm.sector_num = mpd.sector_num
		WHERE mpd.sp_id != -1
	`, cidsArr, sizesArr)
	if err != nil {
		return nil, xerrors.Errorf("market_piece_deal batch lookup: %w", err)
	}

	// Track which requested pieces appear in at least one deal row.
	piecesWithDeals := make(map[string]struct{})
	for _, r := range rows {
		piecesWithDeals[r.PieceCid] = struct{}{}
	}

	// Pieces with no deal.
	var noDeal []string
	for pc := range sizeByV1Cid {
		if _, ok := piecesWithDeals[pc]; !ok {
			noDeal = append(noDeal, pc)
		}
	}
	sort.Strings(noDeal)

	// sector -> set of piece CIDs it contains
	sectorPieces := make(map[SectorID]map[string]struct{})
	alreadyTargeted := make(map[SectorID][]string)
	covered := make(map[string]struct{})

	for _, r := range rows {
		sid := SectorID{SpID: r.SpID, SectorNum: r.SectorNum}
		if r.TargetUnsealState != nil && *r.TargetUnsealState {
			alreadyTargeted[sid] = append(alreadyTargeted[sid], r.PieceCid)
			covered[r.PieceCid] = struct{}{}
			continue
		}
		if sectorPieces[sid] == nil {
			sectorPieces[sid] = make(map[string]struct{})
		}
		sectorPieces[sid][r.PieceCid] = struct{}{}
	}

	removeCoveredPieces := func() {
		for sid, pieces := range sectorPieces {
			for pc := range pieces {
				if _, ok := covered[pc]; ok {
					delete(sectorPieces[sid], pc)
					if len(sectorPieces[sid]) == 0 {
						delete(sectorPieces, sid)
					}
				}
			}
		}
	}
	removeCoveredPieces()

	sectorIdToPieces := make(map[SectorID][]string)
	spIdToSectorNum := make(map[int64][]int64)

	// Greedily pick sectors by most uncovered pieces first.
	for len(sectorPieces) > 0 {
		sectorsByCount := make([]SectorID, 0, len(sectorPieces))
		for sid := range sectorPieces {
			sectorsByCount = append(sectorsByCount, sid)
		}
		sort.SliceStable(sectorsByCount, func(i, j int) bool {
			return len(sectorPieces[sectorsByCount[i]]) > len(sectorPieces[sectorsByCount[j]])
		})

		sid := sectorsByCount[0]
		for pc := range sectorPieces[sid] {
			sectorIdToPieces[sid] = append(sectorIdToPieces[sid], pc)
			covered[pc] = struct{}{}
		}
		sort.Strings(sectorIdToPieces[sid])
		spIdToSectorNum[sid.SpID] = append(spIdToSectorNum[sid.SpID], sid.SectorNum)
		removeCoveredPieces()
	}

	return &Plan{
		NoDeal:           noDeal,
		AlreadyTargeted:  alreadyTargeted,
		SectorIdToPieces: sectorIdToPieces,
		SpIdToSectorNum:  spIdToSectorNum,
	}, nil
}
