package pdpv0

import (
	"context"
	"errors"
	"math/big"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

var (
	chainSyncFWSSAddress = common.HexToAddress("0x00000000000000000000000000000000000c5a11")
	chainSyncViewAddress = common.HexToAddress("0x00000000000000000000000000000000000c5a12")
)

// chainSyncViewClient answers the contract calls made by the chain sync steps
// under test. A call it has no answer for fails the test.
type chainSyncViewClient struct {
	ethchain.EthClient
	t         *testing.T
	responses map[string][]byte
	failures  map[string]error
}

func (c *chainSyncViewClient) CallContract(_ context.Context, msg ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	c.t.Helper()
	if err, ok := c.failures[string(msg.Data)]; ok {
		return nil, err
	}
	response, ok := c.responses[string(msg.Data)]
	require.True(c.t, ok, "unexpected contract call: %x", msg.Data)
	return response, nil
}

func newChainSyncViewClient(t *testing.T) *chainSyncViewClient {
	t.Helper()
	contract.SetAddresses(contract.Addresses{FWSService: chainSyncFWSSAddress})

	c := &chainSyncViewClient{t: t, responses: make(map[string][]byte), failures: make(map[string]error)}
	c.responses[c.callData(contract.ContractWithViewMetaData, "viewContractAddress")] =
		c.pack(contract.ContractWithViewMetaData, "viewContractAddress", chainSyncViewAddress)
	return c
}

func (c *chainSyncViewClient) callData(metadata *bind.MetaData, name string, args ...any) string {
	c.t.Helper()
	parsed, err := metadata.GetAbi()
	require.NoError(c.t, err)
	data, err := parsed.Pack(name, args...)
	require.NoError(c.t, err)
	return string(data)
}

func (c *chainSyncViewClient) pack(metadata *bind.MetaData, name string, value any) []byte {
	c.t.Helper()
	parsed, err := metadata.GetAbi()
	require.NoError(c.t, err)
	response, err := parsed.Methods[name].Outputs.Pack(value)
	require.NoError(c.t, err)
	return response
}

func (c *chainSyncViewClient) getDataSetCall(dataSetID int64) string {
	return c.callData(FWSS.FilecoinWarmStorageServiceStateViewMetaData, "getDataSet", big.NewInt(dataSetID))
}

// setDataSet makes FWSS report the data set with the given PDP rail and
// service end epoch; an end epoch of 0 is a live service.
func (c *chainSyncViewClient) setDataSet(dataSetID, railID, endEpoch int64) {
	c.responses[c.getDataSetCall(dataSetID)] = c.pack(FWSS.FilecoinWarmStorageServiceStateViewMetaData, "getDataSet",
		FWSS.FilecoinWarmStorageServiceDataSetInfoView{
			PdpRailId:       big.NewInt(railID),
			CacheMissRailId: big.NewInt(0),
			CdnRailId:       big.NewInt(0),
			CommissionBps:   big.NewInt(0),
			ClientDataSetId: big.NewInt(0),
			PdpEndEpoch:     big.NewInt(endEpoch),
			ProviderId:      big.NewInt(0),
			DataSetId:       big.NewInt(dataSetID),
		})
}

// setRemoved makes PDPVerifier report the data set as removed.
func (c *chainSyncViewClient) setRemoved(dataSetID int64) {
	c.failures[c.callData(contract.PDPVerifierMetaData, "dataSetLive", big.NewInt(dataSetID))] =
		selectorRevert(contractErrorSelector(ErrPDPVerifierDataSetNotFound))
}

// setCleanupMode makes PDPVerifier report the data set as deleted with pieces
// left to clean up.
func (c *chainSyncViewClient) setCleanupMode(dataSetID int64) {
	id := big.NewInt(dataSetID)
	c.responses[c.callData(contract.PDPVerifierMetaData, "dataSetLive", id)] =
		c.pack(contract.PDPVerifierMetaData, "dataSetLive", false)
	c.responses[c.callData(contract.PDPVerifierMetaData, "getNextPieceId", id)] =
		c.pack(contract.PDPVerifierMetaData, "getNextPieceId", big.NewInt(1))
	c.responses[c.callData(contract.PDPVerifierMetaData, "getNextChallengeEpoch", id)] =
		c.pack(contract.PDPVerifierMetaData, "getNextChallengeEpoch", cleanupModeSentinel)
}

type failingAlerting struct {
	recordingAlerting
}

func (failingAlerting) EmitEvent(context.Context, curioalerting.AlertEvent) error {
	return errors.New("alert store unavailable")
}

func TestIntegration_SyncServiceTerminations(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	// The unreadable data set has the lowest id, so it is read first.
	unreadable := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	terminated := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	live := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	requested := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	provingStopped := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	deleteSent := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	deleteQueued := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	deleteFailed := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})

	_, err = db.Exec(ctx, `INSERT INTO pdp_delete_data_set (id, operator_requested_deletion) VALUES ($1, TRUE)`, requested)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO pdp_data_set_review (data_set) VALUES ($1)`, provingStopped)
	require.NoError(t, err)

	// Rows nobody requested: two are in the deleteDataSet stage, the third is not.
	var deleteTaskID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO harmony_task (posted_time, added_by, name)
		VALUES (NOW(), 1, 'test')
		RETURNING id
	`).Scan(&deleteTaskID))
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_delete_data_set (id, after_delete_data_set, delete_data_set_task_id)
		VALUES ($1, TRUE, NULL), ($2, FALSE, $4), ($3, FALSE, NULL)
	`, deleteSent, deleteQueued, deleteFailed, deleteTaskID)
	require.NoError(t, err)

	// No answer is registered for a data set with a deletion in progress:
	// reading one fails the test.
	client := newChainSyncViewClient(t)
	client.failures[client.getDataSetCall(unreadable)] = errors.New("rpc unavailable")
	client.setDataSet(terminated, 7, 5000)
	client.setDataSet(live, 8, 0)
	client.setDataSet(provingStopped, 10, 5200)
	client.setDataSet(deleteFailed, 11, 5300)

	alerts := &recordingAlerting{}
	task := &TaskChainSync{db: db, ethClient: client, al: alerts}

	// The second pass must not alert again.
	for range 2 {
		require.NoError(t, task.syncServiceTerminations(ctx))
	}

	endEpoch := func(epoch int64) []dataSetReview { return []dataSetReview{{ServiceEndEpoch: &epoch}} }
	require.Empty(t, loadDataSetReviews(t, ctx, db, unreadable))
	require.Equal(t, endEpoch(5000), loadDataSetReviews(t, ctx, db, terminated))
	require.Empty(t, loadDataSetReviews(t, ctx, db, live))
	require.Empty(t, loadDataSetReviews(t, ctx, db, requested))
	require.Equal(t, endEpoch(5200), loadDataSetReviews(t, ctx, db, provingStopped))
	require.Empty(t, loadDataSetReviews(t, ctx, db, deleteSent))
	require.Empty(t, loadDataSetReviews(t, ctx, db, deleteQueued))
	require.Equal(t, endEpoch(5300), loadDataSetReviews(t, ctx, db, deleteFailed))

	type alerted struct {
		dataSet, rail string
	}
	var got []alerted
	for _, event := range alerts.events {
		require.Equal(t, alertType, event.System)
		require.Equal(t, alertNameChainSync, event.Subsystem)
		got = append(got, alerted{
			dataSet: event.Context[curioalerting.CONTEXT_KEY_DATASET],
			rail:    event.Context[curioalerting.CONTEXT_KEY_RAIL],
		})
	}
	require.Equal(t, []alerted{
		{dataSet: strconv.FormatInt(terminated, 10), rail: "7"},
		{dataSet: strconv.FormatInt(provingStopped, 10), rail: "10"},
		{dataSet: strconv.FormatInt(deleteFailed, 10), rail: "11"},
	}, got)

	// A deletion that starts after the data set was selected still keeps it out.
	for _, dataSetID := range []int64{requested, deleteSent, deleteQueued} {
		alert, err := recordServiceTermination(ctx, db, dataSetID, 5100)
		require.NoError(t, err)
		require.False(t, alert)
		require.Empty(t, loadDataSetReviews(t, ctx, db, dataSetID))
	}
}

func TestIntegration_SyncServiceTerminations_RecordsWhenAlertFails(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	dataSetID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	client := newChainSyncViewClient(t)
	client.setDataSet(dataSetID, 7, 5000)

	task := &TaskChainSync{db: db, ethClient: client, al: &failingAlerting{}}
	require.NoError(t, task.syncServiceTerminations(ctx))

	endEpoch := int64(5000)
	require.Equal(t, []dataSetReview{{ServiceEndEpoch: &endEpoch}}, loadDataSetReviews(t, ctx, db, dataSetID))
}

// loadKeptDataSets maps each data set that still has its local row to whether
// its pipeline row is past deleteDataSet.
func loadKeptDataSets(t *testing.T, ctx context.Context, db *harmonydb.DB) map[int64]bool {
	t.Helper()

	var rows []struct {
		ID          int64 `db:"id"`
		AfterDelete bool  `db:"after_delete"`
	}
	require.NoError(t, db.Select(ctx, &rows, `
		SELECT ds.id, COALESCE(dd.after_delete_data_set, FALSE) AS after_delete
		FROM pdp_data_sets ds
		LEFT JOIN pdp_delete_data_set dd ON dd.id = ds.id
	`))
	kept := make(map[int64]bool, len(rows))
	for _, row := range rows {
		kept[row.ID] = row.AfterDelete
	}
	return kept
}

func TestIntegration_SyncPDPVerifierDeletionState_OnlyDeletionsInProgress(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	newDataSet := func() int64 {
		return insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	}
	undecided := newDataSet()
	undecidedCleanup := newDataSet()
	deleteSent := newDataSet()
	deleteQueued := newDataSet()
	operatorRequested := newDataSet()
	clientRequested := newDataSet()

	var deleteTaskID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO harmony_task (posted_time, added_by, name)
		VALUES (NOW(), 1, 'test')
		RETURNING id
	`).Scan(&deleteTaskID))

	// Every row has its service terminated and its rail final. Nobody asked
	// for the first four; the third and fourth are in the deleteDataSet stage.
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_delete_data_set (
			id, after_terminate_service, service_termination_epoch, deletion_allowed,
			after_delete_data_set, delete_data_set_task_id,
			operator_requested_deletion, client_requested_termination, termination_extra_data
		)
		VALUES
			($1, TRUE, 5300, TRUE, FALSE, NULL, FALSE, FALSE, NULL),
			($2, TRUE, 5300, TRUE, FALSE, NULL, FALSE, FALSE, NULL),
			($3, TRUE, 5300, TRUE, TRUE, NULL, FALSE, FALSE, NULL),
			($4, TRUE, 5300, TRUE, FALSE, $7, FALSE, FALSE, NULL),
			($5, TRUE, 5300, TRUE, FALSE, NULL, TRUE, FALSE, NULL),
			($6, TRUE, 5300, TRUE, FALSE, NULL, FALSE, TRUE, '\x01')
	`, undecided, undecidedCleanup, deleteSent, deleteQueued, operatorRequested, clientRequested, deleteTaskID)
	require.NoError(t, err)

	// On chain every data set is gone, except undecidedCleanup, which still
	// has pieces to clean up.
	client := newChainSyncViewClient(t)
	for _, dataSetID := range []int64{undecided, deleteSent, deleteQueued, operatorRequested, clientRequested} {
		client.setRemoved(dataSetID)
	}
	client.setCleanupMode(undecidedCleanup)

	task := &TaskChainSync{db: db, ethClient: client}
	require.NoError(t, task.syncPDPVerifierDeletionState(ctx))

	// Only the rows awaiting the operator keep their local data, untouched.
	require.Equal(t, map[int64]bool{
		undecided:        false,
		undecidedCleanup: false,
	}, loadKeptDataSets(t, ctx, db))

	// Once the operator asks, they follow the chain like the others.
	_, err = db.Exec(ctx, `
		UPDATE pdp_delete_data_set
		SET operator_requested_deletion = TRUE
		WHERE id IN ($1, $2)
	`, undecided, undecidedCleanup)
	require.NoError(t, err)
	require.NoError(t, task.syncPDPVerifierDeletionState(ctx))

	require.Equal(t, map[int64]bool{undecidedCleanup: true}, loadKeptDataSets(t, ctx, db))
}
