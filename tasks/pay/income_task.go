package pay

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	"github.com/filecoin-project/curio/tasks/tasknames"

	"github.com/filecoin-project/lotus/chain/actors/policy"
)

const (
	// INCOME_LOG_WINDOW stays under the 360-height eth_getLogs cap of public nodes.
	INCOME_LOG_WINDOW = 300

	// INCOME_GAP_AFTER_RUNS runs failing to read the cursor height make the task skip ahead.
	INCOME_GAP_AFTER_RUNS = 4

	INCOME_RPC_TIMEOUT = 2 * time.Minute

	INCOME_ALERT_SUBSYSTEM    = "PDPIncome"
	INCOME_STALLED_CONDITION  = "IncomeRecordingStalled"
	INCOME_STALLED_AFTER_RUNS = 2

	INCOME_KIND_SETTLE   = "settle"
	INCOME_KIND_ONE_TIME = "one_time"
)

// IncomeTask records Filecoin Pay payments to this node's PDP data sets from chain events.
type IncomeTask struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
	al        curioalerting.AlertingInterface
}

func NewIncomeTask(db *harmonydb.DB, ethClient ethchain.EthClient, al curioalerting.AlertingInterface) *IncomeTask {
	return &IncomeTask{
		db:        db,
		ethClient: ethClient,
		al:        al,
	}
}

type incomeEvent struct {
	TxHash      string
	LogIndex    uint
	BlockNumber uint64
	RailID      int64
	DataSet     int64
	Kind        string
	Net         *big.Int
	Commission  *big.Int
	NetworkFee  *big.Int
	Total       *big.Int
	SettledUpTo *big.Int
}

type dataSetRails struct {
	DataSet   int64  `db:"data_set"`
	PDP       int64  `db:"pdp_rail_id"`
	CacheMiss *int64 `db:"cache_miss_rail_id"`
	Payer     string `db:"payer"`
}

type incomeBatch struct {
	rails  []dataSetRails
	events []incomeEvent
}

type incomeCursor struct {
	Next         int64
	FailedRuns   int
	StalledSince *time.Time
}

type incomeReader struct {
	ethClient  ethchain.EthClient
	payments   common.Address
	fwss       common.Address
	payEvents  *filecoinpayment.PaymentsFilterer
	fwssEvents *FWSS.FilecoinWarmStorageServiceFilterer
	settledID  common.Hash
	oneTimeID  common.Hash
	createdID  common.Hash
	ours       map[common.Address]bool
	rails      map[int64]int64
}

