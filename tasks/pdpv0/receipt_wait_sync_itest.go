//go:build harmony_itest

package pdpv0

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
)

func TestIntegration_CheckCreateLandedViaClientNonces_UnknownWithoutIdentity(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	wait := OutstandingReceipt{TxHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", HasCreate: true}
	created, known, err := checkCreateLandedViaClientNonces(ctx, db, nil, wait, nil)
	require.NoError(t, err)
	require.False(t, created)
	require.False(t, known)
}

func TestIntegration_ResolveCreateIdentityFromPersistedExtraData(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	svc, txHash, cleanup := insertReceiptTestService(t, ctx, db)
	defer cleanup()

	payer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	extraData := packBareCreateExtraData(t, payer, 77)

	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')
	`, txHash)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_set_creates (create_message_hash, service, extra_data)
		VALUES ($1, $2, $3)
	`, txHash, svc, extraData)
	require.NoError(t, err)

	wait := OutstandingReceipt{TxHash: txHash, HasCreate: true, Service: svc}
	identity, err := resolveCreateIdentity(ctx, db, &wait, nil)
	require.NoError(t, err)
	require.Equal(t, payer, identity.Payer)
	require.Equal(t, int64(77), identity.ClientDataSetId.Int64())
}

func TestIntegration_ResolveCreateIdentityFromSignedTx(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	svc, txHash, cleanup := insertReceiptTestService(t, ctx, db)
	defer cleanup()

	payer := common.HexToAddress("0x2222222222222222222222222222222222222222")
	createPayload := packBareCreateExtraData(t, payer, 9)
	listener := common.HexToAddress("0x3333333333333333333333333333333333333333")

	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	require.NoError(t, err)
	calldata, err := pdpABI.Pack("createDataSet", listener, createPayload)
	require.NoError(t, err)

	signedTx := types.NewTransaction(1, contract.ContractAddresses().PDPVerifier, big.NewInt(0), 100000, big.NewInt(1), calldata)
	signedBytes, err := signedTx.MarshalBinary()
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')
	`, txHash)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_set_creates (create_message_hash, service)
		VALUES ($1, $2)
	`, txHash, svc)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO message_sends_eth (
			from_address, to_address, send_reason, unsigned_tx, unsigned_hash,
			signed_hash, signed_tx, nonce, send_time, send_success
		) VALUES (
			'0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			'0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			'pdp-mkdataset', $1, 'unsigned', $2, $3, 1, NOW(), TRUE
		)
	`, []byte{0x01}, txHash, signedBytes)
	require.NoError(t, err)

	wait := OutstandingReceipt{TxHash: txHash, HasCreate: true, Service: svc}
	identity, err := resolveCreateIdentity(ctx, db, &wait, nil)
	require.NoError(t, err)
	require.Equal(t, payer, identity.Payer)
	require.Equal(t, int64(9), identity.ClientDataSetId.Int64())
}

func TestIntegration_MaterializeCreateOnlyConfirmsWaitAndCreate(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	svc, txHash, cleanup := insertReceiptTestService(t, ctx, db)
	defer cleanup()

	dsID := int64(910_000_000 + time.Now().UnixNano()%100000)
	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')
	`, txHash)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_set_creates (create_message_hash, service, data_set_created, ok)
		VALUES ($1, $2, FALSE, NULL)
	`, txHash, svc)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window)
		VALUES ($1, $2, $3, 100, 10)
	`, dsID, txHash, svc)
	require.NoError(t, err)

	wait := OutstandingReceipt{
		TxHash:    txHash,
		HasCreate: true,
		DataSet:   sql.NullInt64{Int64: dsID, Valid: true},
		Service:   svc,
	}
	require.NoError(t, materializeLandedReceipt(ctx, db, wait, nil))

	var status string
	var success sql.NullBool
	err = db.QueryRow(ctx, `
		SELECT tx_status, tx_success FROM message_waits_eth WHERE signed_tx_hash = $1
	`, txHash).Scan(&status, &success)
	require.NoError(t, err)
	require.Equal(t, "confirmed", status)
	require.True(t, success.Valid && success.Bool)

	var created, ok bool
	err = db.QueryRow(ctx, `
		SELECT data_set_created, ok FROM pdp_data_set_creates WHERE create_message_hash = $1
	`, txHash).Scan(&created, &ok)
	require.NoError(t, err)
	require.True(t, created)
	require.True(t, ok)
}

