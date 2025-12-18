package pdpv1

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

type PDPTaskAddDataSet struct {
	db        *harmonydb.DB
	sender    *message.SenderETH
	ethClient *ethclient.Client
	filClient PDPServiceNodeApi
}

func NewPDPTaskAddDataSet(db *harmonydb.DB, sender *message.SenderETH, ethClient *ethclient.Client, filClient PDPServiceNodeApi) *PDPTaskAddDataSet {
	return &PDPTaskAddDataSet{
		db:        db,
		sender:    sender,
		ethClient: ethClient,
		filClient: filClient,
	}
}

func (p *PDPTaskAddDataSet) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()
	var pcreates []struct {
		RecordKeeper string `db:"record_keeper"`
		ExtraData    []byte `db:"extra_data"`
	}

	err = p.db.Select(ctx, &pcreates, `SELECT record_keeper, extra_data FROM pdp_data_set_create WHERE task_id = $1 AND tx_hash IS NULL`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details from DB: %w", err)
	}

	if len(pcreates) != 1 {
		return false, xerrors.Errorf("incorrect rows for dataset create found for taskID %d", taskID)
	}

	pcreate := pcreates[0]

	recordKeeperAddr := common.HexToAddress(pcreate.RecordKeeper)
	if recordKeeperAddr == (common.Address{}) {
		return false, xerrors.Errorf("invalid record keeper address: %s", pcreate.RecordKeeper)
	}

	extraDataBytes := []byte{}

	if pcreate.ExtraData != nil {
		extraDataBytes = pcreate.ExtraData
	}

	// Get the sender address from 'eth_keys' table where role = 'pdp' limit 1
	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get sender address: %w", err)
	}

	// Manually create the transaction without requiring a Signer
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("getting PDPVerifier ABI: %w", err)
	}

	// Pack the method call data
	data, err := abiData.Pack("createDataSet", recordKeeperAddr, extraDataBytes)
	if err != nil {
		return false, xerrors.Errorf("packing data: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	tx := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPVerifier,
		contract.SybilFee(),
		0,
		nil,
		data,
	)

	// Send the transaction using SenderETH
	reason := "pdp-create-data-set"
	txHash, err := p.sender.Send(ctx, fromAddress, tx, reason)
	if err != nil {
		return false, xerrors.Errorf("sending transaction: %w", err)
	}

	// Insert into message_waits_eth and pdp_data_set_create
	txHashLower := strings.ToLower(txHash.Hex())
	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE pdp_data_set_create SET tx_hash = $1, task_id = NULL WHERE task_id = $2`, txHashLower, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_set_create: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows updated for pdp_data_set_create: %d", n)
		}
		_, err = tx.Exec(`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, $2)`, txHashLower, "pending")
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, xerrors.Errorf("failed to commit transaction: %w", err)
	}

	if !comm {
		return false, xerrors.Errorf("failed to commit transaction")
	}

	return true, nil
}

func (p *PDPTaskAddDataSet) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (p *PDPTaskAddDataSet) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPAddDataSet",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(3*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return p.schedule(context.Background(), taskFunc)
		}),
	}
}

func (p *PDPTaskAddDataSet) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_data_set_create WHERE task_id IS NULL AND tx_hash IS NULL LIMIT 1`).Scan(&did)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query pdp_data_set_create: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid id found for taskID")
			}

			_, err = tx.Exec(`UPDATE pdp_data_set_create SET task_id = $1 WHERE id = $2 AND tx_hash IS NULL`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_data_set_create: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

// getSenderAddress retrieves the sender address from the database where role = 'pdp' limit 1
func (p *PDPTaskAddDataSet) getSenderAddress(ctx context.Context) (common.Address, error) {
	var addressStr string
	err := p.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addressStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Address{}, errors.New("no sender address with role 'pdp' found")
		}
		return common.Address{}, err
	}
	address := common.HexToAddress(addressStr)
	return address, nil
}

func (p *PDPTaskAddDataSet) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPTaskAddDataSet{}
var _ = harmonytask.Reg(&PDPTaskAddDataSet{})