func (t *IncomeTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	head, err := t.blockNumber(ctx)
	if err != nil {
		log.Warnw("could not read the chain head", "error", err)
		return t.stopEarly(ctx, 0, "PDP income is not being recorded: Curio cannot read the chain head from its chain node.")
	}

	cur, err := t.startCursor(ctx, int64(head))
	if err != nil {
		return false, err
	}

	finalized := int64(head) - int64(policy.ChainFinality)
	if finalized < 1 {
		return t.reachedHead(ctx)
	}

	ours, err := t.pdpKeys(ctx)
	if err != nil {
		return false, err
	}
	unread, err := t.syncDataSetRails(ctx, finalized, ours)
	if err != nil {
		return false, err
	}
	if unread > 0 {
		log.Warnw("could not read the rails of some data sets from FWSS; not reading payment events this run", "unread", unread)
		return t.stopEarly(ctx, 0, fmt.Sprintf("PDP income is not being recorded: Curio cannot read %d data sets from FWSS.", unread))
	}

	reader, err := t.newIncomeReader(ctx, ours)
	if err != nil {
		return false, err
	}

	next := cur.Next
	for next <= finalized {
		if !stillOwned() {
			return false, nil
		}

		to := min(next+INCOME_LOG_WINDOW-1, finalized)
		batch, readTo, readErr := reader.readPrefix(ctx, next, to)
		if readTo >= next {
			if err := t.recordBatch(ctx, next, readTo, batch); err != nil {
				return false, err
			}
			next = readTo + 1
			cur.FailedRuns, cur.StalledSince = 0, nil
		}
		if readErr == nil {
			continue
		}

		since := time.Now()
		if cur.StalledSince != nil {
			since = *cur.StalledSince
		}
		if cur.FailedRuns+1 < INCOME_GAP_AFTER_RUNS {
			log.Warnw("could not read payment events", "height", next, "failedRuns", cur.FailedRuns+1, "error", readErr)
			return t.stopEarly(ctx, next, fmt.Sprintf("PDP income has not been recorded since %s: the chain node cannot return payment events at height %d. If %d runs fail there, Curio skips ahead and income paid in the skipped heights is not recorded.",
				since.UTC().Format(time.RFC3339), next, INCOME_GAP_AFTER_RUNS))
		}

		if retried, retryErr := reader.read(ctx, next, next); retryErr == nil {
			if err := t.recordBatch(ctx, next, next, retried); err != nil {
				return false, err
			}
			next++
			cur.FailedRuns, cur.StalledSince = 0, nil
			continue
		}
		stuck := fmt.Sprintf("PDP income has not been recorded since %s: the chain node cannot return payment events.", since.UTC().Format(time.RFC3339))
		latestFrom := finalized - INCOME_LOG_WINDOW + 1
		if latestFrom <= next {
			log.Warnw("could not read payment events within the latest finalized heights", "height", next, "error", readErr)
			return t.stopEarly(ctx, next, stuck)
		}
		if _, _, latestErr := reader.readPrefix(ctx, latestFrom, finalized); latestErr != nil {
			log.Warnw("could not read payment events, nor at the latest finalized heights", "height", next, "error", readErr, "latestError", latestErr)
			return t.stopEarly(ctx, next, stuck)
		}

		// A failed read does not tell a pruned height from other failures, so
		// the skip also drops heights the node still serves.
		if err := t.recordGap(ctx, next, latestFrom-1); err != nil {
			return false, err
		}
		log.Warnw("skipped heights; their income is not recorded", "from", next, "to", latestFrom-1, "error", readErr)
		_ = t.al.EmitEvent(ctx, curioalerting.AlertEvent{
			System:    alertType,
			Subsystem: INCOME_ALERT_SUBSYSTEM,
			Message: fmt.Sprintf("PDP income paid at heights %d-%d is not recorded: Curio skipped them after %d runs could not read payment events at height %d.",
				next, latestFrom-1, INCOME_GAP_AFTER_RUNS, next),
		})
		next = latestFrom
		cur.FailedRuns, cur.StalledSince = 0, nil
	}

	return t.reachedHead(ctx)
}

// blockNumber reads the chain head, bounded by INCOME_RPC_TIMEOUT.
func (t *IncomeTask) blockNumber(ctx context.Context) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, INCOME_RPC_TIMEOUT)
	defer cancel()
	return t.ethClient.BlockNumber(ctx)
}

// pdpKeys returns this node's PDP addresses.
func (t *IncomeTask) pdpKeys(ctx context.Context) (map[common.Address]bool, error) {
	var keys []string
	if err := t.db.Select(ctx, &keys, `SELECT address FROM eth_keys WHERE role = 'pdp'`); err != nil {
		return nil, xerrors.Errorf("selecting PDP keys: %w", err)
	}
	ours := make(map[common.Address]bool, len(keys))
	for _, key := range keys {
		ours[common.HexToAddress(key)] = true
	}
	return ours, nil
}

