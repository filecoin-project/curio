package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/promise"
)

const FindURLTaskPollInterval = 3 * time.Second
const SizeHeader = "Filecoin-Piece-RawSize"

var log = logging.Logger("deal-handler")

type FindURLTask struct {
	db   *harmonydb.DB
	sc   *ffi.SealCalls
	api  headAPI
	urls map[string]http.Header
	cfg  *config.MK12Config

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewFindURLTask(db *harmonydb.DB, sc *ffi.SealCalls, cfg config.DealConfig, api headAPI) *FindURLTask {

	urls := make(map[string]http.Header)
	for _, curl := range cfg.PieceLocator {
		urls[curl.URL] = curl.Headers
	}

	ft := &FindURLTask{
		db:   db,
		sc:   sc,
		urls: urls,
		api:  api,
		cfg:  &cfg.MK12,
	}

	ctx := context.Background()

	go ft.pollFindURLTasks(ctx)
	return ft
}

type apiece struct {
	Pcid string  `db:"piece_cid"`
	UUID string  `db:"uuid"`
	URL  *string `db:"url"`
}

func (f *FindURLTask) pollFindURLTasks(ctx context.Context) {
	for {
		// Get all deals which do not have a URL and are not after_find
		var deals []string
		err := f.db.Select(ctx, &deals, `SELECT uuid FROM market_mk12_deal_pipeline 
             WHERE after_find = FALSE AND url = NULL AND find_task_id = NULL`)

		if err != nil {
			log.Errorf("getting deal without URL: %w", err)
			time.Sleep(FindURLTaskPollInterval)
			continue
		}

		if len(deals) == 0 {
			time.Sleep(FindURLTaskPollInterval)
			continue
		}

		for _, deal := range deals {
			deal := deal

			// create a task for each deal
			f.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// update
				n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET find_task_id = $1 WHERE uuid = $2 AND find_task_id IS NULL`, id, deal)
				if err != nil {
					return false, xerrors.Errorf("updating deal pipeline: %w", err)
				}

				// commit only if we updated the piece
				return n > 0, nil
			})
		}
	}
}

func (f *FindURLTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var apieces []apiece
	err = f.db.Select(ctx, &apieces, `SELECT uuid, url, piece_cid  
								FROM market_mk12_deal_pipeline WHERE find_task_id = $1`, taskID)

	if err != nil {
		return false, xerrors.Errorf("getting piece details: %w", err)
	}

	if len(apieces) != 1 {
		return false, xerrors.Errorf("expected 1 piece, got %d", len(apieces))
	}
	p := apieces[0]

	expired, err := checkExpiry(ctx, f.db, f.api, p.UUID, f.cfg.ExpectedSealDuration)
	if err != nil {
		return false, xerrors.Errorf("deal %s expired: %w", p.UUID, err)
	}
	if expired {
		return true, nil
	}

	if p.URL == nil {
		err = f.findURL(ctx, taskID, p.Pcid)
		if err != nil {
			return false, err
		}
		return true, nil
	} else {
		log.Infof("Deal %s already has a URL assigned to it", p.UUID)
	}

	_, err = f.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET after_find = TRUE, find_task_id = NULL  
                           WHERE find_task_id = $1`, taskID)

	if err != nil {
		return false, xerrors.Errorf("store FindURL Success %s: updating pipeline: %w", p.Pcid, err)
	}

	return true, nil
}

func (f *FindURLTask) findURL(ctx context.Context, taskID harmonytask.TaskID, pcid string) error {

	// Check if DB has a URL
	var goUrls []struct {
		Url     string          `db:"url"`
		Headerb json.RawMessage `db:"headers"`
	}

	err := f.db.Select(ctx, &goUrls, `SELECT url, headers   
								FROM market_offline_urls WHERE piece_cid = $1`, pcid)

	if err != nil {
		return xerrors.Errorf("getting url and headers from db: %w", err)
	}

	if len(goUrls) > 1 {
		return xerrors.Errorf("expected 1 row per piece, got %d", len(goUrls))
	}

	if len(goUrls) == 1 {
		_, err := f.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET url = $1, headers = $2  
                           WHERE find_task_id = $3`, goUrls[0].Url, goUrls[0].Headerb, taskID)
		if err != nil {
			return xerrors.Errorf("store url for piece %s: updating pipeline: %w", pcid, err)
		}

		return nil
	}

	// Check if We can find the URL for this piece on remote servers i.e. len(goUrls) == 0
	for rUrl, headers := range f.urls {
		// Create a new HTTP request
		urlString := fmt.Sprintf("%s?piece=%s", rUrl, pcid)
		req, err := http.NewRequest(http.MethodGet, urlString, nil)
		if err != nil {
			return xerrors.Errorf("error creating request: %w", err)
		}

		req.Header = headers

		// Create a client and make the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return xerrors.Errorf("error making GET request: %w", err)
		}

		// Check the response code for 404
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode != 404 {
				return xerrors.Errorf("not ok response from HTTP server: %s", resp.Status)
			}
			continue
		}

		hdrs, err := json.Marshal(headers)
		if err != nil {
			return xerrors.Errorf("marshaling headers: %w", err)
		}

		rawSizeStr := resp.Header.Get(SizeHeader)
		if rawSizeStr == "" {
			continue
		}
		rawSize, err := strconv.ParseInt(rawSizeStr, 10, 64)
		if err != nil {
			return xerrors.Errorf("failed to parse the raw size: %w", err)
		}

		_, err = f.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET url = $1, headers = $2, file_size = %3   
                           WHERE find_task_id = $4`, urlString, hdrs, rawSize, taskID)
		if err != nil {
			return xerrors.Errorf("store url for piece %s: updating pipeline: %w", pcid, err)
		}

		return nil
	}

	return xerrors.Errorf("no suitable URL found for piece %s in DB or supplied remote piece servers", pcid)

}

func (f *FindURLTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (f *FindURLTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  10, //TODO make config
		Name: "FindURL",
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 10,
	}
}

func (f *FindURLTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	f.TF.Set(taskFunc)
}

var _ = harmonytask.Reg(&FindURLTask{})
var _ harmonytask.TaskInterface = &FindURLTask{}
