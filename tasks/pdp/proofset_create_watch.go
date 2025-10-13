package pdp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/nonffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type DataSetCreate struct {
	CreateMessageHash string `db:"create_message_hash"`
	Service           string `db:"service"`
}

func NewWatcherCreate(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched, sender *message.SenderETH) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetCreates(ctx, db, ethClient, sender)
		if err != nil {
			log.Warnf("Failed to process pending data set creates: %v", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingDataSetCreates(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, sender *message.SenderETH) error {
	// Query for pdp_data_set_creates entries where ok = TRUE and data_set_created = FALSE
	var dataSetCreates []DataSetCreate

	err := db.Select(ctx, &dataSetCreates, `
        SELECT create_message_hash, service
        FROM pdp_data_set_creates
        WHERE ok = TRUE AND data_set_created = FALSE
    `)
	if err != nil {
		return xerrors.Errorf("failed to select data set creates: %w", err)
	}

	log.Infow("DataSetCreate watcher checking pending data sets", "count", len(dataSetCreates))

	if len(dataSetCreates) == 0 {
		// No pending data set creates
		return nil
	}

	// Process each data set create
	for _, psc := range dataSetCreates {
		log.Infow("Processing data set create",
			"txHash", psc.CreateMessageHash,
			"service", psc.Service)
		err := processDataSetCreate(ctx, db, psc, ethClient, sender)
		if err != nil {
			log.Warnf("Failed to process data set create for tx %s: %v", psc.CreateMessageHash, err)
			continue
		}
		log.Infow("Successfully processed data set create", "txHash", psc.CreateMessageHash)
	}

	return nil
}

func processDataSetCreate(ctx context.Context, db *harmonydb.DB, psc DataSetCreate, ethClient *ethclient.Client, sender *message.SenderETH) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	log.Debugw("Fetching tx_receipt from message_waits_eth", "txHash", psc.CreateMessageHash)
	err := db.QueryRow(ctx, `
        SELECT tx_receipt
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, psc.CreateMessageHash).Scan(&txReceiptJSON)
	if err != nil {
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", psc.CreateMessageHash, err)
	}
	log.Debugw("Retrieved tx_receipt", "txHash", psc.CreateMessageHash, "receiptLength", len(txReceiptJSON))

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", psc.CreateMessageHash, err)
	}
	log.Debugw("Unmarshalled receipt", "txHash", psc.CreateMessageHash, "status", txReceipt.Status, "logs", len(txReceipt.Logs))

	// Parse the logs to extract the dataSetId
	dataSetId, err := extractDataSetIdFromReceipt(&txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to extract dataSetId from receipt for tx %s: %w", psc.CreateMessageHash, err)
	}
	log.Infow("Extracted dataSetId from receipt", "txHash", psc.CreateMessageHash, "dataSetId", dataSetId)

	// Get the listener address for this data set from the PDPVerifier contract
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	listenerAddr, err := pdpVerifier.GetDataSetListener(nil, big.NewInt(int64(dataSetId)))
	if err != nil {
		return xerrors.Errorf("failed to get listener address for data set %d: %w", dataSetId, err)
	}

	// Get the proving period from the listener
	// Assumption: listener is a PDP Service with proving window informational methods
	provingPeriod, challengeWindow, err := getProvingPeriodChallengeWindow(ctx, ethClient, listenerAddr)
	if err != nil {
		return xerrors.Errorf("failed to get max proving period: %w", err)
	}

	// Insert a new entry into pdp_data_sets
	err = insertDataSet(ctx, db, psc.CreateMessageHash, dataSetId, psc.Service, provingPeriod, challengeWindow)
	if err != nil {
		return xerrors.Errorf("failed to insert data set %d for tx %+v: %w", dataSetId, psc, err)
	}

	// Update pdp_data_set_creates to set data_set_created = TRUE
	_, err = db.Exec(ctx, `
        UPDATE pdp_data_set_creates
        SET data_set_created = TRUE
        WHERE create_message_hash = $1
    `, psc.CreateMessageHash)
	if err != nil {
		return xerrors.Errorf("failed to update data_set_creates for tx %s: %w", psc.CreateMessageHash, err)
	}

	var raw json.RawMessage
	err = db.QueryRow(ctx, `SELECT payload FROM pdp_pending_piece_adds WHERE create_message_hash = $1 AND service = $2`, psc.CreateMessageHash, psc.Service).Scan(&raw)
	if err == nil && len(raw) > 0 {
		var req struct {
			RecordKeeper string  `json:"recordKeeper"`
			ExtraData    *string `json:"extraData,omitempty"`
			Pieces       *struct {
				Pieces []struct {
					PieceCID  string `json:"pieceCid"`
					SubPieces []struct {
						SubPieceCID string `json:"subPieceCid"`
					} `json:"subPieces"`
				} `json:"pieces"`
				ExtraData *string `json:"extraData,omitempty"`
			} `json:"pieces,omitempty"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return xerrors.Errorf("failed to decode pending piece intent: %w", err)
		}
		if req.Pieces == nil || len(req.Pieces.Pieces) == 0 {
			_, _ = db.Exec(ctx, `DELETE FROM pdp_pending_piece_adds WHERE create_message_hash = $1`, psc.CreateMessageHash)
			return nil
		}

		// Build pieceData array and sub-piece info similar to handler
		type subInfo struct {
			PaddedSize uint64
			RawSize    uint64
			Offset     uint64
			RefID      int64
		}
		subPieceInfoMap := make(map[string]*subInfo)

		unique := make(map[string]struct{})
		for _, p := range req.Pieces.Pieces {
			for _, sp := range p.SubPieces {
				unique[sp.SubPieceCID] = struct{}{}
			}
		}
		subList := make([]string, 0, len(unique))
		for k := range unique {
			subList = append(subList, k)
		}

		rows, err := db.Query(ctx, `
            SELECT ppr.piece_cid, ppr.id AS pdp_pieceref_id, pp.piece_padded_size, pp.piece_raw_size
            FROM pdp_piecerefs ppr
            JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
            JOIN parked_pieces pp ON pp.id = pprf.piece_id
            WHERE ppr.service = $1 AND ppr.piece_cid = ANY($2)
        `, psc.Service, subList)
		if err != nil {
			return xerrors.Errorf("failed subpiece lookup: %w", err)
		}
		for rows.Next() {
			var cidStr string
			var refID int64
			var pad, raw uint64
			if err := rows.Scan(&cidStr, &refID, &pad, &raw); err != nil {
				return err
			}
			subPieceInfoMap[cidStr] = &subInfo{PaddedSize: pad, RawSize: raw, RefID: refID}
		}
		rows.Close()

		type PieceData struct{ Data []byte }
		var pieceDataArray []PieceData

		type staged struct {
			PieceCIDv1 string
			SubCIDv1   string
			Offset     uint64
			Size       uint64
			RefID      int64
			MsgIdx     int
		}
		var stagedRows []staged

		for msgIdx, p := range req.Pieces.Pieces {
			var totalOffset uint64
			c, err := cid.Decode(p.PieceCID)
			if err != nil {
				return xerrors.Errorf("invalid pieceCid: %w", err)
			}
			_, rawSize, err := commcid.PieceCidV1FromV2(c)
			if err != nil {
				return xerrors.Errorf("invalid commP v2: %w", err)
			}

			pieceInfos := make([]abi.PieceInfo, len(p.SubPieces))
			var sum uint64
			var prevSize uint64
			for i, sp := range p.SubPieces {
				info, ok := subPieceInfoMap[sp.SubPieceCID]
				if !ok {
					return fmt.Errorf("subPiece not found: %s", sp.SubPieceCID)
				}
				if i == 0 {
					prevSize = info.PaddedSize
				} else if info.PaddedSize > prevSize {
					return fmt.Errorf("subPieces must be in descending size")
				} else {
					prevSize = info.PaddedSize
				}

				subPieceCid, err := cid.Decode(sp.SubPieceCID)
				if err != nil {
					return xerrors.Errorf("invalid subPieceCid %s: %w", sp.SubPieceCID, err)
				}

				pieceInfos[i] = abi.PieceInfo{
					Size:     abi.PaddedPieceSize(info.PaddedSize),
					PieceCID: subPieceCid,
				}

				stagedRows = append(stagedRows, staged{PieceCIDv1: p.PieceCID, SubCIDv1: sp.SubPieceCID, Offset: totalOffset, Size: info.PaddedSize, RefID: info.RefID, MsgIdx: msgIdx})
				totalOffset += info.PaddedSize
				sum += info.RawSize
			}
			if sum != rawSize {
				return fmt.Errorf("raw size mismatch")
			}

			proofType := abi.RegisteredSealProof_StackedDrg64GiBV1_1
			generatedPieceCid, err := nonffi.GenerateUnsealedCID(proofType, pieceInfos)
			if err != nil {
				return xerrors.Errorf("failed to generate PieceCid: %w", err)
			}
			providedPieceCidv1, _, err := commcid.PieceCidV1FromV2(c)
			if err != nil {
				return xerrors.Errorf("invalid provided PieceCid: %w", err)
			}
			if !providedPieceCidv1.Equals(generatedPieceCid) {
				return fmt.Errorf("provided PieceCid does not match generated PieceCid: %s != %s", providedPieceCidv1, generatedPieceCid)
			}

			pieceDataArray = append(pieceDataArray, PieceData{Data: c.Bytes()})
		}

		abiData, err := contract.PDPVerifierMetaData.GetAbi()
		if err != nil {
			return err
		}

		extraDataBytes := []byte{}
		if req.Pieces.ExtraData != nil {
			extraDataHexStr := *req.Pieces.ExtraData
			decodedBytes, err := hex.DecodeString(strings.TrimPrefix(extraDataHexStr, "0x"))
			if err != nil {
				return xerrors.Errorf("failed to decode hex extraData: %w", err)
			}
			extraDataBytes = decodedBytes
		}

		data, err := abiData.Pack("addPieces", new(big.Int).SetUint64(dataSetId), pieceDataArray, extraDataBytes)
		if err != nil {
			return err
		}

		fromAddr, err := getSenderAddress(ctx, db)
		if err != nil {
			return err
		}
		tx := types.NewTransaction(0, contract.ContractAddresses().PDPVerifier, big.NewInt(0), 0, nil, data)
		txHash, err := sender.Send(ctx, fromAddr, tx, "pdp-addpieces")
		if err != nil {
			return err
		}

		txLower := strings.ToLower(txHash.Hex())
		_, err = db.BeginTransaction(ctx, func(txdb *harmonydb.Tx) (bool, error) {
			if _, err := txdb.Exec(`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, $2)`, txLower, "pending"); err != nil {
				return false, err
			}
			for _, r := range stagedRows {
				if _, err := txdb.Exec(`
                    INSERT INTO pdp_data_set_piece_adds (data_set, piece, add_message_hash, add_message_index, sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref)
                    VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
                `, dataSetId, r.PieceCIDv1, txLower, r.MsgIdx, r.SubCIDv1, r.Offset, r.Size, r.RefID); err != nil {
					return false, err
				}
			}
			return true, nil
		})
		if err != nil {
			return err
		}
		_, err = db.Exec(ctx, `DELETE FROM pdp_pending_piece_adds WHERE create_message_hash = $1`, psc.CreateMessageHash)
		if err != nil {
			log.Warnf("Failed to cleanup pending piece add intent for %s: %v", psc.CreateMessageHash, err)
		}
	}

	return nil
}