func TestIntegration_MarkReceiptLostAndDeferredClassify(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	_, txHash, cleanup := insertReceiptTestService(t, ctx, db)
	defer cleanup()

	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')
	`, txHash)
	require.NoError(t, err)

	wait := OutstandingReceipt{TxHash: txHash, HasCreate: true}
	outcome, signedTx, err := classifyMissingReceipt(ctx, db, nil, wait)
	require.NoError(t, err)
	require.Equal(t, receiptDeferred, outcome)
	require.Nil(t, signedTx)

	require.NoError(t, markReceiptLost(ctx, db, wait))
	var status string
	err = db.QueryRow(ctx, `SELECT tx_status FROM message_waits_eth WHERE signed_tx_hash = $1`, txHash).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, "failed", status)
}

func TestIntegration_DoNotMarkLostWhenCreateIdentityUnknown(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	svc, txHash, cleanup := insertReceiptTestService(t, ctx, db)
	defer cleanup()

	fromAddr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')
	`, txHash)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_set_creates (create_message_hash, service)
		VALUES ($1, $2)
	`, txHash, svc)
	require.NoError(t, err)

	// Signed tx without PDP create calldata — identity decode fails → known=false.
	signedTx := types.NewTransaction(5, common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		big.NewInt(0), 21000, big.NewInt(1), []byte{0xde, 0xad})
	signedBytes, err := signedTx.MarshalBinary()
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO message_sends_eth (
			from_address, to_address, send_reason, unsigned_tx, unsigned_hash,
			signed_hash, signed_tx, nonce, send_time, send_success
		) VALUES ($1, $2, 'pdp-mkdataset', $3, 'unsigned', $4, $5, 5, NOW() - INTERVAL '2 days', TRUE)
	`, fromAddr, "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", []byte{0x01}, txHash, signedBytes)
	require.NoError(t, err)

	eth := &receiptTestEth{pendingNonce: 10} // nonce advanced past 5
	wait := OutstandingReceipt{TxHash: txHash, HasCreate: true, Service: svc}

	outcome, stx, err := classifyMissingReceipt(ctx, db, eth, wait)
	require.NoError(t, err)
	require.Equal(t, receiptLost, outcome)

	created, known, err := checkCreateLandedViaClientNonces(ctx, db, eth, wait, stx)
	require.NoError(t, err)
	require.False(t, created)
	require.False(t, known)

	var status string
	err = db.QueryRow(ctx, `SELECT tx_status FROM message_waits_eth WHERE signed_tx_hash = $1`, txHash).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, "pending", status)
}

type receiptTestEth struct {
	ethchain.EthClient
	pendingNonce uint64
}

func (e *receiptTestEth) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return e.pendingNonce, nil
}

func insertReceiptTestService(t *testing.T, ctx context.Context, db *harmonydb.DB) (service, txHash string, cleanup func()) {
	t.Helper()
	svc := fmt.Sprintf("receipt-sync-%d", time.Now().UnixNano())
	pub := make([]byte, 32)
	_, err := rand.Read(pub)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, pub, svc)
	require.NoError(t, err)

	raw := make([]byte, 32)
	_, err = rand.Read(raw)
	require.NoError(t, err)
	txHash = "0x" + hex.EncodeToString(raw)

	cleanup = func() {
		_, _ = db.Exec(ctx, `DELETE FROM pdp_data_set_piece_adds WHERE LOWER(TRIM(BOTH FROM add_message_hash)) = $1`, txHash)
		_, _ = db.Exec(ctx, `DELETE FROM pdp_data_set_pieces WHERE LOWER(TRIM(BOTH FROM add_message_hash)) = $1`, txHash)
		_, _ = db.Exec(ctx, `DELETE FROM pdp_data_sets WHERE LOWER(TRIM(BOTH FROM create_message_hash)) = $1`, txHash)
		_, _ = db.Exec(ctx, `DELETE FROM pdp_data_set_creates WHERE LOWER(TRIM(BOTH FROM create_message_hash)) = $1`, txHash)
		_, _ = db.Exec(ctx, `DELETE FROM message_sends_eth WHERE LOWER(TRIM(BOTH FROM signed_hash)) = $1`, txHash)
		_, _ = db.Exec(ctx, `DELETE FROM message_waits_eth WHERE signed_tx_hash = $1`, txHash)
		_, _ = db.Exec(ctx, `DELETE FROM pdp_services WHERE service_label = $1`, svc)
	}
	return svc, txHash, cleanup
}

func packBareCreateExtraData(t *testing.T, payer common.Address, clientDataSetId int64) []byte {
	t.Helper()
	bytesType, err := abi.NewType("bytes", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	uint256Type, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	stringArrayType, err := abi.NewType("string[]", "", nil)
	require.NoError(t, err)
	args := abi.Arguments{
		{Type: addressType},
		{Type: uint256Type},
		{Type: stringArrayType},
		{Type: stringArrayType},
		{Type: bytesType},
	}
	payload, err := args.Pack(payer, big.NewInt(clientDataSetId), []string{}, []string{}, []byte("sig"))
	require.NoError(t, err)
	return payload
}
