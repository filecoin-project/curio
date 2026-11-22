package pay

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

// incomeChain is a fake chain node.
type incomeChain struct {
	ethchain.EthClient
	t              *testing.T
	head           uint64
	headErr        error
	payments       common.Address
	fwss           common.Address
	provider       common.Address
	getDataSetID   []byte
	calls          map[string][]byte
	failed         map[string]bool
	logs           []types.Log
	fail           func(from, to int64) bool
	queries        [][2]int64
	dataSetHeights []int64
}

func newIncomeChain(t *testing.T, head uint64) *incomeChain {
	t.Helper()
	t.Setenv("CURIO_DEVNET_PAYMENTS_ADDRESS", "0x0000000000000000000000000000000000000001")
	t.Setenv("CURIO_DEVNET_PDP_VERIFIER_ADDRESS", "0x0000000000000000000000000000000000000002")
	t.Setenv("CURIO_DEVNET_FWSS_ADDRESS", "0x0000000000000000000000000000000000000003")

	payments, err := filecoinpayment.PaymentContractAddress()
	require.NoError(t, err)
	view, err := FWSS.FilecoinWarmStorageServiceStateViewMetaData.GetAbi()
	require.NoError(t, err)

	c := &incomeChain{
		t:            t,
		head:         head,
		payments:     payments,
		fwss:         contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService,
		provider:     common.HexToAddress("0x00000000000000000000000000000000000000c0"),
		getDataSetID: view.Methods["getDataSet"].ID,
		calls:        map[string][]byte{},
		failed:       map[string]bool{},
	}
	c.respond(contract.ContractWithViewMetaData, "viewContractAddress", nil, common.HexToAddress("0x0000000000000000000000000000000000000004"))
	return c
}

func (c *incomeChain) respond(metadata *bind.MetaData, name string, args []any, value any) {
	c.t.Helper()
	parsed, err := metadata.GetAbi()
	require.NoError(c.t, err)
	data, err := parsed.Pack(name, args...)
	require.NoError(c.t, err)
	response, err := parsed.Methods[name].Outputs.Pack(value)
	require.NoError(c.t, err)
	c.calls[string(data)] = response
	delete(c.failed, string(data))
}

func (c *incomeChain) getDataSetCall(id int64) string {
	c.t.Helper()
	parsed, err := FWSS.FilecoinWarmStorageServiceStateViewMetaData.GetAbi()
	require.NoError(c.t, err)
	data, err := parsed.Pack("getDataSet", big.NewInt(id))
	require.NoError(c.t, err)
	return string(data)
}

// dataSet makes FWSS report a data set of this node's PDP key; 0 means no rail.
func (c *incomeChain) dataSet(id, pdpRail, cacheMissRail int64, payer common.Address) {
	c.dataSetOf(c.provider, id, pdpRail, cacheMissRail, payer)
}

func (c *incomeChain) dataSetOf(provider common.Address, id, pdpRail, cacheMissRail int64, payer common.Address) {
	c.respond(FWSS.FilecoinWarmStorageServiceStateViewMetaData, "getDataSet", []any{big.NewInt(id)},
		FWSS.FilecoinWarmStorageServiceDataSetInfoView{
			PdpRailId:       big.NewInt(pdpRail),
			CacheMissRailId: big.NewInt(cacheMissRail),
			CdnRailId:       big.NewInt(0),
			Payer:           payer,
			Payee:           common.Address{},
			ServiceProvider: provider,
			CommissionBps:   big.NewInt(0),
			ClientDataSetId: big.NewInt(0),
			PdpEndEpoch:     big.NewInt(0),
			ProviderId:      big.NewInt(0),
			DataSetId:       big.NewInt(id),
		})
}

func (c *incomeChain) dataSetFails(id int64) {
	call := c.getDataSetCall(id)
	delete(c.calls, call)
	c.failed[call] = true
}

