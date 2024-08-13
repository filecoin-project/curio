package storage_market

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	market9 "github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/exitcode"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/promise"

	"github.com/filecoin-project/lotus/api"
	apitypes "github.com/filecoin-project/lotus/api/types"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/types"
)

var fdLog = logging.Logger("Post-PSD")

type fdealApi interface {
	headAPI
	ChainGetMessage(context.Context, cid.Cid) (*types.Message, error)
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateMarketStorageDeal(context.Context, abi.DealID, types.TipSetKey) (*api.MarketDeal, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (apitypes.NetworkVersion, error)
	StateSearchMsg(ctx context.Context, from types.TipSetKey, msg cid.Cid, limit abi.ChainEpoch, allowReplaced bool) (*api.MsgLookup, error)
}

// FindDealTask represents a task for finding and identifying on chain deals once deal have been published.
// Once PublishStorageDeal message has been successfully executed, each proposal is assigned a deal.
// These deal ID must be matched to the original sent proposals so a local deal can be identified with an
// on chain deal ID.
type FindDealTask struct {
	sm  *CurioStorageDealMarket
	db  *harmonydb.DB
	api fdealApi
	TF  promise.Promise[harmonytask.AddTaskFunc]
	cfg *config.MK12Config
}

func NewFindDealTask(sm *CurioStorageDealMarket, db *harmonydb.DB, api fdealApi, cfg *config.MK12Config) *FindDealTask {
	return &FindDealTask{
		sm:  sm,
		db:  db,
		api: api,
		cfg: cfg,
	}
}

func (f *FindDealTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var bdeals []struct {
		UUID       string          `db:"uuid"`
		PublishCid string          `db:"publish_cid"`
		Proposal   json.RawMessage `db:"proposal"`
	}

	err = f.db.Select(ctx, &bdeals, `SELECT 
										p.uuid,
										b.publish_cid,
										b.proposal
									FROM 
										market_mk12_deal_pipeline p
									JOIN 
										market_mk12_deals b ON p.uuid = b.uuid
									WHERE 
										p.find_deal_task_id = $1;`, taskID)

	if err != nil {
		return false, xerrors.Errorf("getting deals from db: %w", err)
	}

	if len(bdeals) != 1 {
		return false, xerrors.Errorf("expected 1 deal, got %d", len(bdeals))
	}
	bd := bdeals[0]

	expired, err := checkExpiry(ctx, f.db, f.api, bd.UUID, f.cfg.ExpectedSealDuration)
	if err != nil {
		return false, xerrors.Errorf("deal %s expired: %w", bd.UUID, err)
	}
	if expired {
		return true, nil
	}

	pcd, err := cid.Parse(bd.PublishCid)
	if err != nil {
		return false, xerrors.Errorf("parsing publishCid: %w", err)
	}

	var prop market.DealProposal
	err = json.Unmarshal(bd.Proposal, &prop)
	if err != nil {
		return false, xerrors.Errorf("unmarshalling proposal: %w", err)
	}

	var execResult []struct {
		ExecutedTskCID   string `db:"executed_tsk_cid"`
		ExecutedTskEpoch int64  `db:"executed_tsk_epoch"`
		ExecutedMsgCID   string `db:"executed_msg_cid"`

		ExecutedRcptExitCode int64 `db:"executed_rcpt_exitcode"`
		ExecutedRcptGasUsed  int64 `db:"executed_rcpt_gas_used"`
	}

	err = f.db.Select(ctx, &execResult, `SELECT executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, 
       					executed_rcpt_exitcode, executed_rcpt_gas_used
						FROM message_waits
						WHERE signed_message_cid AND executed_tsk_epoch IS NOT NULL`, bd.PublishCid)
	if err != nil {
		fdLog.Errorw("failed to query message_waits", "error", err)
	}
	if len(execResult) != 1 {
		return false, xerrors.Errorf("expected 1 result, got %d", len(execResult))
	}

	res := execResult[0]
	if exitcode.ExitCode(res.ExecutedRcptExitCode) != exitcode.Ok {
		// Reset the deal to after_commp state
		err = f.resendPSD(ctx, bd.UUID, taskID)
		if err != nil {
			return false, xerrors.Errorf("Storing failure FindDeal: %w", err)
		}
		return true, nil
	}

	// Get the return value of the publish deals message
	wmsg, err := f.api.StateSearchMsg(ctx, types.EmptyTSK, pcd, api.LookbackNoLimit, true)
	if err != nil {
		return false, xerrors.Errorf("getting publish deals message return value: %w", err)
	}

	if wmsg == nil {
		return false, xerrors.Errorf("looking for publish deal message %s: not found", pcd.String())
	}

	nv, err := f.api.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting network version: %w", err)
	}

	retval, err := market.DecodePublishStorageDealsReturn(wmsg.Receipt.Return, nv)
	if err != nil {
		return false, xerrors.Errorf("looking for publish deal message %s: decoding message return: %w", pcd, err)
	}

	dealIDs, err := retval.DealIDs()
	if err != nil {
		return false, xerrors.Errorf("looking for publish deal message %s: getting dealIDs: %w", pcd, err)
	}

	// Get the parameters to the publish deals message
	pubmsg, err := f.api.ChainGetMessage(ctx, pcd)
	if err != nil {
		return false, xerrors.Errorf("getting publish deal message %s: %w", pcd, err)
	}

	var pubDealsParams market9.PublishStorageDealsParams
	if err := pubDealsParams.UnmarshalCBOR(bytes.NewReader(pubmsg.Params)); err != nil {
		return false, xerrors.Errorf("unmarshalling publish deal message params for message %s: %w", pcd, err)
	}

	// Scan through the deal proposals in the message parameters to find the
	// index of the target deal proposal
	dealIdx := -1
	for i, paramDeal := range pubDealsParams.Deals {
		eq, err := f.checkDealEquality(ctx, prop, paramDeal.Proposal)
		if err != nil {
			return false, xerrors.Errorf("comparing publish deal message %s proposal to deal proposal: %w", pcd, err)
		}
		if eq {
			dealIdx = i
			break
		}
	}

	if dealIdx == -1 {
		return false, xerrors.Errorf("could not find deal in publish deals message %s", pcd)
	}

	if dealIdx >= len(pubDealsParams.Deals) {
		return false, xerrors.Errorf(
			"deal index %d out of bounds of deal proposals (len %d) in publish deals message %s",
			dealIdx, len(pubDealsParams.Deals), pcd)
	}

	valid, outIdx, err := retval.IsDealValid(uint64(dealIdx))
	if err != nil {
		return false, xerrors.Errorf("determining deal validity: %w", err)
	}

	if !valid {
		return false, xerrors.Errorf("deal was invalid at publication")
	}

	// final check against for invalid return value output
	// should not be reachable from onchain output, only pathological test cases
	if outIdx >= len(dealIDs) {
		return false, fmt.Errorf("invalid publish storage deals ret marking %d as valid while only returning %d valid deals in publish deal message %s", outIdx, len(dealIDs), pcd)
	}

	onChainID := dealIDs[outIdx]

	// Lookup the deal state by deal ID
	marketDeal, err := f.api.StateMarketStorageDeal(ctx, onChainID, types.EmptyTSK)
	if err == nil {
		// Make sure the retrieved deal proposal matches the target proposal
		equal, err := f.checkDealEquality(ctx, prop, marketDeal.Proposal)
		if err != nil {
			return false, xerrors.Errorf("verifying proposal")
		}
		if !equal {
			return false, xerrors.Errorf("Deal proposals for publish message %s did not match", pcd)
		}
	}

	comm, err := f.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE market_mk12_deals SET chain_deal_id = $1 WHERE uuid = $2`, onChainID, bd.UUID)
		if err != nil {
			return false, xerrors.Errorf("failed to update on chain deal ID in DB: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("failed to update on chain deal ID in DB: expected 1 rows affected, got %d", n)
		}
		n, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET after_find_deal = TRUE, find_deal_task_id = NULL WHERE find_deal_task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("DealFind store success: %w", err)
		}
		return n == 1, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("updating DB: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit the PSD success to DB")
	}
	return true, nil

}

func (f *FindDealTask) checkDealEquality(ctx context.Context, p1, p2 market.DealProposal) (bool, error) {
	p1ClientID, err := f.api.StateLookupID(ctx, p1.Client, types.EmptyTSK)
	if err != nil {
		return false, err
	}
	p2ClientID, err := f.api.StateLookupID(ctx, p2.Client, types.EmptyTSK)
	if err != nil {
		return false, err
	}
	res := p1.PieceCID.Equals(p2.PieceCID) &&
		p1.PieceSize == p2.PieceSize &&
		p1.VerifiedDeal == p2.VerifiedDeal &&
		p1.Label.Equals(p2.Label) &&
		p1.StartEpoch == p2.StartEpoch &&
		p1.EndEpoch == p2.EndEpoch &&
		p1.StoragePricePerEpoch.Equals(p2.StoragePricePerEpoch) &&
		p1.ProviderCollateral.Equals(p2.ProviderCollateral) &&
		p1.ClientCollateral.Equals(p2.ClientCollateral) &&
		p1.Provider == p2.Provider &&
		p1ClientID == p2ClientID

	fdLog.Debugw("check deal quality", "result", res, "p1clientid", p1ClientID, "p2clientid", p2ClientID, "label_equality", p1.Label.Equals(p2.Label), "provider_equality", p1.Provider == p2.Provider)

	return res, nil
}

func (f *FindDealTask) resendPSD(ctx context.Context, deal string, taskID harmonytask.TaskID) error {
	n, err := f.db.Exec(ctx, `UPDATE market_mk12_deals SET publish_cid = NULL WHERE uuid = $1`, deal)
	if err != nil {
		return err
	}
	if n != 1 {
		return xerrors.Errorf("expected 1 rows but got %d", n)
	}
	n, err = f.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET after_find_deal = FALSE, find_deal_task_id = NULL WHERE find_deal_task_id = $1`, taskID)
	if err != nil {
		return err
	}
	if n != 1 {
		return xerrors.Errorf("expected 1 rows but got %d", n)
	}
	return nil
}

func (f *FindDealTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (f *FindDealTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  128,
		Name: "FindDeal",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 16,
	}
}

func (f *FindDealTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	f.sm.pollers[pollerFindDeal].Set(taskFunc)
}

var _ = harmonytask.Reg(&FindDealTask{})
var _ harmonytask.TaskInterface = &FindDealTask{}