// syncDataSetRails stores the rails FWSS reports at height for local data sets
// of ours that have none, and returns how many it could not read.
func (t *IncomeTask) syncDataSetRails(ctx context.Context, height int64, ours map[common.Address]bool) (unread int, err error) {
	var pending []int64
	err = t.db.Select(ctx, &pending, `
		SELECT ds.id
		FROM pdp_data_sets ds
		LEFT JOIN pdp_data_set_rails r ON r.data_set = ds.id
		WHERE r.data_set IS NULL
		ORDER BY ds.id
	`)
	if err != nil {
		return 0, xerrors.Errorf("selecting data sets without rails: %w", err)
	}
	if len(pending) == 0 {
		return 0, nil
	}

	viewAddr, err := contract.ResolveViewAddress(ctx, contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService, t.ethClient)
	if err != nil {
		log.Warnw("could not resolve the FWSS view address", "error", err)
		return len(pending), nil
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, t.ethClient)
	if err != nil {
		return 0, xerrors.Errorf("binding the FWSS view: %w", err)
	}

	for _, id := range pending {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		opts := contract.EthCallOpts(ctx)
		opts.BlockNumber = big.NewInt(height)
		info, err := fwssv.GetDataSet(opts, big.NewInt(id))
		if err != nil {
			unread++
			log.Debugw("could not read data set from FWSS", "dataSetId", id, "height", height, "error", err)
			continue
		}
		rails, ok := newDataSetRails(id, info.PdpRailId, info.CacheMissRailId, info.Payer)
		if !ok {
			// Created after height, or not an FWSS data set.
			continue
		}
		if !ours[info.ServiceProvider] {
			log.Warnw("FWSS reports a local data set for another service provider; not recording its income",
				"dataSetId", id, "serviceProvider", info.ServiceProvider, "height", height)
			continue
		}
		_, err = t.db.Exec(ctx, `
			INSERT INTO pdp_data_set_rails (data_set, pdp_rail_id, cache_miss_rail_id, payer)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (data_set) DO NOTHING
		`, rails.DataSet, rails.PDP, rails.CacheMiss, rails.Payer)
		if err != nil {
			return 0, xerrors.Errorf("storing rails of data set %d: %w", id, err)
		}
	}
	return unread, nil
}

// newDataSetRails returns the rails FWSS reports, false without a PDP rail.
func newDataSetRails(dataSet int64, pdpRail, cacheMissRail *big.Int, payer common.Address) (dataSetRails, bool) {
	pdp, ok := railID(pdpRail)
	if !ok {
		return dataSetRails{}, false
	}
	rails := dataSetRails{DataSet: dataSet, PDP: pdp, Payer: payer.Hex()}
	if cacheMiss, ok := railID(cacheMissRail); ok {
		rails.CacheMiss = &cacheMiss
	}
	return rails, true
}

// railID returns id as a rail id, false when it is none.
func railID(id *big.Int) (int64, bool) {
	if id == nil || id.Sign() <= 0 || !id.IsInt64() {
		return 0, false
	}
	return id.Int64(), true
}

// addTo maps the rails in m to their data set.
func (x dataSetRails) addTo(m map[int64]int64) {
	m[x.PDP] = x.DataSet
	if x.CacheMiss != nil {
		m[*x.CacheMiss] = x.DataSet
	}
}

// startCursor starts the cursor at head on the first run and returns it.
func (t *IncomeTask) startCursor(ctx context.Context, head int64) (incomeCursor, error) {
	_, err := t.db.Exec(ctx, `
		UPDATE pdp_income_cursor
		SET next_height = $1, started_height = $1, started_at = NOW()
		WHERE id = 1 AND next_height IS NULL
	`, head)
	if err != nil {
		return incomeCursor{}, xerrors.Errorf("starting the income cursor: %w", err)
	}
	var cur incomeCursor
	err = t.db.QueryRow(ctx, `SELECT next_height, failed_runs, stalled_since FROM pdp_income_cursor WHERE id = 1`).
		Scan(&cur.Next, &cur.FailedRuns, &cur.StalledSince)
	if err != nil {
		return incomeCursor{}, xerrors.Errorf("reading the income cursor: %w", err)
	}
	return cur, nil
}

