package message

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestEthMessageReplacementCleanupRetention(t *testing.T) {
	for _, tc := range []struct {
		name               string
		age                time.Duration
		wait               bool
		waitStatus         string
		nullWaitStatus     bool
		executionSucceeded bool
		failedReplacement  bool
		nonceUnconsumed    bool
		retained           bool
	}{
		{name: "old successful replacement without wait", age: 31 * 24 * time.Hour},
		{name: "old pending wait without assigned watcher", age: 31 * 24 * time.Hour, wait: true, waitStatus: "pending", retained: true},
		{name: "old confirmed successful execution", age: 31 * 24 * time.Hour, wait: true, waitStatus: "confirmed", executionSucceeded: true},
		{name: "old confirmed failed execution", age: 31 * 24 * time.Hour, wait: true, waitStatus: "confirmed"},
		{name: "old wait with null status", age: 31 * 24 * time.Hour, wait: true, nullWaitStatus: true, retained: true},
		{name: "old reorged wait", age: 31 * 24 * time.Hour, wait: true, waitStatus: "reorged", retained: true},
		{name: "recent replacement without wait", age: 29 * 24 * time.Hour, retained: true},
		{name: "recent confirmed wait", age: 29 * 24 * time.Hour, wait: true, waitStatus: "confirmed", executionSucceeded: true, retained: true},
		{name: "old replacement nonce not consumed", age: 31 * 24 * time.Hour, nonceUnconsumed: true, retained: true},
		{name: "old failed replacement with pending wait", age: 31 * 24 * time.Hour, wait: true, waitStatus: "pending", failedReplacement: true, retained: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newEthReplaceHarness(t)
			privateKey, err := gethcrypto.GenerateKey()
			require.NoError(t, err)
			from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
			to := common.HexToAddress("0x1000000000000000000000000000000000000009")
			const nonce = uint64(19)
			h.client.nonce = nonce + 1
			if tc.nonceUnconsumed {
				h.client.nonce = nonce
			}
			h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

			original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
			replacement, replacementData := signedDynamicTx(t, privateKey, to, nonce, 300, 30)
			sendTime := h.now.Add(-tc.age)
			h.insertEthMessageSend(t, from, to, original, originalData, sendTime)
			h.insertSuccessfulReplacement(t, from, nonce, original.Hash().Hex(), original.Hash().Hex(), replacement.Hash().Hex(), replacementData, sendTime)
			if tc.failedReplacement {
				_, err = h.db.Exec(h.ctx, `
					UPDATE message_send_eth_replacements
					SET send_success = FALSE, send_error = 'send failed'
					WHERE signed_hash = $1`, replacement.Hash().Hex())
				require.NoError(t, err)
			}
			if tc.wait {
				var status, success any
				if !tc.nullWaitStatus {
					status = tc.waitStatus
				}
				if tc.waitStatus == "confirmed" {
					success = tc.executionSucceeded
				}
				_, err = h.db.Exec(h.ctx, `
					INSERT INTO message_waits_eth (signed_tx_hash, waiter_machine_id, tx_status, tx_success)
					VALUES ($1, NULL, $2, $3)`, original.Hash().Hex(), status, success)
				require.NoError(t, err)
			}

			_, err = h.replacer.loadEthMessageCandidates(h.ctx, h.now.Add(-h.replacer.stuckForDuration), 100)
			require.NoError(t, err)

			var retained bool
			err = h.db.QueryRow(h.ctx, `
				SELECT EXISTS (
					SELECT 1 FROM message_send_eth_replacements WHERE signed_hash = $1
				)`, replacement.Hash().Hex()).Scan(&retained)
			require.NoError(t, err)
			require.Equal(t, tc.retained, retained)
			require.Empty(t, h.client.sentTxs)
		})
	}
}
