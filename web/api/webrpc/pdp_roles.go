package webrpc

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	pdpwallet "github.com/filecoin-project/curio/pdp/wallet"
)

// PDPRoleStatus is the on-chain worker/payee view for the local PDP provider.
type PDPRoleStatus struct {
	Configured         bool   `json:"configured"`
	ProviderID         int64  `json:"providerId"`
	Worker             string `json:"worker"`
	Payee              string `json:"payee"`
	SameAddress        bool   `json:"sameAddress"`
	LocalWorker        string `json:"localWorker"`
	LocalWorkerMatches bool   `json:"localWorkerMatches"`
	WorkerMismatchNote string `json:"workerMismatchNote,omitempty"`
	LiveDataSets       int64  `json:"liveDataSets"`
	RailsToMove        int64  `json:"railsToMove"`
	RailsCurrent       int64  `json:"railsCurrent"`
	RailsNote          string `json:"railsNote,omitempty"`
	ActionNonce        string `json:"actionNonce"`
	RegistryAddress    string `json:"registryAddress"`
	FWSSAddress        string `json:"fwssAddress"`
	ChainID            string `json:"chainId"`
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
			out.WorkerMismatchNote = "Local Curio worker key does not match the on-chain worker. Sign a worker change with the owner (payee) wallet, which is not stored on this node, or import the current worker key."
		}
	}

	stale, current, scanErr := a.findDatasetPayeesNotEqualTo(ctx, eclient, common.HexToAddress(payee))
	if scanErr != nil {
		out.RailsNote = "Could not check whether payment rails match the payee: " + scanErr.Error()
	} else {
		out.RailsToMove = int64(len(stale))
		out.RailsCurrent = current
		if len(stale) == 0 {
			out.RailsNote = "Payment rails already pay the current payee."
		} else {
			out.RailsNote = fmt.Sprintf("%d data set(s) still pay a previous payee.", len(stale))
		}
	}
	return out, nil
}

