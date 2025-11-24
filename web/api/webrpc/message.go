package webrpc

import (
	"context"
	"encoding/json"

	"github.com/filecoin-project/lotus/chain/types"
)

type MessageDetail struct {
	FromKey                 string          `db:"from_key" json:"from_key"`
	ToAddr                  string          `db:"to_addr" json:"to_addr"`
	SendReason              string          `db:"send_reason" json:"send_reason"`
	SendTaskID              int64           `db:"send_task_id" json:"send_task_id"`
	UnsignedData            []byte          `db:"unsigned_data" json:"unsigned_data"`
	UnsignedCID             string          `db:"unsigned_cid" json:"unsigned_cid"`
	Nonce                   NullInt64       `db:"nonce" json:"nonce"`
	SignedData              []byte          `db:"signed_data" json:"signed_data"`
	SignedJSON              json.RawMessage `db:"signed_json" json:"signed_json"`
	SignedCID               string          `db:"signed_cid" json:"signed_cid"`
	SendTime                NullTime        `db:"send_time" json:"send_time"`
	SendSuccess             NullBool        `db:"send_success" json:"send_success"`
	SendError               NullString      `db:"send_error" json:"send_error"`
	WaiterMachineID         NullInt64       `db:"waiter_machine_id" json:"waiter_machine_id"`
	ExecutedTSKCID          NullString      `db:"executed_tsk_cid" json:"executed_tsk_cid"`
	ExecutedTSKEpoch        NullInt64       `db:"executed_tsk_epoch" json:"executed_tsk_epoch"`
	ExecutedMsgCID          NullString      `db:"executed_msg_cid" json:"executed_msg_cid"`
	ExecutedMsgData         json.RawMessage `db:"executed_msg_data" json:"executed_msg_data"`
	ExecutedReceiptExitCode NullInt64       `db:"executed_rcpt_exitcode" json:"executed_rcpt_exitcode"`
	ExecutedReceiptReturn   []byte          `db:"executed_rcpt_return" json:"executed_rcpt_return"`
	ExecutedReceiptGasUsed  NullInt64       `db:"executed_rcpt_gas_used" json:"executed_rcpt_gas_used"`

	ValueStr string `db:"-" json:"value_str"`
	FeeStr   string `db:"-" json:"fee_str"`
}

func (a *WebRPC) MessageByCid(ctx context.Context, cid string) (*MessageDetail, error) {
	var messages []MessageDetail
	err := a.deps.DB.Select(ctx, &messages, `
        SELECT ms.from_key, ms.to_addr, ms.send_reason, ms.send_task_id,
               ms.unsigned_data, ms.unsigned_cid, ms.nonce, ms.signed_data,
               ms.signed_json, ms.signed_cid, ms.send_time, ms.send_success, ms.send_error,
               mw.waiter_machine_id, mw.executed_tsk_cid, mw.executed_tsk_epoch, mw.executed_msg_cid, mw.executed_msg_data,
               mw.executed_rcpt_exitcode, mw.executed_rcpt_return, mw.executed_rcpt_gas_used
        FROM message_sends ms
        LEFT JOIN message_waits mw ON mw.signed_message_cid = ms.signed_cid
        WHERE ms.unsigned_cid = $1 OR ms.signed_cid = $1
    `, cid)
	if err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return nil, nil
	}

	var smsg types.SignedMessage
	if err := json.Unmarshal(messages[0].SignedJSON, &smsg); err != nil {
		return nil, err
	}

	message := &messages[0]

	message.ValueStr = types.FIL(smsg.Message.Value).Short()
	message.FeeStr = types.FIL(smsg.Message.RequiredFunds()).Short()

	return message, nil
}
