package storage_market

import (
	"context"
	"fmt"
	"io"
	"math/bits"
	"net/url"
	"os"
	"strconv"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/writer"
	"github.com/filecoin-project/go-data-segment/datasegment"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/mk20"
)

type AggregateTask struct {
	sm   *CurioStorageDealMarket
	db   *harmonydb.DB
	sc   *ffi.SealCalls
	stor paths.StashStore
	api  headAPI
	max  int
}

func NewAggregateTask(sm *CurioStorageDealMarket, db *harmonydb.DB, sc *ffi.SealCalls, stor paths.StashStore, api headAPI, max int) *AggregateTask {
	return &AggregateTask{
		sm:   sm,
		db:   db,
		sc:   sc,
		stor: stor,
		api:  api,
		max:  max,
	}
}

func (a *AggregateTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var pieces []struct {
		Pcid       string `db:"piece_cid"`
		Psize      int64  `db:"piece_size"`
		RawSize    int64  `db:"raw_size"`
		URL        string `db:"url"`
		ID         string `db:"id"`
		SpID       int64  `db:"sp_id"`
		AggrIndex  int    `db:"aggr_index"`
		Aggregated bool   `db:"aggregated"`
		Aggreation int    `db:"deal_aggregation"`
	}

	err = a.db.Select(ctx, &pieces, `
										SELECT
											url, 
											headers, 
											raw_size, 
											piece_cid, 
											piece_size, 
											id, 
											sp_id, 
											aggr_index
										FROM 
											market_mk20_pipeline 
										WHERE 
											agg_task_id = $1 ORDER BY aggr_index ASC`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting piece details: %w", err)
	}

	if len(pieces) == 0 {
		return false, xerrors.Errorf("no pieces to aggregate for task %d", taskID)
	}

	if len(pieces) == 1 {
		n, err := a.db.Exec(ctx, `UPDATE market_mk20_pipeline SET aggregated = TRUE, agg_task_id = NULL 
                                   WHERE id = $1 
                                     AND agg_task_id = $2`, pieces[0].ID, taskID)
		if err != nil {
			return false, xerrors.Errorf("updating aggregated piece details in DB: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row updated, got %d", n)
		}
		log.Infof("skipping aggregation as deal %s only has 1 piece for task %s", pieces[0].ID, taskID)
		return true, nil
	}

	id := pieces[0].ID
	spid := pieces[0].SpID

	ID, err := ulid.Parse(id)
	if err != nil {
		return false, xerrors.Errorf("parsing deal ID: %w", err)
	}

	deal, err := mk20.DealFromDB(ctx, a.db, ID)
	if err != nil {
		return false, xerrors.Errorf("getting deal details from DB: %w", err)
	}

	rawSize, err := deal.Data.RawSize()
	if err != nil {
		return false, xerrors.Errorf("getting raw size: %w", err)
	}

	var pinfos []abi.PieceInfo
	var readers []io.Reader

	for _, piece := range pieces {
		if piece.Aggregated {
			return false, xerrors.Errorf("piece %s for deal %s already aggregated for task %d", piece.Pcid, piece.ID, taskID)
		}
		if piece.Aggreation != 1 {
			return false, xerrors.Errorf("incorrect aggregation value for piece %s for deal %s for task %d", piece.Pcid, piece.ID, taskID)
		}
		if piece.ID != id || piece.SpID != spid {
			return false, xerrors.Errorf("piece details do not match")
		}
		goUrl, err := url.Parse(piece.URL)
		if err != nil {
			return false, xerrors.Errorf("parsing data URL: %w", err)
		}
		if goUrl.Scheme != "pieceref" {
			return false, xerrors.Errorf("invalid data URL scheme: %s", goUrl.Scheme)
		}

		var reader io.Reader // io.ReadCloser is not supported by padreader
		var closer io.Closer

		refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
		if err != nil {
			return false, xerrors.Errorf("parsing piece reference number: %w", err)
		}

		// get pieceID
		var pieceID []struct {
			PieceID storiface.PieceNumber `db:"piece_id"`
		}
		err = a.db.Select(ctx, &pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum)
		if err != nil {
			return false, xerrors.Errorf("getting pieceID: %w", err)
		}

		if len(pieceID) != 1 {
			return false, xerrors.Errorf("expected 1 pieceID, got %d", len(pieceID))
		}

		pr, err := a.sc.PieceReader(ctx, pieceID[0].PieceID)
		if err != nil {
			return false, xerrors.Errorf("getting piece reader: %w", err)
		}

		closer = pr
		reader = pr
		defer func() {
			_ = closer.Close()
		}()

		pcid, err := cid.Parse(piece.Pcid)
		if err != nil {
			return false, xerrors.Errorf("parsing piece cid: %w", err)
		}

		pinfos = append(pinfos, abi.PieceInfo{
			Size:     abi.PaddedPieceSize(piece.Psize),
			PieceCID: pcid,
		})

		pReader, _ := padreader.New(reader, uint64(piece.RawSize))
		readers = append(readers, pReader)
	}

	_, aggregatedRawSize, err := datasegment.ComputeDealPlacement(pinfos)
	if err != nil {
		return false, xerrors.Errorf("computing aggregated piece size: %w", err)
	}

	overallSize := abi.PaddedPieceSize(aggregatedRawSize)
	// we need to make this the 'next' power of 2 in order to have space for the index
	next := 1 << (64 - bits.LeadingZeros64(uint64(overallSize+256)))

	aggr, err := datasegment.NewAggregate(abi.PaddedPieceSize(next), pinfos)
	if err != nil {
		return false, xerrors.Errorf("creating aggregate: %w", err)
	}

	outR, err := aggr.AggregateObjectReader(readers)
	if err != nil {
		return false, xerrors.Errorf("aggregating piece readers: %w", err)
	}

	w := &writer.Writer{}

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		multiWriter := io.MultiWriter(w, f)

		// Copy data from limitedReader to multiWriter
		n, err := io.CopyBuffer(multiWriter, outR, make([]byte, writer.CommPBuf))
		if err != nil {
			return fmt.Errorf("failed to read and write aggregated piece data: %w", err)
		}

		if n != int64(rawSize) {
			return fmt.Errorf("number of bytes written to CommP writer %d not equal to the file size %d", n, aggregatedRawSize)
		}

		return nil
	}

	stashID, err := a.stor.StashCreate(ctx, int64(next), writeFunc)
	if err != nil {
		return false, xerrors.Errorf("stashing aggregated piece data: %w", err)
	}

	calculatedCommp, err := w.Sum()
	if err != nil {
		return false, xerrors.Errorf("computing commP failed: %w", err)
	}

	if !calculatedCommp.PieceCID.Equals(deal.Data.PieceCID) {
		return false, xerrors.Errorf("commP mismatch calculated %s and supplied %s", calculatedCommp.PieceCID.String(), deal.Data.PieceCID.String())
	}

	if calculatedCommp.PieceSize != deal.Data.Size {
		return false, xerrors.Errorf("commP size mismatch calculated %d and supplied %d", calculatedCommp.PieceSize, deal.Data.Size)
	}

	comm, err := a.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var parkedPieceID int64

		err = tx.QueryRow(`
            INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
            VALUES ($1, $2, $3, TRUE) RETURNING id
        `, calculatedCommp.PieceCID.String(), calculatedCommp.PieceSize, rawSize).Scan(&parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
		}

		// Create a piece ref with data_url being "stashstore://<stash-url>"
		// Get StashURL
		stashURL, err := a.stor.StashURL(stashID)
		if err != nil {
			return false, fmt.Errorf("failed to get stash URL: %w", err)
		}

		// Change scheme to "custore"
		stashURL.Scheme = dealdata.CustoreScheme
		dataURL := stashURL.String()

		var pieceRefID int64
		err = tx.QueryRow(`
            INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
            VALUES ($1, $2, TRUE) RETURNING ref_id
        `, parkedPieceID, dataURL).Scan(&pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_piece_refs entry: %w", err)
		}

		pieceIDUrl := url.URL{
			Scheme: "pieceref",
			Opaque: fmt.Sprintf("%d", pieceRefID),
		}

		// Replace the pipeline piece with a new aggregated piece
		_, err = tx.Exec(`DELETE FROM market_mk20_pipeline WHERE id = $1`, id)
		if err != nil {
			return false, fmt.Errorf("failed to delete pipeline pieces: %w", err)
		}

		ddo := deal.Products.DDOV1
		data := deal.Data

		var allocationID interface{}
		if ddo.AllocationId != nil {
			allocationID = *ddo.AllocationId
		} else {
			allocationID = nil
		}

		n, err := tx.Exec(`INSERT INTO market_mk20_pipeline (
            id, sp_id, contract, client, piece_cid, piece_size, raw_size, url, 
            offline, indexing, announce, allocation_id, duration, 
            piece_aggregation, deal_aggregation, started, downloaded, after_commp, aggregated) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, TRUE, TRUE, TRUE, TRUE)`,
			id, spid, ddo.ContractAddress, ddo.Client.String(), data.PieceCID.String(), data.Size, int64(data.SourceHTTP.RawSize), pieceIDUrl.String(),
			false, ddo.Indexing, ddo.AnnounceToIPNI, allocationID, ddo.Duration,
			data.Format.Aggregate.Type, data.Format.Aggregate.Type)
		if err != nil {
			return false, xerrors.Errorf("inserting aggregated piece in mk20 pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("inserting aggregated piece in mk20 pipeline: %d rows affected", n)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("saving aggregated piece details to DB: %w", err)
	}

	if !comm {
		return false, xerrors.Errorf("failed to commit the transaction")
	}
	return true, nil
}

func (a *AggregateTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	// If no local pieceRef was found then just return first TaskID
	return &ids[0], nil
}

func (a *AggregateTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(a.max),
		Name: "AggregateDeals",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 4 << 30,
		},
		MaxFailures: 3,
	}
}

func (a *AggregateTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	a.sm.adders[pollerAggregate].Set(taskFunc)
}

var _ = harmonytask.Reg(&AggregateTask{})
var _ harmonytask.TaskInterface = &AggregateTask{}
