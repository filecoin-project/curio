package storage_market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/writer"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commpl "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/types"
)

type CommpTask struct {
	sm         *CurioStorageDealMarket
	db         *harmonydb.DB
	sc         *ffi.SealCalls
	api        headAPI
	max        int
	bindToData bool
}

func NewCommpTask(sm *CurioStorageDealMarket, db *harmonydb.DB, sc *ffi.SealCalls, api headAPI, max int, bindToData bool) *CommpTask {
	return &CommpTask{
		sm:         sm,
		db:         db,
		sc:         sc,
		api:        api,
		max:        max,
		bindToData: bindToData,
	}
}

func (c *CommpTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var pieces []struct {
		Pcid      string          `db:"piece_cid"`
		Psize     int64           `db:"piece_size"`
		RawSize   int64           `db:"raw_size"`
		URL       *string         `db:"url"`
		Headers   json.RawMessage `db:"headers"`
		ID        string          `db:"id"`
		SpID      int64           `db:"sp_id"`
		MK12Piece bool            `db:"mk12_source_table"`
		AggrIndex int64           `db:"aggr_index"`
	}

	err = c.db.Select(ctx, &pieces, `SELECT 
											uuid AS id, 
											url, 
											headers, 
											raw_size, 
											piece_cid, 
											piece_size, 
											sp_id,
											0 AS aggr_index,
											TRUE AS mk12_source_table
										FROM 
											market_mk12_deal_pipeline 
										WHERE 
											commp_task_id = $1
										
										UNION ALL
										
										SELECT 
											id, 
											url, 
											NULL AS headers, 
											raw_size, 
											piece_cid, 
											piece_size,  
											sp_id,
											aggr_index,
											FALSE AS mk12_source_table
										FROM 
											market_mk20_pipeline 
										WHERE 
											commp_task_id = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting piece details: %w", err)
	}
	if len(pieces) != 1 {
		return false, xerrors.Errorf("expected 1 piece, got %d", len(pieces))
	}
	piece := pieces[0]

	if piece.MK12Piece {
		expired, err := checkExpiry(ctx, c.db, c.api, piece.ID, c.sm.pin.GetExpectedSealDuration())
		if err != nil {
			return false, xerrors.Errorf("deal %s expired: %w", piece.ID, err)
		}
		if expired {
			return true, nil
		}
	}

	if piece.URL != nil {
		dataUrl := *piece.URL

		goUrl, err := url.Parse(dataUrl)
		if err != nil {
			return false, xerrors.Errorf("parsing data URL: %w", err)
		}

		var reader io.Reader // io.ReadCloser is not supported by padreader
		var closer io.Closer

		if goUrl.Scheme == "pieceref" {
			// url is to a piece reference

			refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
			if err != nil {
				return false, xerrors.Errorf("parsing piece reference number: %w", err)
			}

			// get pieceID
			var pieceID []struct {
				PieceID storiface.PieceNumber `db:"piece_id"`
			}
			err = c.db.Select(ctx, &pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum)
			if err != nil {
				return false, xerrors.Errorf("getting pieceID: %w", err)
			}

			if len(pieceID) != 1 {
				return false, xerrors.Errorf("expected 1 pieceID, got %d", len(pieceID))
			}

			pr, err := c.sc.PieceReader(ctx, pieceID[0].PieceID)
			if err != nil {
				return false, xerrors.Errorf("getting piece reader: %w", err)
			}

			closer = pr
			reader = pr

		} else {
			// Create a new HTTP request
			req, err := http.NewRequest(http.MethodGet, goUrl.String(), nil)
			if err != nil {
				return false, xerrors.Errorf("error creating request: %w", err)
			}

			hdrs := make(http.Header)

			err = json.Unmarshal(piece.Headers, &hdrs)

			if err != nil {
				return false, xerrors.Errorf("error unmarshaling headers: %w", err)
			}

			// Add custom headers for security and authentication
			req.Header = hdrs

			// Create a client and make the request
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				return false, xerrors.Errorf("error making GET request: %w", err)
			}

			// Check if the file is found
			if resp.StatusCode != http.StatusOK {
				return false, xerrors.Errorf("not ok response from HTTP server: %s", resp.Status)
			}

			closer = resp.Body
			reader = resp.Body
		}

		pReader, pSz := padreader.New(reader, uint64(piece.RawSize))

		defer func() {
			_ = closer.Close()
		}()

		w := new(proof.DataCidWriter)
		written, err := io.CopyBuffer(w, pReader, make([]byte, writer.CommPBuf))
		if err != nil {
			return false, xerrors.Errorf("copy into commp writer: %w", err)
		}

		if written != int64(pSz) {
			return false, xerrors.Errorf("number of bytes written to CommP writer %d not equal to the file size %d", written, pSz)
		}

		calculatedCommp, err := w.Sum()
		if err != nil {
			return false, xerrors.Errorf("computing commP failed: %w", err)
		}

		if calculatedCommp.PieceSize < abi.PaddedPieceSize(piece.Psize) {
			// pad the data so that it fills the piece
			rawPaddedCommp, err := commpl.PadCommP(
				// we know how long a pieceCid "hash" is, just blindly extract the trailing 32 bytes
				calculatedCommp.PieceCID.Hash()[len(calculatedCommp.PieceCID.Hash())-32:],
				uint64(calculatedCommp.PieceSize),
				uint64(piece.Psize),
			)
			if err != nil {
				return false, xerrors.Errorf("failed to pad commp: %w", err)
			}
			calculatedCommp.PieceCID, _ = commcid.DataCommitmentV1ToCID(rawPaddedCommp)
		}

		pcid, err := cid.Parse(piece.Pcid)
		if err != nil {
			return false, xerrors.Errorf("parsing piece cid: %w", err)
		}

		if !pcid.Equals(calculatedCommp.PieceCID) {
			return false, xerrors.Errorf("commP mismatch calculated %s and supplied %s", pcid, calculatedCommp.PieceCID)
		}

		var n int

		if piece.MK12Piece {
			n, err = c.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET after_commp = TRUE, psd_wait_time = NOW(), commp_task_id = NULL WHERE commp_task_id = $1`, taskID)
		} else {
			n, err = c.db.Exec(ctx, `UPDATE market_mk20_pipeline SET after_commp = TRUE, commp_task_id = NULL
										 	WHERE id = $1 
											  AND sp_id = $2 
											  AND piece_cid = $3
											  AND piece_size = $4
											  AND raw_size = $5
										 	  AND aggr_index = $6
											  AND downloaded = TRUE
											  AND after_commp = FALSE 
										 	  AND commp_task_id = $7`,
				piece.ID, piece.SpID, piece.Pcid, piece.Psize, piece.RawSize, piece.AggrIndex, taskID)
		}

		if err != nil {
			return false, xerrors.Errorf("store commp success: updating deal pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("store commp success: updated %d rows", n)
		}

		return true, nil
	}

	if piece.MK12Piece {
		return false, xerrors.Errorf("failed to find URL for the piece %s in the db", piece.Pcid)
	}

	return false, xerrors.Errorf("failed to find URL for the mk20 deal piece with id %s, SP %d, CID %s, Size %d and Index %d in the db", piece.ID, piece.SpID, piece.Pcid, piece.Psize, piece.AggrIndex)

}

