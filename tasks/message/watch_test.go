package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/api"
	ltypes "github.com/filecoin-project/lotus/chain/types"
)

func TestMessageWatcherConfirmsOriginalWaitAfterReplacementCleanup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		winner int
	}{
		{name: "original", winner: 0},
		{name: "first replacement", winner: 1},
		{name: "latest replacement", winner: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newFilecoinReplaceHarness(t)
			from := testIDAddress(t, 1008)
			to := testIDAddress(t, 2008)
			nonce := uint64(14)
			h.registerSender(t, from, nonce)

			original := signedMessageWithByte(testMessage(from, to, nonce, 100, 10), 1)
			h.insertMessageSend(t, original, h.oldSendTime(), 1)
			_, err := h.db.Exec(h.ctx, `
				INSERT INTO message_waits (signed_message_cid)
				VALUES ($1)`, original.Cid().String())
			require.NoError(t, err)

			for i := range 2 {
				h.expectGasEstimate(t, func(msg *ltypes.Message) *ltypes.Message {
					estimated := *msg
					estimated.GasLimit = 1000
					estimated.GasFeeCap = abi.NewTokenAmount(int64(200 * (i + 1)))
					estimated.GasPremium = abi.NewTokenAmount(int64(20 * (i + 1)))
					return &estimated
				})
				h.expectMpoolPush(t, nil)
				h.run(t)
				require.Len(t, h.pushedMsgs, i+1)
				require.True(t, h.pushedMsgs[i].Message.EqualCall(&original.Message))
				require.NotEqual(t, original.Cid(), h.pushedMsgs[i].Cid())
				_, err = h.db.Exec(h.ctx, `
					UPDATE message_send_replacements
					SET send_time = $1
					WHERE signed_cid = $2`, h.oldSendTime(), h.pushedMsgs[i].Cid().String())
				require.NoError(t, err)
			}
			require.Equal(t, 2, messageReplacementRowCount(t, h.db, h.ctx))
			require.NotEqual(t, h.pushedMsgs[0].Cid(), h.pushedMsgs[1].Cid())
			messages := []*ltypes.SignedMessage{original, h.pushedMsgs[0], h.pushedMsgs[1]}
			winner := messages[tc.winner]

			h.actors[from.String()].Nonce = nonce + 1
			h.run(t)
			require.Zero(t, messageReplacementRowCount(t, h.db, h.ctx))

			var machineID int64
			err = h.db.QueryRow(h.ctx, `
				SELECT id FROM harmony_machines WHERE host_and_port = 'replace-test'`).Scan(&machineID)
			require.NoError(t, err)
			client := &filecoinWatchTestAPI{
				t:          t,
				head:       makeMockTipSet(t, 100),
				confidence: makeMockTipSet(t, 100-MinConfidence),
				sender:     from,
				actorNonce: nonce,
				original:   original.Cid(),
				message:    &winner.Message,
			}
			watcher := &MessageWatcher{
				db:  h.db,
				ht:  &mockTaskEngine{machineID: machineID},
				api: client,
			}
			headKey := client.head.Key()
			watcher.bestTs.Store(&headKey)
			watcher.update()

			var executedCID sql.NullString
			err = h.db.QueryRow(h.ctx, `
				SELECT executed_msg_cid FROM message_waits WHERE signed_message_cid = $1`,
				original.Cid().String()).Scan(&executedCID)
			require.NoError(t, err)
			require.False(t, executedCID.Valid)
			require.Equal(t, 1, client.searchCalls)
			require.Zero(t, client.messageCalls)

			client.confidence = client.head
			client.head = makeMockTipSet(t, 100+MinConfidence)
			client.actorNonce = nonce + 1
			client.lookup = &api.MsgLookup{
				Message: winner.Cid(),
				Receipt: ltypes.MessageReceipt{Return: []byte{1, 2, 3}, GasUsed: 123},
				TipSet:  client.confidence.Key(),
				Height:  client.confidence.Height(),
			}
			headKey = client.head.Key()
			watcher.bestTs.Store(&headKey)
			watcher.update()

			var waits []struct {
				SignedMessageCID string        `db:"signed_message_cid"`
				WaiterMachineID  sql.NullInt64 `db:"waiter_machine_id"`
				ExecutedTskCID   string        `db:"executed_tsk_cid"`
				ExecutedTskEpoch int64         `db:"executed_tsk_epoch"`
				ExecutedMsgCID   string        `db:"executed_msg_cid"`
				ExecutedMsgData  []byte        `db:"executed_msg_data"`
				ExitCode         int64         `db:"executed_rcpt_exitcode"`
				Return           []byte        `db:"executed_rcpt_return"`
				GasUsed          int64         `db:"executed_rcpt_gas_used"`
			}
			err = h.db.Select(h.ctx, &waits, `
				SELECT signed_message_cid, waiter_machine_id, executed_tsk_cid, executed_tsk_epoch,
					executed_msg_cid, executed_msg_data, executed_rcpt_exitcode, executed_rcpt_return, executed_rcpt_gas_used
				FROM message_waits`)
			require.NoError(t, err)
			require.Len(t, waits, 1)
			require.Equal(t, original.Cid().String(), waits[0].SignedMessageCID)
			require.False(t, waits[0].WaiterMachineID.Valid)
			executedTskCID, err := client.lookup.TipSet.Cid()
			require.NoError(t, err)
			require.Equal(t, executedTskCID.String(), waits[0].ExecutedTskCID)
			require.Equal(t, int64(100), waits[0].ExecutedTskEpoch)
			require.Equal(t, winner.Cid().String(), waits[0].ExecutedMsgCID)
			expectedData, err := json.Marshal(&winner.Message)
			require.NoError(t, err)
			require.JSONEq(t, string(expectedData), string(waits[0].ExecutedMsgData))
			require.Zero(t, waits[0].ExitCode)
			require.Equal(t, []byte{1, 2, 3}, waits[0].Return)
			require.Equal(t, int64(123), waits[0].GasUsed)
			require.Equal(t, 2, client.searchCalls)
			require.Equal(t, 1, client.messageCalls)
			require.Zero(t, messageReplacementRowCount(t, h.db, h.ctx))
		})
	}
}