// newIncomeReader loads the rails stored so far.
func (t *IncomeTask) newIncomeReader(ctx context.Context, ours map[common.Address]bool) (*incomeReader, error) {
	var stored []dataSetRails
	if err := t.db.Select(ctx, &stored, `SELECT data_set, pdp_rail_id, cache_miss_rail_id, payer FROM pdp_data_set_rails`); err != nil {
		return nil, xerrors.Errorf("loading data set rails: %w", err)
	}
	rails := make(map[int64]int64, 2*len(stored))
	for _, x := range stored {
		x.addTo(rails)
	}

	payments, err := filecoinpayment.PaymentContractAddress()
	if err != nil {
		return nil, err
	}
	payEvents, err := filecoinpayment.NewPaymentsFilterer(payments, nil)
	if err != nil {
		return nil, xerrors.Errorf("binding Filecoin Pay events: %w", err)
	}
	payABI, err := filecoinpayment.PaymentsMetaData.GetAbi()
	if err != nil {
		return nil, xerrors.Errorf("loading the Filecoin Pay ABI: %w", err)
	}

	fwss := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	fwssEvents, err := FWSS.NewFilecoinWarmStorageServiceFilterer(fwss, nil)
	if err != nil {
		return nil, xerrors.Errorf("binding FWSS events: %w", err)
	}
	fwssABI, err := FWSS.FilecoinWarmStorageServiceMetaData.GetAbi()
	if err != nil {
		return nil, xerrors.Errorf("loading the FWSS ABI: %w", err)
	}

	return &incomeReader{
		ethClient:  t.ethClient,
		payments:   payments,
		fwss:       fwss,
		payEvents:  payEvents,
		fwssEvents: fwssEvents,
		settledID:  payABI.Events["RailSettled"].ID,
		oneTimeID:  payABI.Events["RailOneTimePaymentProcessed"].ID,
		createdID:  fwssABI.Events["DataSetCreated"].ID,
		ours:       ours,
		rails:      rails,
	}, nil
}

// readPrefix reads from..to, halving failed ranges, and returns the longest
// readable prefix, its last height (from-1 if none) and the error after it.
func (r *incomeReader) readPrefix(ctx context.Context, from, to int64) (incomeBatch, int64, error) {
	batch, err := r.read(ctx, from, to)
	if err == nil {
		return batch, to, nil
	}
	if from == to {
		return incomeBatch{}, from - 1, err
	}
	mid := from + (to-from)/2
	low, lowTo, err := r.readPrefix(ctx, from, mid)
	if err != nil {
		return low, lowTo, err
	}
	high, highTo, err := r.readPrefix(ctx, mid+1, to)
	low.rails = append(low.rails, high.rails...)
	low.events = append(low.events, high.events...)
	return low, highTo, err
}

// read returns the new rails and the payments in from..to from one eth_getLogs
// call, and makes the rails known to later reads. Undecodable events fail it.
func (r *incomeReader) read(ctx context.Context, from, to int64) (incomeBatch, error) {
	ctx, cancel := context.WithTimeout(ctx, INCOME_RPC_TIMEOUT)
	defer cancel()
	logs, err := r.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(from),
		ToBlock:   big.NewInt(to),
		Addresses: []common.Address{r.payments, r.fwss},
		Topics:    [][]common.Hash{{r.settledID, r.oneTimeID, r.createdID}},
	})
	if err != nil {
		return incomeBatch{}, err
	}

	// Creations first, so payments to new rails match whatever the log order.
	var batch incomeBatch
	created := map[int64]int64{}
	for _, l := range logs {
		if l.Removed || l.Address != r.fwss || len(l.Topics) == 0 || l.Topics[0] != r.createdID {
			continue
		}
		c, err := r.fwssEvents.ParseDataSetCreated(l)
		if err != nil {
			return incomeBatch{}, xerrors.Errorf("decoding DataSetCreated in tx %s: %w", l.TxHash, err)
		}
		if !r.ours[c.ServiceProvider] || !c.DataSetId.IsInt64() {
			continue
		}
		rails, ok := newDataSetRails(c.DataSetId.Int64(), c.PdpRailId, c.CacheMissRailId, c.Payer)
		if !ok {
			continue
		}
		rails.addTo(created)
		batch.rails = append(batch.rails, rails)
	}

	for _, l := range logs {
		if l.Removed || l.Address != r.payments || len(l.Topics) < 2 || (l.Topics[0] != r.settledID && l.Topics[0] != r.oneTimeID) {
			continue
		}
		rail := new(big.Int).SetBytes(l.Topics[1].Bytes())
		if !rail.IsInt64() {
			continue
		}
		dataSet, ok := r.rails[rail.Int64()]
		if !ok {
			dataSet, ok = created[rail.Int64()]
		}
		if !ok {
			continue
		}
		ev, err := r.decode(l, rail.Int64(), dataSet)
		if err != nil {
			return incomeBatch{}, err
		}
		batch.events = append(batch.events, ev)
	}

	for rail, dataSet := range created {
		r.rails[rail] = dataSet
	}
	return batch, nil
}

