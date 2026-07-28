package message

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestHarmonyEthTxManagerGetPendingForMachineUsesLatestReplacement(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	machineID := int64(7)
	waitHash := common.HexToHash("0x1111111111111111").Hex()
	firstReplacement := common.HexToHash("0x2222222222222222").Hex()
	latestReplacement := common.HexToHash("0x3333333333333333").Hex()

	_, err = db.Exec(ctx, `
		INSERT INTO harmony_machines (id, host_and_port, cpu, ram, gpu)
		VALUES ($1, 'eth-tx-manager-test', 1, 1, 0)`, machineID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, waiter_machine_id, tx_status)
		VALUES ($1, $2, 'pending')`, waitHash, machineID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_replacements (
			from_address, nonce, original_signed_hash, replaces_signed_hash,
			claim_id, signed_hash, send_time, send_success
		)
		VALUES
			($1, 1, $2, $2, 'claim-1', $3, $5, TRUE),
			($1, 1, $2, $3, 'claim-2', $4, $5, TRUE)`,
		common.HexToAddress("0x1234").Hex(),
		waitHash,
		firstReplacement,
		latestReplacement,
		time.Now().UTC())
	require.NoError(t, err)

	pending, err := NewHarmonyEthTxManager(db).GetPendingForMachine(ctx, machineID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, waitHash, pending[0].WaitHash)
	require.Equal(t, latestReplacement, pending[0].LookupHash)
}
