package webrpc

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/pdp/contract"
)

// PDPRoleStatus is the on-chain worker/payee view for the local PDP provider.
type PDPRoleStatus struct {
	Configured          bool   `json:"configured"`
	ProviderID          int64  `json:"providerId"`
	Worker              string `json:"worker"`
	Payee               string `json:"payee"`
	SameAddress         bool   `json:"sameAddress"`
	LocalWorker         string `json:"localWorker"`
	LocalWorkerMatches  bool   `json:"localWorkerMatches"`
	WorkerMismatchNote  string `json:"workerMismatchNote,omitempty"`
	LiveDataSets        int64  `json:"liveDataSets"`
	ActionNonce         string `json:"actionNonce"`
	RegistryAddress     string `json:"registryAddress"`
	FWSSAddress         string `json:"fwssAddress"`
	ChainID             string `json:"chainId"`
}

// PDPPayeeRetargetEstimate is a gas estimate for retargeting rails after an on-chain payee change.
type PDPPayeeRetargetEstimate struct {
	LiveDataSets     int64  `json:"liveDataSets"`
	PerRailGas       uint64 `json:"perRailGas"`
	TotalGas         uint64 `json:"totalGas"`
	BaseFeeWei       string `json:"baseFeeWei"`
	EstimatedCostWei string `json:"estimatedCostWei"`
	EstimatedCostFil string `json:"estimatedCostFil"`
	Note             string `json:"note"`
}

// PDPPayeeWalletTx is calldata for the browser wallet (MetaMask/Glif) to submit as the payee.
// Curio never signs or broadcasts this; the connected payee account must be msg.sender.
type PDPPayeeWalletTx struct {
	Method      string `json:"method"`
	To          string `json:"to"`
	Data        string `json:"data"`
	ChainID     string `json:"chainId"`
	FromMustBe  string `json:"fromMustBe"`
	ProviderID  int64  `json:"providerId"`
	NewAddress  string `json:"newAddress"`
	Note        string `json:"note"`
}

// PDPRoleStatus returns the current worker and payee for this node's PDP provider.
func (a *WebRPC) PDPRoleStatus(ctx context.Context) (*PDPRoleStatus, error) {
	status, err := a.FSRegistryStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := &PDPRoleStatus{}
	if status == nil {
		return out, nil
	}

	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, fmt.Errorf("eth client: %w", err)
	}
	registryAddr, err := contract.ServiceRegistryAddress()
	if err != nil {
		return nil, err
	}
	chainID, err := eclient.NetworkID(ctx)
	if err != nil {
		return nil, err
	}

	var live int64
	if err := a.Deps.DB.QueryRow(ctx, `SELECT count(*) FROM pdp_data_sets`).Scan(&live); err != nil {
		return nil, xerrors.Errorf("count data sets: %w", err)
	}

	nonce, err := contract.ProviderActionNonce(ctx, eclient, big.NewInt(status.ID))
	if err != nil {
		// Contract may not be upgraded yet; treat as zero.
		nonce = big.NewInt(0)
	}

	worker := status.Address
	payee := status.Payee
	out.Configured = true
	out.ProviderID = status.ID
	out.Worker = worker
	out.Payee = payee
	out.SameAddress = strings.EqualFold(worker, payee)
	out.LiveDataSets = live
	out.ActionNonce = nonce.String()
	out.RegistryAddress = registryAddr.Hex()
	out.FWSSAddress = contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService.Hex()
	out.ChainID = chainID.String()

	var localWorker string
	if err := a.Deps.DB.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&localWorker); err == nil {
		out.LocalWorker = localWorker
		out.LocalWorkerMatches = strings.EqualFold(localWorker, worker)
		if !out.LocalWorkerMatches {
			out.WorkerMismatchNote = "Local Curio pdp key does not match the on-chain worker. Import the current worker private key, or have the payee call changeServiceProvider on the registry via Glif."
		}
	}
	return out, nil
}