// createdLog returns a DataSetCreated log; 0 means no rail.
func (c *incomeChain) createdLog(provider common.Address, id, pdpRail, cacheMissRail int64, payer common.Address, block uint64, tx string, index uint) types.Log {
	c.t.Helper()
	parsed, err := FWSS.FilecoinWarmStorageServiceMetaData.GetAbi()
	require.NoError(c.t, err)
	event := parsed.Events["DataSetCreated"]
	data, err := event.Inputs.NonIndexed().Pack(big.NewInt(pdpRail), big.NewInt(cacheMissRail), big.NewInt(0), payer, provider, provider, []string{}, []string{})
	require.NoError(c.t, err)
	return types.Log{
		Address:     c.fwss,
		Topics:      []common.Hash{event.ID, common.BigToHash(big.NewInt(id)), common.BigToHash(big.NewInt(1))},
		Data:        data,
		BlockNumber: block,
		TxHash:      common.HexToHash(tx),
		Index:       index,
	}
}

func (c *incomeChain) BlockNumber(ctx context.Context) (uint64, error) {
	c.t.Helper()
	_, bounded := ctx.Deadline()
	require.True(c.t, bounded, "chain head reads must time out")
	if c.headErr != nil {
		return 0, c.headErr
	}
	return c.head, nil
}

func (c *incomeChain) CallContract(_ context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	c.t.Helper()
	if bytes.HasPrefix(msg.Data, c.getDataSetID) {
		require.NotNil(c.t, block, "data sets must be read at a fixed height")
		c.dataSetHeights = append(c.dataSetHeights, block.Int64())
	}
	if c.failed[string(msg.Data)] {
		return nil, fmt.Errorf("call reverted")
	}
	response, ok := c.calls[string(msg.Data)]
	require.True(c.t, ok, "unexpected contract call: %x", msg.Data)
	return response, nil
}

