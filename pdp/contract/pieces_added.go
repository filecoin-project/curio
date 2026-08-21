package contract

import (
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/core/types"
)

// AddedPiece is one piece from a PiecesAdded or PiecesAddedV2 receipt event,
// ordered by add-message index (0-based within the addPieces call).
type AddedPiece struct {
	PieceID uint64
	CID     []byte
}

// UnpackPackedCid reconstructs CID bytes from a PiecesAddedV2 packed CID.
// The header is right-aligned and zero-padded on the left; only leading zero
// bytes are stripped. Internal zeros are preserved, then root is appended.
func UnpackPackedCid(header, root [32]byte) []byte {
	start := 0
	for start < len(header) && header[start] == 0 {
		start++
	}
	out := make([]byte, 0, (32-start)+32)
	out = append(out, header[start:]...)
	out = append(out, root[:]...)
	return out
}

type piecesAddedV2Batch struct {
	firstPieceID uint64
	pieces       []AddedPiece
}

// PiecesFromReceipt extracts ordered pieces from a transaction receipt.
// If any PiecesAddedV2 logs are present, all of them are parsed (V1 ignored),
// sorted by firstPieceId, and flattened. Otherwise the legacy PiecesAdded
// event is used. Returns an error if neither event is found.
func PiecesFromReceipt(receipt *types.Receipt) ([]AddedPiece, error) {
	if receipt == nil {
		return nil, fmt.Errorf("receipt is nil")
	}

	pdpABI, err := PDPVerifierMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("failed to get PDP ABI: %w", err)
	}

	eventV2, hasV2 := pdpABI.Events["PiecesAddedV2"]
	eventV1, hasV1 := pdpABI.Events["PiecesAdded"]
	if !hasV2 && !hasV1 {
		return nil, fmt.Errorf("neither PiecesAdded nor PiecesAddedV2 found in ABI")
	}

	var v2Batches []piecesAddedV2Batch
	var v1Pieces []AddedPiece

	for _, vLog := range receipt.Logs {
		if vLog == nil || len(vLog.Topics) == 0 {
			continue
		}
		topic := vLog.Topics[0]

		if hasV2 && topic == eventV2.ID {
			batch, err := parsePiecesAddedV2Log(vLog)
			if err != nil {
				return nil, err
			}
			v2Batches = append(v2Batches, batch)
			continue
		}

		if hasV1 && topic == eventV1.ID {
			pieces, err := parsePiecesAddedV1Log(vLog)
			if err != nil {
				return nil, err
			}
			// Keep the first V1 event only (legacy watcher behavior).
			if v1Pieces == nil {
				v1Pieces = pieces
			}
		}
	}

	if len(v2Batches) > 0 {
		sort.Slice(v2Batches, func(i, j int) bool {
			return v2Batches[i].firstPieceID < v2Batches[j].firstPieceID
		})
		var out []AddedPiece
		for _, b := range v2Batches {
			out = append(out, b.pieces...)
		}
		return out, nil
	}

	if v1Pieces != nil {
		return v1Pieces, nil
	}

	return nil, fmt.Errorf("neither PiecesAdded nor PiecesAddedV2 event found in receipt")
}

func parsePiecesAddedV2Log(vLog *types.Log) (piecesAddedV2Batch, error) {
	// nil filterer b/c parsing does not use it. Don't call FilterLogs/WatchLogs though.
	parser, err := NewPDPVerifierFilterer(vLog.Address, nil)
	if err != nil {
		return piecesAddedV2Batch{}, fmt.Errorf("failed to create PDPVerifierFilterer: %w", err)
	}
	parsed, err := parser.ParsePiecesAddedV2(*vLog)
	if err != nil {
		return piecesAddedV2Batch{}, fmt.Errorf("failed to parse PiecesAddedV2: %w", err)
	}
	if parsed.FirstPieceId == nil {
		return piecesAddedV2Batch{}, fmt.Errorf("PiecesAddedV2 firstPieceId is nil")
	}
	first := parsed.FirstPieceId.Uint64()
	pieces := make([]AddedPiece, len(parsed.PieceCids))
	for i, packed := range parsed.PieceCids {
		pieces[i] = AddedPiece{
			PieceID: first + uint64(i),
			CID:     UnpackPackedCid(packed.Header, packed.Root),
		}
	}
	return piecesAddedV2Batch{firstPieceID: first, pieces: pieces}, nil
}

func parsePiecesAddedV1Log(vLog *types.Log) ([]AddedPiece, error) {
	parser, err := NewPDPVerifierFilterer(vLog.Address, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create PDPVerifierFilterer: %w", err)
	}
	parsed, err := parser.ParsePiecesAdded(*vLog)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PiecesAdded: %w", err)
	}
	if len(parsed.PieceIds) != len(parsed.PieceCids) {
		return nil, fmt.Errorf("PiecesAdded pieceIds/pieceCids length mismatch: %d vs %d",
			len(parsed.PieceIds), len(parsed.PieceCids))
	}
	pieces := make([]AddedPiece, len(parsed.PieceIds))
	for i := range parsed.PieceIds {
		if parsed.PieceIds[i] == nil {
			return nil, fmt.Errorf("PiecesAdded pieceId at index %d is nil", i)
		}
		pieces[i] = AddedPiece{
			PieceID: parsed.PieceIds[i].Uint64(),
			CID:     parsed.PieceCids[i].Data,
		}
	}
	return pieces, nil
}