// BuildPayeeWalletTx packs registry calldata for a payee-direct MetaMask/Glif transaction.
// method must be "changePayee" or "changeServiceProvider". Curio does not submit this tx.
func (a *WebRPC) BuildPayeeWalletTx(ctx context.Context, method string, newAddress string) (*PDPPayeeWalletTx, error) {
	method = strings.TrimSpace(method)
	if method != "changePayee" && method != "changeServiceProvider" {
		return nil, fmt.Errorf("method must be changePayee or changeServiceProvider")
	}
	if !common.IsHexAddress(newAddress) {
		return nil, fmt.Errorf("invalid address")
	}
	role, err := a.PDPRoleStatus(ctx)
	if err != nil {
		return nil, err
	}
	if !role.Configured {
		return nil, fmt.Errorf("provider is not registered")
	}

	providerID := big.NewInt(role.ProviderID)
	addr := common.HexToAddress(newAddress)
	var calldata []byte
	switch method {
	case "changePayee":
		if strings.EqualFold(newAddress, role.Payee) {
			return nil, fmt.Errorf("new payee is unchanged")
		}
		calldata, err = contract.PackChangePayee(providerID, addr)
	case "changeServiceProvider":
		if strings.EqualFold(newAddress, role.Worker) {
			return nil, fmt.Errorf("new worker is unchanged")
		}
		calldata, err = contract.PackChangeServiceProvider(providerID, addr)
	}
	if err != nil {
		return nil, err
	}

	return &PDPPayeeWalletTx{
		Method:     method,
		To:         role.RegistryAddress,
		Data:       "0x" + common.Bytes2Hex(calldata),
		ChainID:    role.ChainID,
		FromMustBe: role.Payee,
		ProviderID: role.ProviderID,
		NewAddress: common.HexToAddress(newAddress).Hex(),
		Note:       "Submit from the current payee account in MetaMask or Glif. Curio's worker key is not used.",
	}, nil
}

// EstimatePayeeRetargetGas estimates FIL gas for one retargetDataSetPayee per live data set.
// Payee/worker role changes are submitted by the payee browser wallet, not Curio.
func (a *WebRPC) EstimatePayeeRetargetGas(ctx context.Context) (*PDPPayeeRetargetEstimate, error) {
	role, err := a.PDPRoleStatus(ctx)
	if err != nil {
		return nil, err
	}
	if !role.Configured {
		return nil, fmt.Errorf("provider is not registered")
	}

	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, err
	}
	worker := common.HexToAddress(role.Worker)

	var perRail uint64
	if role.LiveDataSets > 0 {
		var sampleID int64
		if err := a.Deps.DB.QueryRow(ctx, `SELECT id FROM pdp_data_sets ORDER BY id ASC LIMIT 1`).Scan(&sampleID); err != nil {
			return nil, xerrors.Errorf("sample data set: %w", err)
		}
		perRail, err = contract.EstimateRetargetGas(ctx, eclient, worker, big.NewInt(sampleID))
		if err != nil {
			perRail = 400_000
		}
	}

	header, err := eclient.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	baseFee := header.BaseFee
	if baseFee == nil {
		baseFee = big.NewInt(0)
	}

	totalGas := perRail * uint64(role.LiveDataSets)
	cost := new(big.Int).Mul(baseFee, new(big.Int).SetUint64(totalGas))

	return &PDPPayeeRetargetEstimate{
		LiveDataSets:     role.LiveDataSets,
		PerRailGas:       perRail,
		TotalGas:         totalGas,
		BaseFeeWei:       baseFee.String(),
		EstimatedCostWei: cost.String(),
		EstimatedCostFil: formatAttoFil(cost),
		Note:             "Estimate only. After the payee changes on-chain via Glif, the worker wallet pays gas for each retarget. Data sets whose payer cannot cover lockup will fail and keep the old rail until retried.",
	}, nil
}

// QueueDataSetPayeeRetargets submits retargetDataSetPayee for each local data set from the worker key.
func (a *WebRPC) QueueDataSetPayeeRetargets(ctx context.Context) ([]string, error) {
	role, err := a.PDPRoleStatus(ctx)
	if err != nil {
		return nil, err
	}
	if !role.Configured {
		return nil, fmt.Errorf("provider is not registered")
	}
	if role.LocalWorker != "" && !role.LocalWorkerMatches {
		return nil, fmt.Errorf("local pdp key %s is not the on-chain worker %s", role.LocalWorker, role.Worker)
	}
	var ids []int64
	if err := a.Deps.DB.Select(ctx, &ids, `SELECT id FROM pdp_data_sets ORDER BY id ASC`); err != nil {
		return nil, xerrors.Errorf("list data sets: %w", err)
	}
	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, err
	}
	fwss := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	if fwss == (common.Address{}) {
		return nil, fmt.Errorf("FWSS address not configured")
	}

	hashes := make([]string, 0, len(ids))
	for _, id := range ids {
		calldata, err := contract.PackRetargetDataSetPayee(big.NewInt(id))
		if err != nil {
			return hashes, err
		}
		txHash, err := contract.SubmitWorkerTransaction(ctx, a.Deps.DB, eclient, fwss, calldata)
		if err != nil {
			return hashes, xerrors.Errorf("retarget data set %d: %w", id, err)
		}
		hashes = append(hashes, txHash)
	}
	return hashes, nil
}

func formatAttoFil(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	// 1 FIL = 1e18 attoFIL/wei
	rat := new(big.Rat).SetFrac(wei, big.NewInt(1e18))
	return rat.FloatString(6)
}