// decode turns a payment log on a known rail into an incomeEvent.
func (r *incomeReader) decode(l types.Log, rail, dataSet int64) (incomeEvent, error) {
	ev := incomeEvent{
		TxHash:      l.TxHash.Hex(),
		LogIndex:    l.Index,
		BlockNumber: l.BlockNumber,
		RailID:      rail,
		DataSet:     dataSet,
	}
	if l.Topics[0] == r.settledID {
		s, err := r.payEvents.ParseRailSettled(l)
		if err != nil {
			return incomeEvent{}, xerrors.Errorf("decoding RailSettled in tx %s: %w", l.TxHash, err)
		}
		ev.Kind = INCOME_KIND_SETTLE
		ev.Net, ev.Commission, ev.NetworkFee = s.TotalNetPayeeAmount, s.OperatorCommission, s.NetworkFee
		ev.Total, ev.SettledUpTo = s.TotalSettledAmount, s.SettledUpTo
		return ev, nil
	}
	p, err := r.payEvents.ParseRailOneTimePaymentProcessed(l)
	if err != nil {
		return incomeEvent{}, xerrors.Errorf("decoding RailOneTimePaymentProcessed in tx %s: %w", l.TxHash, err)
	}
	ev.Kind = INCOME_KIND_ONE_TIME
	ev.Net, ev.Commission, ev.NetworkFee = p.NetPayeeAmount, p.OperatorCommission, p.NetworkFee
	return ev, nil
}

// recordBatch stores batch and moves the cursor, which must be at from, past to.
func (t *IncomeTask) recordBatch(ctx context.Context, from, to int64, batch incomeBatch) error {
	committed, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		for _, x := range batch.rails {
			_, err := tx.Exec(`
				INSERT INTO pdp_data_set_rails (data_set, pdp_rail_id, cache_miss_rail_id, payer)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (data_set) DO NOTHING
			`, x.DataSet, x.PDP, x.CacheMiss, x.Payer)
			if err != nil {
				return false, xerrors.Errorf("storing rails of data set %d: %w", x.DataSet, err)
			}
		}
		for _, ev := range batch.events {
			_, err := tx.Exec(`
				INSERT INTO pdp_income_events (
					tx_hash, log_index, block_number, rail_id, data_set, kind,
					net_amount, operator_commission, network_fee, total_amount, settled_up_to
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7::NUMERIC, $8::NUMERIC, $9::NUMERIC, $10::NUMERIC, $11)
				ON CONFLICT (tx_hash, log_index) DO NOTHING
			`, ev.TxHash, ev.LogIndex, ev.BlockNumber, ev.RailID, ev.DataSet, ev.Kind,
				ev.Net.String(), ev.Commission.String(), ev.NetworkFee.String(), bigIntArg(ev.Total), bigIntArg(ev.SettledUpTo))
			if err != nil {
				return false, xerrors.Errorf("storing payment in tx %s log %d: %w", ev.TxHash, ev.LogIndex, err)
			}
		}
		return advanceIncomeCursor(tx, from, to)
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("recording payments at heights %d-%d: %w", from, to, err)
	}
	if !committed {
		return xerrors.Errorf("recording payments at heights %d-%d was not committed", from, to)
	}
	return nil
}

