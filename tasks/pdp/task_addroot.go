package pdp

//import (
//	"context"
//	"database/sql"
//	"errors"
//	"math/big"
//	"net/http"
//	"time"
//
//	"github.com/ethereum/go-ethereum/common"
//	"github.com/ethereum/go-ethereum/core/types"
//	"github.com/ethereum/go-ethereum/ethclient"
//	"github.com/filecoin-project/curio/harmony/harmonydb"
//	"github.com/filecoin-project/curio/harmony/harmonytask"
//	"github.com/filecoin-project/curio/harmony/resources"
//	"github.com/filecoin-project/curio/harmony/taskhelp"
//	"github.com/filecoin-project/curio/lib/passcall"
//	"github.com/filecoin-project/curio/pdp/contract"
//	"github.com/filecoin-project/curio/tasks/message"
//	types2 "github.com/filecoin-project/lotus/chain/types"
//	"golang.org/x/xerrors"
//)
//
//type PDPServiceNodeApi interface {
//	ChainHead(ctx context.Context) (*types2.TipSet, error)
//}
//
//type PDPTaskAddRoot struct {
//	db        *harmonydb.DB
//	sender    *message.SenderETH
//	ethClient *ethclient.Client
//	filClient PDPServiceNodeApi
//}
//
//func (p *PDPTaskAddRoot) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
//	ctx := context.Background()
//
//	// Step 5: Prepare the Ethereum transaction data outside the DB transaction
//	// Obtain the ABI of the PDPVerifier contract
//	abiData, err := contract.PDPVerifierMetaData.GetAbi()
//	if err != nil {
//		return false, xerrors.Errorf("getting PDPVerifier ABI: %w", err)
//	}
//
//	// Prepare RootData array for Ethereum transaction
//	// Define a Struct that matches the Solidity RootData struct
//	type RootData struct {
//		Root    struct{ Data []byte }
//		RawSize *big.Int
//	}
//
//	var rootDataArray []RootData
//
//	rootData := RootData{
//		Root:    struct{ Data []byte }{Data: rootCID.Bytes()},
//		RawSize: new(big.Int).SetUint64(totalSize),
//	}
//
//	// Step 6: Prepare the Ethereum transaction
//	// Pack the method call data
//	// The extraDataBytes variable is now correctly populated above
//	data, err := abiData.Pack("addRoots", proofSetID, rootDataArray, extraDataBytes)
//	if err != nil {
//		return false, xerrors.Errorf("packing data: %w", err)
//	}
//
//	// Step 7: Get the sender address from 'eth_keys' table where role = 'pdp' limit 1
//	fromAddress, err := p.getSenderAddress(ctx)
//	if err != nil {
//		return false, xerrors.Errorf("getting sender address: %w", err)
//	}
//
//	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
//	txEth := types.NewTransaction(
//		0,
//		contract.ContractAddresses().PDPVerifier,
//		big.NewInt(0),
//		0,
//		nil,
//		data,
//	)
//
//	// Step 8: Send the transaction using SenderETH
//	reason := "pdp-addroots"
//	txHash, err := p.sender.Send(ctx, fromAddress, txEth, reason)
//	if err != nil {
//		return false, xerrors.Errorf("sending transaction: %w", err)
//	}
//
//	// Step 9: Insert into message_waits_eth and pdp_proofset_roots
//	_, err = p.db.BeginTransaction(ctx, func(txdb *harmonydb.Tx) (bool, error) {
//		// Insert into message_waits_eth
//		_, err = txdb.Exec(`
//           INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
//           VALUES ($1, $2)
//       `, txHash.Hex(), "pending")
//		if err != nil {
//			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
//		}
//
//		// Update proof set for initialization upon first add
//		_, err = txdb.Exec(`
//			UPDATE pdp_proof_sets SET init_ready = true
//			WHERE id = $1 AND prev_challenge_request_epoch IS NULL AND challenge_request_msg_hash IS NULL AND prove_at_epoch IS NULL
//			`, proofSetIDUint64)
//		if err != nil {
//			return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
//		}
//
//		// Insert into pdp_proofset_roots
//
//		for addMessageIndex, addRootReq := range payload.Roots {
//			for _, subrootEntry := range addRootReq.Subroots {
//				subrootInfo := subrootInfoMap[subrootEntry.SubrootCID]
//
//				// Insert into pdp_proofset_roots
//				_, err = txdb.Exec(`
//                   INSERT INTO pdp_proofset_root_adds (
//                       proofset,
//                       root,
//                       add_message_hash,
//                       add_message_index,
//                       subroot,
//                       subroot_offset,
//						subroot_size,
//                       pdp_pieceref
//                   )
//                   VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
//               `,
//					proofSetIDUint64,
//					addRootReq.RootCID,
//					txHash.Hex(),
//					addMessageIndex,
//					subrootEntry.SubrootCID,
//					subrootInfo.SubrootOffset,
//					subrootInfo.PieceInfo.Size,
//					subrootInfo.PDPPieceRefID,
//				)
//				if err != nil {
//					return false, err
//				}
//			}
//		}
//
//		// Return true to commit the transaction
//		return true, nil
//	}, harmonydb.OptionRetry())
//	if err != nil {
//		return false, xerrors.Errorf("failed to save details to DB: %w", err)
//	}
//	return true, nil
//}
//
//// getSenderAddress retrieves the sender address from the database where role = 'pdp' limit 1
//func (p *PDPTaskAddRoot) getSenderAddress(ctx context.Context) (common.Address, error) {
//	var addressStr string
//	err := p.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addressStr)
//	if err != nil {
//		if errors.Is(err, sql.ErrNoRows) {
//			return common.Address{}, errors.New("no sender address with role 'pdp' found")
//		}
//		return common.Address{}, err
//	}
//	address := common.HexToAddress(addressStr)
//	return address, nil
//}
//
//func (p *PDPTaskAddRoot) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
//	return &ids[0], nil
//}
//
//func (p *PDPTaskAddRoot) TypeDetails() harmonytask.TaskTypeDetails {
//	return harmonytask.TaskTypeDetails{
//		Max:  taskhelp.Max(50),
//		Name: "PDPAddRoot",
//		Cost: resources.Resources{
//			Cpu: 1,
//			Ram: 64 << 20,
//		},
//		MaxFailures: 3,
//		IAmBored: passcall.Every(5*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
//			return p.schedule(context.Background(), taskFunc)
//		}),
//	}
//}
//
//func (p *PDPTaskAddRoot) Adder(taskFunc harmonytask.AddTaskFunc) {}
//
//var _ harmonytask.TaskInterface = &PDPTaskAddRoot{}