func (c *incomeChain) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	c.t.Helper()
	_, bounded := ctx.Deadline()
	require.True(c.t, bounded, "log reads must time out")
	require.Equal(c.t, []common.Address{c.payments, c.fwss}, q.Addresses)
	from, to := q.FromBlock.Int64(), q.ToBlock.Int64()
	c.queries = append(c.queries, [2]int64{from, to})
	if c.fail != nil && c.fail(from, to) {
		return nil, fmt.Errorf("requested heights are not available")
	}

	wanted := map[common.Hash]bool{}
	if len(q.Topics) > 0 {
		for _, topic := range q.Topics[0] {
			wanted[topic] = true
		}
	}
	var out []types.Log
	for _, l := range c.logs {
		if int64(l.BlockNumber) < from || int64(l.BlockNumber) > to {
			continue
		}
		if l.Address != c.payments && l.Address != c.fwss {
			continue
		}
		if len(wanted) > 0 && !wanted[l.Topics[0]] {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func incomeEventLog(t *testing.T, name string, rail int64, block uint64, tx string, index uint, values ...any) types.Log {
	t.Helper()
	parsed, err := filecoinpayment.PaymentsMetaData.GetAbi()
	require.NoError(t, err)
	payments, err := filecoinpayment.PaymentContractAddress()
	require.NoError(t, err)
	event := parsed.Events[name]
	data, err := event.Inputs.NonIndexed().Pack(values...)
	require.NoError(t, err)
	return types.Log{
		Address:     payments,
		Topics:      []common.Hash{event.ID, common.BigToHash(big.NewInt(rail))},
		Data:        data,
		BlockNumber: block,
		TxHash:      common.HexToHash(tx),
		Index:       index,
	}
}

// settleLog returns a RailSettled log; the amounts are total, net, commission, fee.
func settleLog(t *testing.T, rail int64, block uint64, tx string, index uint, total, net, commission, fee *big.Int, settledUpTo int64) types.Log {
	return incomeEventLog(t, "RailSettled", rail, block, tx, index, total, net, commission, fee, big.NewInt(settledUpTo))
}

// oneTimeLog returns a RailOneTimePaymentProcessed log; the amounts are net, commission, fee.
func oneTimeLog(t *testing.T, rail int64, block uint64, tx string, index uint, net, commission, fee *big.Int) types.Log {
	return incomeEventLog(t, "RailOneTimePaymentProcessed", rail, block, tx, index, net, commission, fee)
}

type incomeAlerts struct {
	events []curioalerting.AlertEvent
	active map[string]string
}

func (a *incomeAlerts) EmitEvent(_ context.Context, event curioalerting.AlertEvent) error {
	a.events = append(a.events, event)
	return nil
}

func (a *incomeAlerts) ActivateCondition(_ context.Context, condition curioalerting.AlertCondition, message string) error {
	a.active[condition.Condition] = message
	return nil
}

func (a *incomeAlerts) ResolveCondition(_ context.Context, condition curioalerting.AlertCondition) error {
	delete(a.active, condition.Condition)
	return nil
}

func (a *incomeAlerts) stalled() bool {
	_, ok := a.active[INCOME_STALLED_CONDITION]
	return ok
}

type incomeTest struct {
	t      *testing.T
	db     *harmonydb.DB
	chain  *incomeChain
	alerts *incomeAlerts
	task   *IncomeTask
}

func newIncomeTest(t *testing.T, head uint64) *incomeTest {
	t.Helper()
	chain := newIncomeChain(t, head)
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, []byte("income"), "income")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO eth_keys (address, private_key, role) VALUES ($1, $2, 'pdp')`, chain.provider.Hex(), []byte("key"))
	require.NoError(t, err)
	alerts := &incomeAlerts{active: map[string]string{}}
	return &incomeTest{t: t, db: db, chain: chain, alerts: alerts, task: NewIncomeTask(db, chain, alerts)}
}

func (it *incomeTest) addDataSet(id int64) {
	it.t.Helper()
	_, err := it.db.Exec(it.t.Context(), `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready)
		VALUES ($1, $2, 'income', 100, 10, FALSE)
	`, id, fmt.Sprintf("0x%064x", id))
	require.NoError(it.t, err)
}

func (it *incomeTest) run() {
	it.t.Helper()
	done, err := it.task.Do(it.t.Context(), 0, func() bool { return true })
	require.NoError(it.t, err)
	require.True(it.t, done)
}

func (it *incomeTest) runs(n int) {
	it.t.Helper()
	for range n {
		it.run()
	}
}

type incomeCursorRow struct {
	Next         int64        `db:"next_height"`
	Started      int64        `db:"started_height"`
	FailedRuns   int          `db:"failed_runs"`
	StalledSince sql.NullTime `db:"stalled_since"`
}

func (it *incomeTest) cursor() incomeCursorRow {
	it.t.Helper()
	var rows []incomeCursorRow
	require.NoError(it.t, it.db.Select(it.t.Context(), &rows, `SELECT next_height, started_height, failed_runs, stalled_since FROM pdp_income_cursor`))
	require.Len(it.t, rows, 1)
	return rows[0]
}

func (it *incomeTest) setCursor(next int64) {
	it.t.Helper()
	_, err := it.db.Exec(it.t.Context(), `UPDATE pdp_income_cursor SET next_height = $1`, next)
	require.NoError(it.t, err)
}

type incomeEventRow struct {
	TxHash      string         `db:"tx_hash"`
	LogIndex    int64          `db:"log_index"`
	BlockNumber int64          `db:"block_number"`
	RailID      int64          `db:"rail_id"`
	DataSet     int64          `db:"data_set"`
	Kind        string         `db:"kind"`
	Net         string         `db:"net_amount"`
	Commission  string         `db:"operator_commission"`
	Fee         string         `db:"network_fee"`
	Total       sql.NullString `db:"total_amount"`
	SettledUpTo sql.NullInt64  `db:"settled_up_to"`
}

func (it *incomeTest) events() []incomeEventRow {
	it.t.Helper()
	var rows []incomeEventRow
	require.NoError(it.t, it.db.Select(it.t.Context(), &rows, `
		SELECT tx_hash, log_index, block_number, rail_id, data_set, kind,
		       net_amount::TEXT AS net_amount, operator_commission::TEXT AS operator_commission,
		       network_fee::TEXT AS network_fee, total_amount::TEXT AS total_amount, settled_up_to
		FROM pdp_income_events
		ORDER BY block_number, log_index
	`))
	return rows
}

func (it *incomeTest) eventHeights() []int64 {
	it.t.Helper()
	var heights []int64
	for _, ev := range it.events() {
		heights = append(heights, ev.BlockNumber)
	}
	return heights
}

type incomeGapRow struct {
	From int64 `db:"from_height"`
	To   int64 `db:"to_height"`
}

func (it *incomeTest) gaps() []incomeGapRow {
	it.t.Helper()
	var rows []incomeGapRow
	require.NoError(it.t, it.db.Select(it.t.Context(), &rows, `SELECT from_height, to_height FROM pdp_income_gaps ORDER BY from_height`))
	return rows
}

func (it *incomeTest) hasRails(dataSet int64) bool {
	it.t.Helper()
	var n int
	require.NoError(it.t, it.db.QueryRow(it.t.Context(), `SELECT COUNT(*) FROM pdp_data_set_rails WHERE data_set = $1`, dataSet).Scan(&n))
	return n == 1
}

var (
	incomePayerA = common.HexToAddress("0x00000000000000000000000000000000000000aa")
	incomePayerB = common.HexToAddress("0x00000000000000000000000000000000000000bb")
)

func amount(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return v
}

// startedIncomeTest starts the cursor at 5000, with data set 1 on rails 11 and
// 12 (payer A) and data set 2 on rail 21 (payer B).
func startedIncomeTest(t *testing.T) *incomeTest {
	t.Helper()
	it := newIncomeTest(t, 5000)
	it.addDataSet(1)
	it.addDataSet(2)
	it.chain.dataSet(1, 11, 12, incomePayerA)
	it.chain.dataSet(2, 21, 0, incomePayerB)
	it.run()
	return it
}

func TestIntegration_Income_RecordsPaymentsToLocalRails(t *testing.T) {
	it := startedIncomeTest(t)

	bigNet := amount("123456789012345678901234567890")
	it.chain.logs = []types.Log{
		settleLog(t, 11, 5010, "0xa1", 3, amount("1005"), amount("1000"), big.NewInt(0), big.NewInt(5), 4990),
		settleLog(t, 99, 5020, "0xa2", 0, amount("7"), amount("7"), big.NewInt(0), big.NewInt(0), 5000),
		func() types.Log {
			l := settleLog(t, 11, 5030, "0xa3", 1, amount("9"), amount("9"), big.NewInt(0), big.NewInt(0), 5000)
			l.Removed = true
			return l
		}(),
		oneTimeLog(t, 12, 5310, "0xa4", 2, bigNet, big.NewInt(1), big.NewInt(2)),
		oneTimeLog(t, 21, 5400, "0xa5", 0, amount("55"), big.NewInt(0), big.NewInt(1)),
	}
	it.chain.head = 5000 + 900 + 600
	it.run()

	require.Equal(t, []incomeEventRow{
		{TxHash: common.HexToHash("0xa1").Hex(), LogIndex: 3, BlockNumber: 5010, RailID: 11, DataSet: 1, Kind: "settle",
			Net: "1000", Commission: "0", Fee: "5", Total: sql.NullString{String: "1005", Valid: true}, SettledUpTo: sql.NullInt64{Int64: 4990, Valid: true}},
		{TxHash: common.HexToHash("0xa4").Hex(), LogIndex: 2, BlockNumber: 5310, RailID: 12, DataSet: 1, Kind: "one_time",
			Net: bigNet.String(), Commission: "1", Fee: "2"},
		{TxHash: common.HexToHash("0xa5").Hex(), LogIndex: 0, BlockNumber: 5400, RailID: 21, DataSet: 2, Kind: "one_time",
			Net: "55", Commission: "0", Fee: "1"},
	}, it.events())

	var total string
	require.NoError(t, it.db.QueryRow(t.Context(), `SELECT SUM(net_amount)::TEXT FROM pdp_income_events WHERE data_set = 1`).Scan(&total))
	require.Equal(t, new(big.Int).Add(bigNet, amount("1000")).String(), total)

	require.Equal(t, int64(5601), it.cursor().Next)
	require.False(t, it.alerts.stalled())
	require.Empty(t, it.alerts.events)
}

func TestIntegration_Income_RereadDoesNotDuplicate(t *testing.T) {
	it := startedIncomeTest(t)
	it.chain.logs = []types.Log{
		settleLog(t, 11, 5010, "0xb1", 0, amount("10"), amount("10"), big.NewInt(0), big.NewInt(0), 5000),
		oneTimeLog(t, 21, 5500, "0xb2", 4, amount("3"), big.NewInt(0), big.NewInt(0)),
	}
	it.chain.head = 6500
	it.run()
	require.Len(t, it.events(), 2)

	it.setCursor(5000)
	it.run()
	require.Len(t, it.events(), 2)
	require.Equal(t, int64(5601), it.cursor().Next)
}

func TestIntegration_Income_ReadsOnlyFinalizedHeights(t *testing.T) {
	it := startedIncomeTest(t)

	cur := it.cursor()
	require.Equal(t, int64(5000), cur.Next, "the first run starts at the chain head")
	require.Equal(t, int64(5000), cur.Started)
	require.Empty(t, it.chain.queries, "nothing below the head is final on the first run")
	require.Equal(t, []int64{5000 - 900, 5000 - 900}, it.chain.dataSetHeights, "rails are read at the finalized height")

	it.chain.head = 7000
	it.run()
	require.NotEmpty(t, it.chain.queries)
	for _, q := range it.chain.queries {
		require.LessOrEqual(t, q[1], int64(7000-900))
		require.LessOrEqual(t, q[1]-q[0]+1, int64(INCOME_LOG_WINDOW))
	}
	require.Equal(t, int64(7000-900+1), it.cursor().Next)
}

func TestIntegration_Income_GapRule(t *testing.T) {
	t.Run("fewer failed runs than a skip needs", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 6800
		it.chain.fail = func(from, to int64) bool { return from < 5600 }
		it.runs(INCOME_GAP_AFTER_RUNS - 1)

		cur := it.cursor()
		require.Equal(t, int64(5000), cur.Next)
		require.Equal(t, INCOME_GAP_AFTER_RUNS-1, cur.FailedRuns)
		require.True(t, cur.StalledSince.Valid)
		require.Empty(t, it.gaps())
		require.Empty(t, it.alerts.events)
	})

	t.Run("old heights and the latest heights both unreadable", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 6800
		it.chain.fail = func(from, to int64) bool { return true }
		it.runs(INCOME_GAP_AFTER_RUNS + 1)

		require.Equal(t, int64(5000), it.cursor().Next)
		require.Empty(t, it.gaps())
		require.Empty(t, it.alerts.events)
	})

	t.Run("skips to the latest finalized heights", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.logs = []types.Log{
			oneTimeLog(t, 11, 5150, "0xc1", 0, amount("2"), big.NewInt(0), big.NewInt(0)),
			oneTimeLog(t, 11, 5700, "0xc2", 0, amount("8"), big.NewInt(0), big.NewInt(0)),
		}
		it.chain.head = 6800 // finalized 5900, latest batch 5601-5900
		// The node keeps history from height 5100 and fails any call starting before it.
		it.chain.fail = func(from, to int64) bool { return from < 5100 }
		it.runs(INCOME_GAP_AFTER_RUNS - 1)
		require.Empty(t, it.gaps())
		it.run()

		require.Equal(t, []incomeGapRow{{From: 5000, To: 5600}}, it.gaps())
		require.Equal(t, []int64{5700}, it.eventHeights(), "income in skipped heights is not recorded, even where the node serves it")
		cur := it.cursor()
		require.Equal(t, int64(5901), cur.Next)
		require.Zero(t, cur.FailedRuns)
		require.False(t, cur.StalledSince.Valid)
		require.Len(t, it.alerts.events, 1)
		require.Equal(t, INCOME_ALERT_SUBSYSTEM, it.alerts.events[0].Subsystem)
		require.Contains(t, it.alerts.events[0].Message, "heights 5000-5600")
		require.False(t, it.alerts.stalled(), "a run that reaches the finalized head resolves the condition")
	})

	t.Run("a height that reads on retry is not skipped", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 6800
		it.chain.fail = func(from, to int64) bool { return from < 5600 }
		it.runs(INCOME_GAP_AFTER_RUNS - 1)
		// The node recovers right after the last failed read of height 5000.
		singles := 0
		it.chain.fail = func(from, to int64) bool {
			if from == 5000 && to == 5000 {
				singles++
				return singles == 1
			}
			return singles == 0 && from < 5600
		}
		it.run()

		require.Empty(t, it.gaps())
		require.Equal(t, int64(5901), it.cursor().Next)
	})

	t.Run("a failure after a skip waits again", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 6800
		it.chain.fail = func(from, to int64) bool { return from < 5300 }
		it.runs(INCOME_GAP_AFTER_RUNS)
		require.Equal(t, []incomeGapRow{{From: 5000, To: 5600}}, it.gaps())

		it.chain.head = 7000 // finalized 6100
		it.chain.fail = func(from, to int64) bool { return from <= 5950 && 5950 <= to }
		it.run()

		require.Equal(t, []incomeGapRow{{From: 5000, To: 5600}}, it.gaps())
		cur := it.cursor()
		require.Equal(t, int64(5950), cur.Next)
		require.Equal(t, 1, cur.FailedRuns)
	})

	t.Run("a run that moves the cursor counts again from there", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 6800
		it.chain.fail = func(from, to int64) bool { return from <= 5000 && 5000 <= to }
		it.runs(INCOME_GAP_AFTER_RUNS - 1)
		it.chain.fail = func(from, to int64) bool { return from <= 5100 && 5100 <= to }
		it.run()

		require.Empty(t, it.gaps())
		cur := it.cursor()
		require.Equal(t, int64(5100), cur.Next)
		require.Equal(t, 1, cur.FailedRuns)
	})

	t.Run("a failed read counts only while the cursor is still there", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 6800
		// Another node moves the cursor past 5000 while this run fails to read it.
		it.chain.fail = func(from, to int64) bool {
			if from == 5000 && to == 5000 {
				it.setCursor(5100)
			}
			return from <= 5000 && 5000 <= to
		}
		it.run()

		cur := it.cursor()
		require.Equal(t, int64(5100), cur.Next)
		require.Zero(t, cur.FailedRuns)
		require.False(t, cur.StalledSince.Valid)
	})

	t.Run("runs that stop before reading do not count", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 6800
		it.chain.fail = func(from, to int64) bool { return from < 5600 }
		it.runs(INCOME_GAP_AFTER_RUNS - 1)
		it.addDataSet(3)
		it.chain.dataSetFails(3)
		it.run()
		require.Equal(t, INCOME_GAP_AFTER_RUNS-1, it.cursor().FailedRuns)
		require.Empty(t, it.gaps())

		it.chain.dataSet(3, 31, 0, incomePayerB)
		it.run()
		require.Equal(t, []incomeGapRow{{From: 5000, To: 5600}}, it.gaps())
	})

	t.Run("nothing to skip within the latest finalized batch", func(t *testing.T) {
		it := startedIncomeTest(t)
		it.chain.head = 5000 + 900 + 100 // finalized 5100
		it.chain.fail = func(from, to int64) bool { return from <= 5000 && 5000 <= to }
		it.runs(INCOME_GAP_AFTER_RUNS - 1)
		// From the retry on, the node returns ranges around height 5000 but not the height alone.
		singles := 0
		it.chain.fail = func(from, to int64) bool {
			if from > 5000 || to < 5000 {
				return false
			}
			if from == 5000 && to == 5000 {
				singles++
				return true
			}
			return singles < 2
		}
		it.run()

		require.Empty(t, it.gaps())
		require.Empty(t, it.alerts.events)
		require.Equal(t, int64(5000), it.cursor().Next)
	})
}

func TestIntegration_Income_SplitsBatchWithTooManyEvents(t *testing.T) {
	it := startedIncomeTest(t)
	it.chain.logs = []types.Log{
		oneTimeLog(t, 21, 5200, "0xe1", 0, amount("5"), big.NewInt(0), big.NewInt(0)),
		oneTimeLog(t, 21, 5250, "0xe2", 0, amount("6"), big.NewInt(0), big.NewInt(0)),
	}
	it.chain.head = 6500
	// More than 150 heights around 5200 exceed the node's result limit.
	it.chain.fail = func(from, to int64) bool { return to-from+1 > 150 && from <= 5200 && to >= 5200 }
	it.run()

	require.Equal(t, []int64{5200, 5250}, it.eventHeights())
	require.Empty(t, it.gaps())
	require.Equal(t, int64(5601), it.cursor().Next)
	require.False(t, it.alerts.stalled())
}

func TestIntegration_Income_RecordsDataSetsFromCreationEvents(t *testing.T) {
	it := startedIncomeTest(t)
	// Data set 3 has no local row, as when it was deleted before a run could read
	// its rails from FWSS. Its creation pays a fee in the same transaction.
	it.chain.logs = []types.Log{
		it.chain.createdLog(it.chain.provider, 3, 31, 32, incomePayerB, 5100, "0xc3", 1),
		oneTimeLog(t, 31, 5100, "0xc3", 3, amount("35"), big.NewInt(0), big.NewInt(1)),
		settleLog(t, 32, 5200, "0xc4", 0, amount("6"), amount("6"), big.NewInt(0), big.NewInt(0), 5150),
	}
	it.chain.head = 6500
	it.run()

	var payer string
	require.NoError(t, it.db.QueryRow(t.Context(), `SELECT payer FROM pdp_data_set_rails WHERE data_set = 3`).Scan(&payer))
	require.Equal(t, incomePayerB.Hex(), payer)
	events := it.events()
	require.Len(t, events, 2)
	for _, ev := range events {
		require.Equal(t, int64(3), ev.DataSet)
	}
	require.Equal(t, int64(5601), it.cursor().Next)
}

func TestIntegration_Income_CreationKnownToLaterReads(t *testing.T) {
	it := startedIncomeTest(t)
	it.chain.logs = []types.Log{
		it.chain.createdLog(it.chain.provider, 3, 31, 0, incomePayerB, 5100, "0xc5", 0),
		oneTimeLog(t, 31, 5250, "0xc6", 0, amount("4"), big.NewInt(0), big.NewInt(0)),
	}
	it.chain.head = 6500
	// The node returns at most 150 heights per call, so each batch is read in halves.
	it.chain.fail = func(from, to int64) bool { return to-from+1 > 150 }
	it.run()

	require.True(t, it.hasRails(3))
	require.Equal(t, []int64{5250}, it.eventHeights())
}

func TestIntegration_Income_IgnoresCreationsOfOtherProviders(t *testing.T) {
	it := startedIncomeTest(t)
	other := common.HexToAddress("0x00000000000000000000000000000000000000d0")
	it.chain.logs = []types.Log{
		// Data set 11 shares its id with the known rail 11.
		it.chain.createdLog(other, 11, 61, 0, incomePayerB, 5050, "0xc7", 0),
		oneTimeLog(t, 61, 5060, "0xc8", 0, amount("3"), big.NewInt(0), big.NewInt(0)),
		oneTimeLog(t, 11, 5070, "0xc9", 0, amount("2"), big.NewInt(0), big.NewInt(0)),
	}
	it.chain.head = 6500
	it.run()

	require.False(t, it.hasRails(11))
	require.Equal(t, []int64{5070}, it.eventHeights())
	require.Equal(t, int64(5601), it.cursor().Next)
}

func TestIntegration_Income_RailsUnknownAtFinalizedHeight(t *testing.T) {
	it := startedIncomeTest(t)
	it.addDataSet(3)
	it.chain.dataSet(3, 0, 0, common.Address{}) // FWSS does not report it at the finalized height yet
	it.chain.head = 6500
	it.run()

	require.False(t, it.hasRails(3))
	require.Equal(t, int64(5601), it.cursor().Next, "a data set FWSS does not report yet does not hold the cursor")
	require.False(t, it.alerts.stalled())

	it.chain.dataSet(3, 31, 0, incomePayerB)
	it.chain.logs = []types.Log{
		oneTimeLog(t, 31, 5700, "0xf1", 0, amount("9"), big.NewInt(0), big.NewInt(0)),
	}
	it.chain.head = 6800
	it.run()

	require.True(t, it.hasRails(3))
	require.Equal(t, int64(6800-900), it.chain.dataSetHeights[len(it.chain.dataSetHeights)-1])
	events := it.events()
	require.Len(t, events, 1)
	require.Equal(t, int64(3), events[0].DataSet)
}

func TestIntegration_Income_WaitsForUnreadRails(t *testing.T) {
	it := startedIncomeTest(t)
	it.addDataSet(3)
	it.chain.dataSetFails(3)
	it.chain.logs = []types.Log{
		oneTimeLog(t, 31, 5100, "0xd1", 0, amount("6"), big.NewInt(0), big.NewInt(0)),
	}
	it.chain.head = 6500
	it.run()

	require.Equal(t, int64(5000), it.cursor().Next)
	require.Empty(t, it.chain.queries)

	it.chain.dataSet(3, 31, 0, incomePayerB)
	it.run()
	events := it.events()
	require.Len(t, events, 1)
	require.Equal(t, int64(3), events[0].DataSet)
	require.Equal(t, int64(5601), it.cursor().Next)
}

func TestIntegration_Income_StalledAlertNeedsConsecutiveRuns(t *testing.T) {
	it := startedIncomeTest(t)
	it.addDataSet(3)
	it.chain.dataSetFails(3)
	it.chain.head = 6500

	it.run()
	require.False(t, it.alerts.stalled(), "one run that stops early does not alert")
	it.run()
	require.True(t, it.alerts.stalled(), "a second consecutive run that stops early alerts")

	it.chain.dataSet(3, 31, 0, incomePayerB)
	it.run()
	require.False(t, it.alerts.stalled(), "a run that reaches the finalized head resolves the alert")

	// Runs in a row count whatever stopped them early.
	it.addDataSet(4)
	it.chain.dataSetFails(4)
	it.run()
	require.False(t, it.alerts.stalled())
	it.chain.headErr = fmt.Errorf("chain node unavailable")
	it.run()
	require.True(t, it.alerts.stalled())
}

func TestIntegration_Income_IgnoresDataSetsOfOtherProviders(t *testing.T) {
	it := startedIncomeTest(t)
	it.addDataSet(3)
	other := common.HexToAddress("0x00000000000000000000000000000000000000d0")
	it.chain.dataSetOf(other, 3, 31, 0, incomePayerB)
	it.chain.logs = []types.Log{
		oneTimeLog(t, 31, 5100, "0xd2", 0, amount("6"), big.NewInt(0), big.NewInt(0)),
		oneTimeLog(t, 11, 5200, "0xd3", 0, amount("7"), big.NewInt(0), big.NewInt(0)),
	}
	it.chain.head = 6500
	it.run()

	require.False(t, it.hasRails(3))
	require.Equal(t, []int64{5200}, it.eventHeights())
	require.Equal(t, int64(5601), it.cursor().Next)
}

func TestIntegration_Income_RecordsOutliveDataSet(t *testing.T) {
	it := startedIncomeTest(t)
	it.chain.logs = []types.Log{
		settleLog(t, 11, 5010, "0xe1", 0, amount("40"), amount("40"), big.NewInt(0), big.NewInt(0), 5000),
	}
	it.chain.head = 6500
	it.run()

	_, err := it.db.Exec(t.Context(), `DELETE FROM pdp_data_sets WHERE id = 1`)
	require.NoError(t, err)

	var payer string
	var net string
	require.NoError(t, it.db.QueryRow(t.Context(), `
		SELECT r.payer, e.net_amount::TEXT
		FROM pdp_income_events e
		JOIN pdp_data_set_rails r ON r.data_set = e.data_set
		WHERE e.data_set = 1
	`).Scan(&payer, &net))
	require.Equal(t, incomePayerA.Hex(), payer)
	require.Equal(t, "40", net)
}
