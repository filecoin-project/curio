//go:build harmony_itest

package pdpv0

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// TestIntegration_RollbackCreateRemovesDataset exercises rollbackCreate against a real Harmony DB (itest).
func TestIntegration_RollbackCreateRemovesDataset(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	svc := fmt.Sprintf("itest-reorg-%d", time.Now().UnixNano())
	pub := make([]byte, 32)
	_, err = rand.Read(pub)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, pub, svc)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM pdp_services WHERE service_label = $1`, svc)
	})

	txh := "0x" + hex.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32})
	fromAddr := "0x" + hex.EncodeToString([]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x02, 0x03, 0x04, 0x05})
	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status, tx_success, confirmed_block_number)
		VALUES ($1, 'confirmed', TRUE, 1)`, txh)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM message_waits_eth WHERE signed_tx_hash = $1`, txh)
	})

	_, err = db.Exec(ctx, `
		INSERT INTO message_sends_eth (from_address, to_address, send_reason, unsigned_tx, unsigned_hash, signed_hash, nonce, send_time, send_success, send_error)
		VALUES ($1, '0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'pdp-mkdataset', '\x00dead', 'unsignedhash', $2, 42424242, NOW(), TRUE, '')`,
		fromAddr, txh)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM message_sends_eth WHERE signed_hash = $1`, txh)
	})

	dsID := int64(999999000 + (time.Now().UnixNano() % 100000))
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready)
		VALUES ($1, $2, $3, 100, 10, FALSE)`, dsID, txh, svc)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM pdp_data_sets WHERE id = $1`, dsID)
	})

	_, err = db.Exec(ctx, `INSERT INTO pdp_data_set_creates (create_message_hash, service, ok, data_set_created)
		VALUES ($1, $2, TRUE, TRUE)`, txh, svc)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM pdp_data_set_creates WHERE create_message_hash = $1`, txh)
	})

	rt := &ReorgCheckTask{db: db}
	sum, err := rt.rollbackCreate(ctx, txh, reasonPDPMkDataset)
	require.NoError(t, err)
	require.Contains(t, sum, "create rollback")

	var cnt int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM pdp_data_sets WHERE id = $1`, dsID).Scan(&cnt)
	require.NoError(t, err)
	require.Equal(t, 0, cnt)

	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM pdp_data_set_creates WHERE create_message_hash = $1`, txh).Scan(&cnt)
	require.NoError(t, err)
	require.Equal(t, 0, cnt)

	var st string
	err = db.QueryRow(ctx, `SELECT tx_status FROM message_waits_eth WHERE signed_tx_hash = $1`, txh).Scan(&st)
	require.NoError(t, err)
	require.Equal(t, "reorged", st)
}
