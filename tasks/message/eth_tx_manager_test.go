package message

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestHarmonyEthTxManagerGetPendingForMachineReplacementHashes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		replacements bool
		extraRows    bool
	}{
		{name: "original only"},
		{name: "all replacements newest first", replacements: true},
		{name: "excludes failed unknown unsigned and unrelated rows", replacements: true, extraRows: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)

			machineID := int64(7)
			waitHash := common.HexToHash("0x11").Hex()
			first := common.HexToHash("0x22").Hex()
			latest := common.HexToHash("0x33").Hex()
			from := common.HexToAddress("0x1234").Hex()
			_, err = db.Exec(ctx, `
				INSERT INTO harmony_machines (id, host_and_port, cpu, ram, gpu)
				VALUES ($1, 'eth-tx-manager-test', 1, 1, 0)`, machineID)
			require.NoError(t, err)
			_, err = db.Exec(ctx, `
				INSERT INTO message_waits_eth (signed_tx_hash, waiter_machine_id, tx_status)
				VALUES ($1, $2, 'pending')`, waitHash, machineID)
			require.NoError(t, err)

			expected := []string{waitHash}
			if tc.replacements {
				_, err = db.Exec(ctx, `
					INSERT INTO message_send_eth_replacements (
						from_address, nonce, original_signed_hash, replaces_signed_hash,
						claim_id, signed_hash, send_time, send_success
					) VALUES
						($1, 1, $2, $3, 'latest', $4, $5, TRUE),
						($1, 1, $2, $2, 'first', $3, $6, TRUE)`,
					from, waitHash, first, latest, time.Now().UTC(), time.Now().UTC().Add(-time.Minute))
				require.NoError(t, err)
				expected = []string{latest, first, waitHash}
			}
			if tc.extraRows {
				_, err = db.Exec(ctx, `
					INSERT INTO message_send_eth_replacements (
						from_address, nonce, original_signed_hash, replaces_signed_hash,
						claim_id, signed_hash, send_success
					) VALUES
						($1, 1, $2, $3, 'failed', $4, FALSE),
						($1, 1, $2, $3, 'unknown', $5, NULL),
						($1, 1, $2, $5, 'unsigned', NULL, TRUE),
						($1, 2, $6, $6, 'unrelated', $7, TRUE)`,
					from, waitHash, latest, common.HexToHash("0x44").Hex(), common.HexToHash("0x55").Hex(),
					common.HexToHash("0x66").Hex(), common.HexToHash("0x77").Hex())
				require.NoError(t, err)
			}

			pending, err := NewHarmonyEthTxManager(db).GetPendingForMachine(ctx, machineID)
			require.NoError(t, err)
			require.Equal(t, []PendingEthTx{{WaitHash: waitHash, LookupHashes: expected}}, pending)
		})
	}
}
