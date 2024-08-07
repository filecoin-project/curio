package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/exitcode"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/ctladdr"
)

const PSDTaskPollInterval = 3 * time.Second

type psdApi interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateCall(context.Context, *types.Message, types.TipSetKey) (*api.InvocResult, error)
	ctladdr.NodeApi
}

type PSDTask struct {
	db     *harmonydb.DB
	sender *message.Sender
	as     *multictladdr.MultiAddressSelector
	cfg    *config.MK12Config
	api    psdApi

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewPSDTask(db *harmonydb.DB, sender *message.Sender, as *multictladdr.MultiAddressSelector, cfg *config.MK12Config, api psdApi) *PSDTask {
	pt := &PSDTask{
		db:     db,
		sender: sender,
		as:     as,
		cfg:    cfg,
		api:    api,
	}

	ctx := context.Background()
	go pt.pollPSDTasks(ctx)
	return pt
}

func (p *PSDTask) pollPSDTasks(ctx context.Context) {
	for {
		// Get all deals which do not have a URL and are not after_find
		var deals []struct {
			UUID string    `db:"uuid"`
			SpID int64     `db:"sp_id"`
			Time time.Time `db:"psd_wait_time"`
		}
		err := p.db.Select(ctx, &deals, `SELECT uuid, sp_id, psd_wait_time FROM market_mk12_deal_pipeline 
             WHERE after_commp = TRUE AND after_psd = FALSE AND psd_task_id = NULL`)

		if err != nil {
			log.Errorf("getting deal without URL: %w", err)
			time.Sleep(PSDTaskPollInterval)
			continue
		}

		if len(deals) == 0 {
			time.Sleep(PSDTaskPollInterval)
			continue
		}

		type queue struct {
			deals []string
			t     time.Time
		}
		dm := make(map[int64]queue)
		for _, deal := range deals {
			// Check if the spID is already in the map
			if q, exists := dm[deal.SpID]; exists {
				// Append the UUID to the deals list
				q.deals = append(q.deals, deal.UUID)

				// Update the time if the current deal's time is older
				if deal.Time.Before(q.t) {
					q.t = deal.Time
				}

				// Update the map with the new queue
				dm[deal.SpID] = q
			} else {
				// Add a new entry to the map if spID is not present
				dm[deal.SpID] = queue{
					deals: []string{deal.UUID},
					t:     deal.Time,
				}
			}
		}

		for _, q := range dm {
			if q.t.Add(time.Duration(p.cfg.PublishMsgPeriod)).After(time.Now()) || uint64(len(q.deals)) > p.cfg.MaxDealsPerPublishMsg {
				p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
					// update
					n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET psd_task_id = $1 WHERE uuid = ANY($2) AND psd_task_id IS NULL`, id, q.deals)
					if err != nil {
						return false, xerrors.Errorf("updating deal pipeline: %w", err)
					}
					return n > 0, nil
				})
			}
		}
	}
}

func (p *PSDTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var bdeals []struct {
		Prop json.RawMessage `db:"proposal"`
		Sig  []byte          `db:"proposal_signature"`
		UUID string          `db:"uuid"`
	}

	err = p.db.Select(ctx, &bdeals, `SELECT 
										p.uuid,
										b.proposal,
										b.proposal_signature
									FROM 
										market_mk12_deal_pipeline p
									JOIN 
										market_12_deals b ON p.uuid = b.uuid
									WHERE 
										p.psd_task_id = $1;`, taskID)

	if err != nil {
		return false, xerrors.Errorf("getting deals from db: %w", err)
	}

	type deal struct {
		uuid  string
		sprop market.ClientDealProposal
	}

	var deals []deal

	for _, d := range bdeals {
		d := d

		var prop market.DealProposal
		err = json.Unmarshal(d.Prop, &prop)
		if err != nil {
			return false, xerrors.Errorf("unmarshal proposal: %w", err)
		}

		var sig *crypto.Signature
		err = sig.UnmarshalBinary(d.Sig)
		if err != nil {
			return false, xerrors.Errorf("unmarshal signature: %w", err)
		}

		deals = append(deals, deal{
			uuid: d.UUID,
			sprop: market.ClientDealProposal{
				Proposal:        prop,
				ClientSignature: *sig,
			},
		})
	}

	// Validate each deal and skip(fail) the ones which fail validation
	var validDeals []deal
	mi, err := p.api.StateMinerInfo(ctx, deals[0].sprop.Proposal.Provider, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting provider info: %w", err)
	}
	for _, d := range deals {
		pcid, err := d.sprop.Proposal.Cid()
		if err != nil {
			return false, xerrors.Errorf("computing proposal cid: %w", err)
		}

		head, err := p.api.ChainHead(ctx)
		if err != nil {
			return false, err
		}
		buff := int64(math.Floor(time.Duration(p.cfg.ExpectedSealDuration).Seconds() / float64(build.BlockDelaySecs)))
		if head.Height()+abi.ChainEpoch(buff) > d.sprop.Proposal.StartEpoch {
			log.Errorf(
				"cannot publish deal with piece CID %s: current epoch %d has passed deal proposal start epoch %d",
				d.sprop.Proposal.PieceCID, head.Height(), d.sprop.Proposal.StartEpoch)
			// Store error in main MK12 deal Table and Eject the deal from pipeline
			err = failDeal(ctx, p.db, d.uuid, true, fmt.Sprintf("deal proposal must be proven on chain by deal proposal start epoch %d, but it has expired: current chain height: %d",
				d.sprop.Proposal.StartEpoch, head.Height()))
			if err != nil {
				return false, err
			}
			continue
		}

		params, err := actors.SerializeParams(&market.PublishStorageDealsParams{
			Deals: []market.ClientDealProposal{d.sprop},
		})
		if err != nil {
			return false, xerrors.Errorf("serializing PublishStorageDeals params failed: %w", err)
		}

		addr, _, err := p.as.AddressFor(ctx, p.api, d.sprop.Proposal.Provider, mi, api.DealPublishAddr, big.Zero(), big.Zero())
		if err != nil {
			return false, xerrors.Errorf("selecting address for publishing deals: %w", err)
		}

		res, err := p.api.StateCall(ctx, &types.Message{
			To:     builtin.StorageMarketActorAddr,
			From:   addr,
			Value:  types.NewInt(0),
			Method: builtin.MethodsMarket.PublishStorageDeals,
			Params: params,
		}, head.Key())
		if err != nil {
			return false, xerrors.Errorf("simulating deal publish message: %w", err)
		}
		if res.MsgRct.ExitCode != exitcode.Ok {
			// If PSD simulation fails then skip the deal
			log.Errorf("simulating deal publish message: non-zero exitcode %s; message: %s", res.MsgRct.ExitCode, res.Error)
			err = failDeal(ctx, p.db, d.uuid, true, fmt.Sprintf("simulating deal publish message: non-zero exitcode %s; message: %s", res.MsgRct.ExitCode, res.Error))
			if err != nil {
				return false, err
			}
			continue
		}
		log.Debugf("validated deal proposal %s successfully", pcid)
		validDeals = append(validDeals, d)
	}

	// Send PSD for valid deals
	var vdeals []market.ClientDealProposal
	for _, p := range validDeals {
		vdeals = append(vdeals, p.sprop)
	}
	params, err := actors.SerializeParams(&market.PublishStorageDealsParams{
		Deals: vdeals,
	})

	if err != nil {
		return false, xerrors.Errorf("serializing PublishStorageDeals params failed: %w", err)
	}

	addr, _, err := p.as.AddressFor(ctx, p.api, vdeals[0].Proposal.Provider, mi, api.DealPublishAddr, big.Zero(), big.Zero())
	if err != nil {
		return false, xerrors.Errorf("selecting address for publishing deals: %w", err)
	}

	msg := &types.Message{
		To:     builtin.StorageMarketActorAddr,
		From:   addr,
		Method: builtin.MethodsMarket.PublishStorageDeals,
		Params: params,
		Value:  types.NewInt(0),
	}

	mss := &api.MessageSendSpec{
		MaxFee: abi.TokenAmount(p.cfg.MaxPublishDealsFee),
	}

	mcid, err := p.sender.Send(ctx, msg, mss, "psd")

	if err != nil {
		return false, xerrors.Errorf("pushing deal publish message: %w", err)
	}

	log.Infof("published %d deals with message CID %s", len(vdeals), mcid)

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var uuids []string
		// Update Boost Deal table with publish CID
		for _, s := range validDeals {
			uuids = append(uuids, s.uuid)
		}
		n, err := tx.Exec(`UPDATE market_mk12_deals SET publish_cid = $1 WHERE uuid = ANY($2)`, mcid.String(), uuids)
		if err != nil {
			return false, xerrors.Errorf("failed to update publish CID in DB: %w", err)
		}
		if n != len(validDeals) {
			return false, xerrors.Errorf("failed to update publish CID in DB: expected %d rows affected, got %d", len(validDeals), n)
		}

		// Update deal pipeline for successful deal published
		n, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET after_psd = TRUE WHERE uuid = ANY($1)`, uuids)
		if err != nil {
			return false, xerrors.Errorf("PSD store success: %w", err)
		}
		if n != len(validDeals) {
			return false, xerrors.Errorf("PSD store success: expected %d rows affected, got %d", len(validDeals), n)
		}

		// Update deal pipeline for valid+invalid deals
		n, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET psd_task_id = NULL WHERE psd_task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("PSD store success: %w", err)
		}
		if n != len(bdeals) {
			return false, xerrors.Errorf("PSD store success: expected %d rows affected, got %d", len(bdeals), n)
		}

		// Update message wait
		_, err = tx.Exec(`INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, mcid)
		if err != nil {
			return false, xerrors.Errorf("inserting into message_waits: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("updating DB: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit the PSD success to DB")
	}

	return true, nil
}

func (p *PSDTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (p *PSDTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  128,
		Name: "PSD",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 16,
	}
}

func (p *PSDTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.TF.Set(taskFunc)
}

var _ = harmonytask.Reg(&PSDTask{})
var _ harmonytask.TaskInterface = &PSDTask{}