func (c *CommpTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	// CommP task can be of 2 types
	// 1. Using ParkPiece pieceRef
	// 2. Using remote HTTP reader
	// ParkPiece should be scheduled on same node which has the piece
	// Remote HTTP ones can be scheduled on any node

	if !c.bindToData { //
		id := ids[0]
		return &id, nil
	}

	ctx := context.Background()

	var tasks []struct {
		TaskID    harmonytask.TaskID `db:"commp_task_id"`
		StorageID string             `db:"storage_id"`
		Url       *string            `db:"url"`
	}

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	comm, err := c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = tx.Select(&tasks, `  SELECT 
											commp_task_id, 
											url
										FROM 
											market_mk12_deal_pipeline
										WHERE 
											commp_task_id = ANY ($1)
										
										UNION ALL
										
										SELECT 
											commp_task_id, 
											url
										FROM 
											market_mk20_pipeline
										WHERE 
											commp_task_id = ANY ($1);
										`, indIDs)
		if err != nil {
			return false, xerrors.Errorf("failed to get deal details from DB: %w", err)
		}

		if storiface.FTPiece != 32 {
			panic("storiface.FTPiece != 32")
		}

		for i, task := range tasks {
			if task.Url != nil {
				goUrl, err := url.Parse(*task.Url)
				if err != nil {
					return false, xerrors.Errorf("parsing data URL: %w", err)
				}
				if goUrl.Scheme == "pieceref" {
					refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
					if err != nil {
						return false, xerrors.Errorf("parsing piece reference number: %w", err)
					}

					// get pieceID
					var pieceID []struct {
						PieceID storiface.PieceNumber `db:"piece_id"`
					}
					err = tx.Select(&pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum)
					if err != nil {
						return false, xerrors.Errorf("getting pieceID: %w", err)
					}
					if len(pieceID) == 0 {
						return false, xerrors.Errorf("no pieceID found for ref %d", refNum)
					}

					var sLocation string

					err = tx.QueryRow(`
					SELECT storage_id FROM sector_location 
						WHERE miner_id = 0 AND sector_num = $1 AND sector_filetype = 32`, pieceID[0].PieceID).Scan(&sLocation)

					if err != nil {
						return false, xerrors.Errorf("failed to get storage location from DB: %w", err)
					}

					tasks[i].StorageID = sLocation
				}
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return nil, err
	}

	if !comm {
		return nil, xerrors.Errorf("failed to commit the transaction")
	}

	ls, err := c.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}

	acceptables := map[harmonytask.TaskID]bool{}

	for _, t := range ids {
		acceptables[t] = true
	}

	// debug log
	log.Infow("commp task can accept", "tasks", tasks, "acceptables", acceptables, "ls", ls, "bindToData", c.bindToData, "ids", ids)

	for _, t := range tasks {
		if _, ok := acceptables[t.TaskID]; !ok {
			continue
		}

		for _, l := range ls {
			if string(l.ID) == t.StorageID {
				log.Infow("commp task can accept did accept", "t", t, "l", l)
				return &t.TaskID, nil
			}
		}
	}

	// If no local pieceRef was found then just return first TaskID
	return nil, nil
}

