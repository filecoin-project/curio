package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/writer"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commpl "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/promise"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

const CommpTaskPollInterval = 5 * time.Second

type CommpTask struct {
	db  *harmonydb.DB
	sc  *ffi.SealCalls
	api headAPI
	cfg *config.MK12Config

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewCommpTask(db *harmonydb.DB, sc *ffi.SealCalls, api headAPI, cfg *config.MK12Config) *CommpTask {
	ct := &CommpTask{
		db:  db,
		sc:  sc,
		api: api,
		cfg: cfg,
	}

	ctx := context.Background()

	go ct.pollCommPTasks(ctx)
	return ct
}

func (c *CommpTask) pollCommPTasks(ctx context.Context) {
	for {
		// Get all deals which do not have a URL and are not after_find
		var deals []string
		err := c.db.Select(ctx, &deals, `SELECT uuid FROM market_mk12_deal_pipeline 
             WHERE after_find = TRUE AND after_commp = FALSE AND commp_task_id = NULL`)

		if err != nil {
			log.Errorf("getting deal pending commp: %w", err)
			time.Sleep(CommpTaskPollInterval)
			continue
		}

		if len(deals) == 0 {
			time.Sleep(CommpTaskPollInterval)
			continue
		}

		for _, deal := range deals {
			deal := deal

			// create a task for each deal
			c.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// update
				n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET commp_task_id = $1 WHERE uuid = $2 AND commp_task_id IS NULL`, id, deal)
				if err != nil {
					return false, xerrors.Errorf("updating deal pipeline: %w", err)
				}

				// commit only if we updated the piece
				return n > 0, nil
			})
		}
	}
}

func (c *CommpTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var pieces []struct {
		Pcid    string          `db:"piece_cid"`
		Psize   int64           `db:"piece_size"`
		UUID    string          `db:"uuid"`
		URL     *string         `db:"url"`
		Headers json.RawMessage `db:"headers"`
		Size    *int64          `db:"file_size"`
	}

	err = c.db.Select(ctx, &pieces, `SELECT uuid, url, headers, file_size, piece_cid  
								FROM market_mk12_deal_pipeline WHERE commp_task_id = $1`, taskID)

	if err != nil {
		return false, xerrors.Errorf("getting piece details: %w", err)
	}

	if len(pieces) != 1 {
		return false, xerrors.Errorf("expected 1 piece, got %d", len(pieces))
	}
	piece := pieces[0]

	expired, err := checkExpiry(ctx, c.db, c.api, piece.UUID, c.cfg.ExpectedSealDuration)
	if err != nil {
		return false, xerrors.Errorf("deal %s expired: %w", piece.UUID, err)
	}
	if expired {
		return true, nil
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

			reader, _ = padreader.New(pr, uint64(*piece.Size))

		} else {
			// Create a new HTTP request
			req, err := http.NewRequest(http.MethodGet, goUrl.String()+fmt.Sprintf("/data?id=%s", piece.Pcid), nil)
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

		defer func() {
			_ = closer.Close()
		}()

		w := &writer.Writer{}
		written, err := io.CopyBuffer(w, reader, make([]byte, writer.CommPBuf))
		if err != nil {
			return false, xerrors.Errorf("copy into commp writer: %w", err)
		}

		if written != *piece.Size {
			return false, xerrors.Errorf("number of bytes written to CommP writer %d not equal to the file size %d", written, piece.Size)
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

		n, err := c.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET after_commp = TRUE, commp_task_id = NULL WHERE commp_task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("store commp success: updating deal pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("store commp success: updated %d rows", n)
		}

		_, err = c.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET psd_wait_time = NOW() AT TIME ZONE 'UTC' WHERE uuid = $1`, piece.UUID)
		if err != nil {
			return false, xerrors.Errorf("store psd time: updating deal pipeline: %w", err)
		}

		return true, nil
	}

	return false, xerrors.Errorf("failed to find URL for the piece %s in the db", piece.Pcid)

}

func (c *CommpTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (c *CommpTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  10, //TODO make config
		Name: "CommP",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 1 << 30,
		},
		MaxFailures: 3,
	}
}

func (c *CommpTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	c.TF.Set(taskFunc)
}

var _ = harmonytask.Reg(&CommpTask{})
var _ harmonytask.TaskInterface = &CommpTask{}

func failDeal(ctx context.Context, db *harmonydb.DB, deal string, updatePipeline bool, reason string) error {
	n, err := db.Exec(ctx, `UPDATE market_mk12_deals SET error = $1 WHERE uuid = $2`, reason, deal)
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

func checkExpiry(ctx context.Context, db *harmonydb.DB, api headAPI, deal string, buffer config.Duration) (bool, error) {
	var starts []struct {
		StartEpoch int64 `db:"start_epoch"`
	}
	err := db.Select(ctx, &starts, `SELECT start_epoch FROM market_mk12_deals WHERE uuid = $1`, deal)
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

	buff := int64(math.Floor(time.Duration(buffer).Seconds() / float64(build.BlockDelaySecs)))

	if head.Height()+abi.ChainEpoch(buff) > startEPoch {
		err = failDeal(ctx, db, deal, true, fmt.Sprintf("deal proposal must be proven on chain by deal proposal start epoch %d, but it has expired: current chain height: %d",
			startEPoch, head.Height()))
		return true, err
	}
	return false, nil
}