// findDatasetPayeesNotEqualTo lists local data sets whose live FWSS payee is not registryPayee.
// Terminated data sets and data sets with no PDP rail are omitted. The caller has
// a reachable Ethereum client; registryPayee is the current registry payee.
func (a *WebRPC) findDatasetPayeesNotEqualTo(ctx context.Context, eclient ethchain.EthClient, registryPayee common.Address) ([]int64, int64, error) {
	var ids []int64
	if err := a.Deps.DB.Select(ctx, &ids, `SELECT id FROM pdp_data_sets ORDER BY id ASC`); err != nil {
		return nil, 0, xerrors.Errorf("list data sets: %w", err)
	}
	if len(ids) == 0 {
		return nil, 0, nil
	}
	view, err := a.fwssStateView(ctx, eclient)
	if err != nil {
		return nil, 0, err
	}

	results := make([]struct {
		id    int64
		stale bool
	}, len(ids))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i, id := range ids {
		g.Go(func() error {
			info, err := view.GetDataSet(contract.EthCallOpts(gctx), big.NewInt(id))
			if err != nil {
				return xerrors.Errorf("data set %d: %w", id, err)
			}
			if info.PdpRailId == nil || info.PdpRailId.Sign() == 0 {
				return nil
			}
			if info.PdpEndEpoch != nil && info.PdpEndEpoch.Sign() > 0 {
				return nil
			}
			results[i].id = id
			results[i].stale = info.Payee != registryPayee
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, 0, err
	}

	stale := make([]int64, 0)
	var current int64
	for _, result := range results {
		if result.id == 0 {
			continue
		}
		if result.stale {
			stale = append(stale, result.id)
			continue
		}
		current++
	}
	return stale, current, nil
}

// fwssStateView binds the FWSS read contract. The caller supplies the Ethereum client.
func (a *WebRPC) fwssStateView(ctx context.Context, eclient ethchain.EthClient) (*FWSS.FilecoinWarmStorageServiceStateView, error) {
	fwss := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	if fwss == (common.Address{}) {
		return nil, fmt.Errorf("FWSS address not configured")
	}
	viewAddr, err := contract.ResolveViewAddress(ctx, fwss, eclient)
	if err != nil {
		return nil, err
	}
	view, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, eclient)
	if err != nil {
		return nil, err
	}
	return view, nil
}

// PDPWorkerWalletTx is calldata for the payee browser wallet to submit changeServiceProvider.
type PDPWorkerWalletTx struct {
	To         string `json:"to"`
	Data       string `json:"data"`
	ChainID    string `json:"chainId"`
	FromMustBe string `json:"fromMustBe"`
	ProviderID int64  `json:"providerId"`
	NewWorker  string `json:"newWorker"`
	Note       string `json:"note"`
}

// PDPKeyAddress returns the Ethereum address for a worker private key.
// The key is not stored. The caller passes hex or a lotus wallet export.
func (a *WebRPC) PDPKeyAddress(ctx context.Context, keyMaterial string) (string, error) {
	privateKey, err := pdpwallet.ParsePrivateKeyMaterial(keyMaterial)
	if err != nil {
		return "", err
	}
	return pdpwallet.AddressFromPrivateKeyBytes(privateKey)
}

// ReplacePDPWorkerKey stores keyMaterial as the PDP worker in eth_keys.
// The caller has observed that this address is already the on-chain worker.
func (a *WebRPC) ReplacePDPWorkerKey(ctx context.Context, keyMaterial string) (string, error) {
	privateKey, err := pdpwallet.ParsePrivateKeyMaterial(keyMaterial)
	if err != nil {
		return "", err
	}
	address, err := pdpwallet.AddressFromPrivateKeyBytes(privateKey)
	if err != nil {
		return "", err
	}
	status, err := a.FSRegistryStatus(ctx)
	if err != nil {
		return "", err
	}
	if status == nil {
		return "", fmt.Errorf("provider is not registered")
	}
	if !strings.EqualFold(status.Address, address) {
		return "", fmt.Errorf("on-chain worker is %s, not %s; wait for the owner signature to confirm", status.Address, address)
	}
	stored, err := pdpwallet.ReplacePDPKey(ctx, a.Deps.DB, keyMaterial)
	if err != nil {
		return "", err
	}
	return stored, nil
}

// BuildChangeWorkerTx packs registry calldata for an owner (payee) browser wallet.
// Curio does not hold that key and does not submit this transaction.
func (a *WebRPC) BuildChangeWorkerTx(ctx context.Context, newWorker string) (*PDPWorkerWalletTx, error) {
	if !common.IsHexAddress(newWorker) {
		return nil, fmt.Errorf("invalid worker address")
	}
	status, err := a.FSRegistryStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, fmt.Errorf("provider is not registered")
	}
	if strings.EqualFold(newWorker, status.Address) {
		return nil, fmt.Errorf("new worker is unchanged")
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
	calldata, err := contract.PackChangeServiceProvider(big.NewInt(status.ID), common.HexToAddress(newWorker))
	if err != nil {
		return nil, err
	}
	return &PDPWorkerWalletTx{
		To:         registryAddr.Hex(),
		Data:       "0x" + common.Bytes2Hex(calldata),
		ChainID:    chainID.String(),
		FromMustBe: status.Payee,
		ProviderID: status.ID,
		NewWorker:  common.HexToAddress(newWorker).Hex(),
		Note:       "Sign with the owner (payee) wallet in MetaMask or Glif. That key is not stored in Curio. After confirmation, Curio stores the new worker key.",
	}, nil
}

// EstimatePayeeRetargetGas estimates FIL gas for one retargetDataSetPayee per data set
// whose rail still pays a previous payee. The caller has a registered provider.
func (a *WebRPC) EstimatePayeeRetargetGas(ctx context.Context) (*PDPPayeeRetargetEstimate, error) {
	status, err := a.FSRegistryStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, fmt.Errorf("provider is not registered")
	}

	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, err
	}
	stale, _, err := a.findDatasetPayeesNotEqualTo(ctx, eclient, common.HexToAddress(status.Payee))
	if err != nil {
		return nil, err
	}

	var perRail uint64
	if len(stale) > 0 {
		worker := common.HexToAddress(status.Address)
		perRail, err = contract.EstimateRetargetGas(ctx, eclient, worker, big.NewInt(stale[0]))
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

	totalGas := perRail * uint64(len(stale))
	cost := new(big.Int).Mul(baseFee, new(big.Int).SetUint64(totalGas))
	note := "Payment rails already pay the current payee."
	if len(stale) > 0 {
		note = "Estimate only. The worker wallet pays gas for each data set that still pays the previous payee. A data set whose payer cannot cover lockup keeps the old rail until retried."
	}

	return &PDPPayeeRetargetEstimate{
		LiveDataSets:     int64(len(stale)),
		PerRailGas:       perRail,
		TotalGas:         totalGas,
		BaseFeeWei:       baseFee.String(),
		EstimatedCostWei: cost.String(),
		EstimatedCostFil: formatAttoFil(cost),
		Note:             note,
	}, nil
}

// RetargetAllDatasetPayees submits retargetDataSetPayee for data sets that still pay a previous payee.
// The caller has a local pdp key that matches the on-chain worker; that key pays the gas.
func (a *WebRPC) RetargetAllDatasetPayees(ctx context.Context) ([]string, error) {
	status, err := a.FSRegistryStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, fmt.Errorf("provider is not registered")
	}
	var localWorker string
	if err := a.Deps.DB.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&localWorker); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no local pdp key; import the on-chain worker %s", status.Address)
		}
		return nil, xerrors.Errorf("reading local pdp key: %w", err)
	}
	if !strings.EqualFold(localWorker, status.Address) {
		return nil, fmt.Errorf("local pdp key %s is not the on-chain worker %s", localWorker, status.Address)
	}
	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, err
	}
	stale, _, err := a.findDatasetPayeesNotEqualTo(ctx, eclient, common.HexToAddress(status.Payee))
	if err != nil {
		return nil, err
	}
	if len(stale) == 0 {
		return nil, fmt.Errorf("payment rails already pay the current payee")
	}
	fwss := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	if fwss == (common.Address{}) {
		return nil, fmt.Errorf("FWSS address not configured")
	}

	hashes := make([]string, 0, len(stale))
	for _, id := range stale {
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