// bigIntArg returns v as a decimal query argument, NULL when v is nil.
func bigIntArg(v *big.Int) any {
	if v == nil {
		return nil
	}
	return v.String()
}

// recordGap records from..to as skipped and moves the cursor, at from, past to.
func (t *IncomeTask) recordGap(ctx context.Context, from, to int64) error {
	committed, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`UPDATE pdp_income_gaps SET to_height = $1 WHERE to_height = $2`, to, from-1)
		if err != nil {
			return false, xerrors.Errorf("extending income gap: %w", err)
		}
		if n == 0 {
			if _, err := tx.Exec(`INSERT INTO pdp_income_gaps (from_height, to_height) VALUES ($1, $2)`, from, to); err != nil {
				return false, xerrors.Errorf("recording income gap: %w", err)
			}
		}
		return advanceIncomeCursor(tx, from, to)
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("recording income gap at heights %d-%d: %w", from, to, err)
	}
	if !committed {
		return xerrors.Errorf("recording income gap at heights %d-%d was not committed", from, to)
	}
	return nil
}

// advanceIncomeCursor moves the cursor from from to to+1, failing if it is elsewhere.
func advanceIncomeCursor(tx *harmonydb.Tx, from, to int64) (bool, error) {
	n, err := tx.Exec(`
		UPDATE pdp_income_cursor
		SET next_height = $1, failed_runs = 0, stalled_since = NULL
		WHERE id = 1 AND next_height = $2
	`, to+1, from)
	if err != nil {
		return false, xerrors.Errorf("advancing the income cursor: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("the income cursor is no longer at height %d", from)
	}
	return true, nil
}

// stopEarly ends a run before the finalized head, counting a failed read of
// height failedAt (0 if none) while the cursor is still there, and alerts once
// INCOME_STALLED_AFTER_RUNS runs in a row stop early.
func (t *IncomeTask) stopEarly(ctx context.Context, failedAt int64, message string) (bool, error) {
	var runs int
	err := t.db.QueryRow(ctx, `
		UPDATE pdp_income_cursor
		SET stopped_runs = stopped_runs + 1,
		    failed_runs = failed_runs + CASE WHEN next_height = $1 THEN 1 ELSE 0 END,
		    stalled_since = CASE WHEN next_height = $1 THEN COALESCE(stalled_since, NOW()) ELSE stalled_since END
		WHERE id = 1
		RETURNING stopped_runs
	`, failedAt).Scan(&runs)
	if err != nil {
		return false, xerrors.Errorf("counting runs that stopped early: %w", err)
	}
	if runs < INCOME_STALLED_AFTER_RUNS {
		return true, nil
	}
	if err := t.al.ActivateCondition(ctx, incomeStalledCondition(), message); err != nil {
		log.Warnw("could not raise the income alert", "error", err)
	}
	return true, nil
}

// reachedHead ends a run that reached the finalized head and resolves the alert.
func (t *IncomeTask) reachedHead(ctx context.Context) (bool, error) {
	if _, err := t.db.Exec(ctx, `UPDATE pdp_income_cursor SET stopped_runs = 0 WHERE id = 1`); err != nil {
		return false, xerrors.Errorf("clearing runs that stopped early: %w", err)
	}
	if err := t.al.ResolveCondition(ctx, incomeStalledCondition()); err != nil {
		log.Warnw("could not resolve the income alert", "error", err)
	}
	return true, nil
}

// incomeStalledCondition identifies the stalled-income alert condition.
func incomeStalledCondition() curioalerting.AlertCondition {
	return curioalerting.AlertCondition{
		System:    alertType,
		Subsystem: INCOME_ALERT_SUBSYSTEM,
		Condition: INCOME_STALLED_CONDITION,
	}
}

func (t *IncomeTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *IncomeTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: tasknames.PDPv0_Income,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(time.Hour, t),
	}
}

func (t *IncomeTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &IncomeTask{}
var _ = harmonytask.Reg(&IncomeTask{})