func (c *CommpTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(c.max),
		Name: "CommP",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 1 << 30,
		},
		MaxFailures: 3,
	}
}

func (c *CommpTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	c.sm.adders[pollerCommP].Set(taskFunc)
}

var _ = harmonytask.Reg(&CommpTask{})
var _ harmonytask.TaskInterface = &CommpTask{}

func failDeal(ctx context.Context, db *harmonydb.DB, deal string, updatePipeline bool, reason string) error {
	n, err := db.Exec(ctx, `WITH updated AS (
									UPDATE market_mk12_deals
									SET error = $1
									WHERE uuid = $2
									RETURNING uuid
								)
								UPDATE market_direct_deals
								SET error = $1
								WHERE uuid = $2 AND NOT EXISTS (SELECT 1 FROM updated)`, reason, deal)
	if err != nil {
		return xerrors.Errorf("store deal failure: updating deal pipeline: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("store deal failure: updated %d rows", n)
	}
	if updatePipeline {
		n, err := db.Exec(ctx, `DELETE FROM market_mk12_deal_pipeline WHERE uuid = $1`, deal)
		if err != nil {
			return xerrors.Errorf("store deal pipeline cleanup: updating deal pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("store deal pipeline cleanup: updated %d rows", n)
		}
	}
	return nil
}

type headAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
}

func checkExpiry(ctx context.Context, db *harmonydb.DB, api headAPI, deal string, sealDuration abi.ChainEpoch) (bool, error) {
	var starts []struct {
		StartEpoch int64 `db:"start_epoch"`
	}
	err := db.Select(ctx, &starts, `SELECT start_epoch FROM market_mk12_deals WHERE uuid = $1
										UNION ALL
										SELECT start_epoch FROM market_direct_deals WHERE uuid = $1
										LIMIT 1`, deal)
	if err != nil {
		return false, xerrors.Errorf("failed to get start epoch from DB: %w", err)
	}
	if len(starts) != 1 {
		return false, xerrors.Errorf("expected 1 row but got %d", len(starts))
	}
	startEPoch := abi.ChainEpoch(starts[0].StartEpoch)
	head, err := api.ChainHead(ctx)
	if err != nil {
		return false, err
	}

	if head.Height()+sealDuration > startEPoch {
		err = failDeal(ctx, db, deal, true, fmt.Sprintf("deal proposal must be proven on chain by deal proposal start epoch %d, but it has expired: current chain height: %d",
			startEPoch, head.Height()))
		return true, err
	}
	return false, nil
}
