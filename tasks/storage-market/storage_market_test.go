package storage_market

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/types"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
)

type expiryTestMarketAPI struct {
	storageMarketAPI
	head *types.TipSet
}

func (e expiryTestMarketAPI) ChainHead(context.Context) (*types.TipSet, error) {
	return e.head, nil
}

type expiryTestIngester struct {
	expectedSealDuration abi.ChainEpoch
	allocateCalls        int
}

func (e *expiryTestIngester) AllocatePieceToSector(context.Context, *harmonydb.Tx, address.Address, lpiece.PieceDealInfo, int64, url.URL, http.Header) (*abi.SectorNumber, *abi.RegisteredSealProof, error) {
	e.allocateCalls++
	return nil, nil, fmt.Errorf("unexpected sector allocation")
}

func (e *expiryTestIngester) GetExpectedSealDuration() abi.ChainEpoch {
	return e.expectedSealDuration
}

func TestIntegration_CheckExpiry(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	head := abi.ChainEpoch(100)
	sealDuration := abi.ChainEpoch(20)
	api := expiryTestMarketAPI{head: expiryTestTipSet(t, head)}

	tests := []struct {
		name        string
		direct      bool
		start       abi.ChainEpoch
		wantExpired bool
	}{
		{name: "MK1.2 expired", start: head + sealDuration - 1, wantExpired: true},
		{name: "MK1.2 boundary", start: head + sealDuration},
		{name: "direct expired", direct: true, start: head + sealDuration - 1, wantExpired: true},
		{name: "direct boundary", direct: true, start: head + sealDuration},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := fmt.Sprintf("expiry-test-%d", i)
			insertExpiryTestDeal(t, ctx, db, id, int64(1000+i), test.start, test.direct)

			expired, err := checkExpiry(ctx, db, api, id, sealDuration)
			require.NoError(t, err)
			require.Equal(t, test.wantExpired, expired)

			var dealError sql.NullString
			if test.direct {
				err = db.QueryRow(ctx, `SELECT error FROM market_direct_deals WHERE uuid = $1`, id).Scan(&dealError)
			} else {
				err = db.QueryRow(ctx, `SELECT error FROM market_mk12_deals WHERE uuid = $1`, id).Scan(&dealError)
			}
			require.NoError(t, err)

			var pipelineRows int
			err = db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_deal_pipeline WHERE uuid = $1`, id).Scan(&pipelineRows)
			require.NoError(t, err)

			if test.wantExpired {
				require.True(t, dealError.Valid)
				require.Contains(t, dealError.String, fmt.Sprintf("start epoch %d", test.start))
				require.Zero(t, pipelineRows)
			} else {
				require.False(t, dealError.Valid)
				require.Equal(t, 1, pipelineRows)
			}
		})
	}

	t.Run("final assignment rechecks expiry", func(t *testing.T) {
		id := "expiry-test-final-assignment"
		insertExpiryTestDeal(t, ctx, db, id, 2000, head+sealDuration-1, false)
		ingester := &expiryTestIngester{expectedSealDuration: sealDuration}
		market := &CurioStorageDealMarket{db: db, api: api, pin: ingester}

		err := market.processMk12Deal(ctx, MK12Pipeline{
			UUID:          id,
			Started:       true,
			AfterCommp:    true,
			AfterPSD:      true,
			AfterFindDeal: true,
		})
		require.NoError(t, err)
		require.Zero(t, ingester.allocateCalls)

		var dealError string
		err = db.QueryRow(ctx, `SELECT error FROM market_mk12_deals WHERE uuid = $1`, id).Scan(&dealError)
		require.NoError(t, err)
		require.Contains(t, dealError, fmt.Sprintf("start epoch %d", head+sealDuration-1))

		var pipelineRows int
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_deal_pipeline WHERE uuid = $1`, id).Scan(&pipelineRows)
		require.NoError(t, err)
		require.Zero(t, pipelineRows)
	})
}

func insertExpiryTestDeal(t *testing.T, ctx context.Context, db *harmonydb.DB, id string, spID int64, start abi.ChainEpoch, direct bool) {
	t.Helper()

	if direct {
		_, err := db.Exec(ctx, `INSERT INTO market_direct_deals (
			uuid, sp_id, client, offline, verified, start_epoch, end_epoch,
			allocation_id, piece_cid, piece_size, fast_retrieval, announce_to_ipni
		) VALUES ($1, $2, $3, FALSE, TRUE, $4, $5, $6, $7, 128, FALSE, FALSE)`,
			id, spID, "client", start, start+1000, spID, "piece-"+id)
		require.NoError(t, err)
	} else {
		_, err := db.Exec(ctx, `INSERT INTO market_mk12_deals (
			uuid, sp_id, signed_proposal_cid, proposal_signature, proposal, offline,
			verified, start_epoch, end_epoch, client_peer_id, piece_cid, piece_size,
			fast_retrieval, announce_to_ipni, proposal_cid
		) VALUES ($1, $2, $3, $4, $5, FALSE, TRUE, $6, $7, $8, $9, 128, FALSE, FALSE, $10)`,
			id, spID, "signed-"+id, []byte{1}, `{}`, start, start+1000, "peer-"+id, "piece-"+id, "proposal-"+id)
		require.NoError(t, err)
	}

	_, err := db.Exec(ctx, `INSERT INTO market_mk12_deal_pipeline (
		uuid, sp_id, piece_cid, piece_size, offline, started, after_commp,
		after_psd, after_find_deal, is_ddo
	) VALUES ($1, $2, $3, 128, FALSE, TRUE, TRUE, TRUE, TRUE, $4)`,
		id, spID, "piece-"+id, direct)
	require.NoError(t, err)
}

func expiryTestTipSet(t *testing.T, height abi.ChainEpoch) *types.TipSet {
	t.Helper()

	miner, err := address.NewIDAddress(1)
	require.NoError(t, err)
	root, err := cid.Decode("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	require.NoError(t, err)

	head, err := types.NewTipSet([]*types.BlockHeader{{
		Miner:                 miner,
		Ticket:                &types.Ticket{VRFProof: []byte{byte(height)}},
		Height:                height,
		ParentStateRoot:       root,
		Messages:              root,
		ParentMessageReceipts: root,
		BlockSig:              &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate:          &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		Timestamp:             uint64(time.Now().Unix()),
		ParentBaseFee:         types.NewInt(100),
	}})
	require.NoError(t, err)
	return head
}
