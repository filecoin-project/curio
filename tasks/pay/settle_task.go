package pay

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

var log = logging.Logger("filecoin-pay-settle")

type SettleTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH
}

func (s *SettleTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sender string
	err = s.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' ORDER BY address ASC`).Scan(&sender)
	if err != nil {
		log.Errorf("ListPDPKeys: failed to select addresses: %v", err)
		return false, fmt.Errorf("failed to retrieve PDP operator addresses: %w", err)
	}

	if sender == "" {
		log.Error("ListPDPKeys: no PDP operator addresses found")
		return false, fmt.Errorf("no PDP operator addresses found")
	}

	opAddr := common.HexToAddress(sender)

	registryAddr, err := contract.ServiceRegistryAddress()
	if err != nil {
		return false, fmt.Errorf("failed to get service registry address: %w", err)
	}

	registry, err := contract.NewServiceProviderRegistry(registryAddr, s.ethClient)
	if err != nil {
		return false, fmt.Errorf("failed to create service registry: %w", err)
	}

	registered, err := registry.IsRegisteredProvider(&bind.CallOpts{Context: ctx}, opAddr)
	if err != nil {
		return false, fmt.Errorf("failed to check if provider is registered: %w", err)
	}

	if !registered {
		return false, xerrors.Errorf("provider is not registered")
	}

	provider, err := registry.GetProviderByAddress(&bind.CallOpts{Context: ctx}, opAddr)
	if err != nil {
		return false, fmt.Errorf("failed to get provider: %w", err)
	}

	payee := provider.Info.Payee

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService

	err = filecoinpayment.SettleLockupPeriod(ctx, s.db, s.ethClient, s.sender, opAddr, []common.Address{payee}, []common.Address{serviceAddr})
	if err != nil {
		return false, fmt.Errorf("failed to settle lockup period: %w", err)
	}
	return true, nil
}

func (s *SettleTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (s *SettleTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "Settle",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(time.Hour*12, s),
	}
}

func (s *SettleTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &SettleTask{}
var _ = harmonytask.Reg(&SettleTask{})