type filecoinWatchTestAPI struct {
	t            *testing.T
	head         *ltypes.TipSet
	confidence   *ltypes.TipSet
	sender       address.Address
	actorNonce   uint64
	original     cid.Cid
	lookup       *api.MsgLookup
	message      *ltypes.Message
	searchCalls  int
	messageCalls int
}

func (m *filecoinWatchTestAPI) ChainGetTipSet(_ context.Context, key ltypes.TipSetKey) (*ltypes.TipSet, error) {
	require.Equal(m.t, m.head.Key(), key)
	return m.head, nil
}

func (m *filecoinWatchTestAPI) ChainGetTipSetByHeight(_ context.Context, height abi.ChainEpoch, key ltypes.TipSetKey) (*ltypes.TipSet, error) {
	require.Equal(m.t, m.head.Height()-MinConfidence, height)
	require.Equal(m.t, m.head.Key(), key)
	return m.confidence, nil
}

func (m *filecoinWatchTestAPI) StateGetActor(_ context.Context, sender address.Address, key ltypes.TipSetKey) (*ltypes.Actor, error) {
	require.Equal(m.t, m.sender, sender)
	require.Equal(m.t, m.confidence.Key(), key)
	return &ltypes.Actor{Nonce: m.actorNonce}, nil
}

func (m *filecoinWatchTestAPI) StateSearchMsg(_ context.Context, key ltypes.TipSetKey, message cid.Cid, limit abi.ChainEpoch, allowReplaced bool) (*api.MsgLookup, error) {
	m.searchCalls++
	require.Equal(m.t, m.confidence.Key(), key)
	require.Equal(m.t, m.original, message)
	require.Equal(m.t, api.LookbackNoLimit, limit)
	require.True(m.t, allowReplaced)
	return m.lookup, nil
}

func (m *filecoinWatchTestAPI) ChainGetMessage(_ context.Context, message cid.Cid) (*ltypes.Message, error) {
	m.messageCalls++
	require.NotNil(m.t, m.lookup)
	require.Equal(m.t, m.lookup.Message, message)
	return m.message, nil
}