func getSenderAddress(ctx context.Context, db *harmonydb.DB) (common.Address, error) {
	var addressStr string
	if err := db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addressStr); err != nil {
		return common.Address{}, err
	}
	return common.HexToAddress(addressStr), nil
}

func extractDataSetIdFromReceipt(receipt *types.Receipt) (uint64, error) {
	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return 0, xerrors.Errorf("failed to get PDP ABI: %w", err)
	}

	event, exists := pdpABI.Events["DataSetCreated"]
	if !exists {
		return 0, xerrors.Errorf("DataSetCreated event not found in ABI")
	}

	for _, vLog := range receipt.Logs {
		if len(vLog.Topics) > 0 && vLog.Topics[0] == event.ID {
			if len(vLog.Topics) < 2 {
				return 0, xerrors.Errorf("log does not contain setId topic")
			}

			setIdBigInt := new(big.Int).SetBytes(vLog.Topics[1].Bytes())
			return setIdBigInt.Uint64(), nil
		}
	}

	return 0, xerrors.Errorf("DataSetCreated event not found in receipt")
}

func insertDataSet(ctx context.Context, db *harmonydb.DB, createMsg string, dataSetId uint64, service string, provingPeriod uint64, challengeWindow uint64) error {
	// Implement the insertion into pdp_data_sets table
	// Adjust the SQL statement based on your table schema
	_, err := db.Exec(ctx, `
        INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window)
        VALUES ($1, $2, $3, $4, $5)
    `, dataSetId, createMsg, service, provingPeriod, challengeWindow)
	return err
}

func getProvingPeriodChallengeWindow(ctx context.Context, ethClient *ethclient.Client, listenerAddr common.Address) (uint64, uint64, error) {
	// Get the proving schedule from the listener (handles view contract indirection)
	schedule, err := contract.GetProvingScheduleFromListener(listenerAddr, ethClient)
	if err != nil {
		return 0, 0, xerrors.Errorf("failed to get proving schedule from listener: %w", err)
	}

	config, err := schedule.GetPDPConfig(&bind.CallOpts{Context: ctx})
	if err != nil {
		return 0, 0, xerrors.Errorf("failed to GetPDPConfig: %w", err)
	}

	return config.MaxProvingPeriod, config.ChallengeWindow.Uint64(), nil
}
