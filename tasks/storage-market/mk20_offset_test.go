package storage_market

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
)

func TestFindMK20PieceOffsetLayouts(t *testing.T) {
	pieceCIDs := []string{
		syntheticPieceCID(t, 1),
		syntheticPieceCID(t, 2),
		syntheticPieceCID(t, 3),
	}

	tests := []struct {
		name    string
		sizes   []abi.PaddedPieceSize
		offsets []abi.PaddedPieceSize
	}{
		{
			name:    "single piece",
			sizes:   []abi.PaddedPieceSize{512},
			offsets: []abi.PaddedPieceSize{0},
		},
		{
			name:    "distinct descending sizes",
			sizes:   []abi.PaddedPieceSize{512, 256, 128},
			offsets: []abi.PaddedPieceSize{0, 512, 768},
		},
		{
			name:    "equal sizes with distinct CIDs",
			sizes:   []abi.PaddedPieceSize{256, 256, 256},
			offsets: []abi.PaddedPieceSize{0, 256, 512},
		},
		{
			// This deliberately exercises arithmetic alignment; it does not model
			// the normal transfer function's piece ordering.
			name:    "arithmetic alignment fixture",
			sizes:   []abi.PaddedPieceSize{128, 256, 128},
			offsets: []abi.PaddedPieceSize{0, 256, 512},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pieces := make([]mk20SectorPiece, len(test.sizes))
			for index, size := range test.sizes {
				pieces[index] = mk20SectorPiece{
					CID:   pieceCIDs[index],
					Size:  size,
					Index: int64(index),
				}
			}
			original := append([]mk20SectorPiece(nil), pieces...)

			for index, piece := range pieces {
				offset, found := findMK20PieceOffset(pieces, piece.CID, piece.Size)
				require.True(t, found)
				require.Equal(t, test.offsets[index], offset)
			}

			require.Equal(t, original, pieces, "offset calculation must not mutate or reorder its input")
		})
	}
}

func TestFindMK20PieceOffsetMatchesCIDAndSize(t *testing.T) {
	sharedCID := syntheticPieceCID(t, 4)
	pieces := []mk20SectorPiece{
		{CID: sharedCID, Size: 128, Index: 0},
		{CID: sharedCID, Size: 256, Index: 1},
	}

	offset, found := findMK20PieceOffset(pieces, sharedCID, 256)
	require.True(t, found)
	require.Equal(t, abi.PaddedPieceSize(256), offset)

	_, found = findMK20PieceOffset(pieces, syntheticPieceCID(t, 5), 256)
	require.False(t, found, "matching size without matching CID must not succeed")

	_, found = findMK20PieceOffset(pieces, sharedCID, 512)
	require.False(t, found, "matching CID without matching size must not succeed")
}

func TestFindMK20PieceOffsetMissingAndEmpty(t *testing.T) {
	pieceCID := syntheticPieceCID(t, 6)
	pieces := []mk20SectorPiece{{CID: pieceCID, Size: 128, Index: 0}}

	offset, found := findMK20PieceOffset(pieces, syntheticPieceCID(t, 7), 128)
	require.False(t, found)
	require.Zero(t, offset, "a missing target must not be reported as a successful zero offset")

	offset, found = findMK20PieceOffset(nil, pieceCID, 128)
	require.False(t, found)
	require.Zero(t, offset)
}

func TestAddDealOffsetUsesTestedCalculation(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	mk20File := filepath.Join(filepath.Dir(testFile), "mk20.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), mk20File, nil, 0)
	require.NoError(t, err)

	var addDealOffset *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "addDealOffset" {
			addDealOffset = function
			break
		}
	}
	require.NotNil(t, addDealOffset)

	helperCalls := 0
	paddingCalls := 0
	orderedPieceQuery := false
	offsetUpdateCalls := 0
	offsetUpdateUsesHelperResult := false
	ast.Inspect(addDealOffset.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, argument := range call.Args {
			literal, ok := argument.(*ast.BasicLit)
			if ok && strings.Contains(literal.Value, "ORDER BY piece_index ASC") {
				orderedPieceQuery = true
			}
		}
		if function, ok := call.Fun.(*ast.Ident); ok && function.Name == "findMK20PieceOffset" {
			helperCalls++
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
			if selector.Sel.Name == "GetRequiredPadding" {
				paddingCalls++
			}
			if selector.Sel.Name == "Exec" && len(call.Args) > 1 {
				query, ok := call.Args[0].(*ast.BasicLit)
				if ok && strings.Contains(query.Value, "SET sector_offset = $1") {
					offsetUpdateCalls++
					argument, ok := call.Args[1].(*ast.Ident)
					offsetUpdateUsesHelperResult = ok && argument.Name == "offset"
				}
			}
		}
		return true
	})

	require.Equal(t, 1, helperCalls, "addDealOffset must use the tested offset calculation")
	require.Zero(t, paddingCalls, "addDealOffset must not retain a separate padding loop")
	require.True(t, orderedPieceQuery, "addDealOffset must preserve authoritative piece_index order")
	require.Equal(t, 1, offsetUpdateCalls, "addDealOffset must retain one conditional offset update")
	require.True(t, offsetUpdateUsesHelperResult, "addDealOffset must persist the tested helper result")
}

func syntheticPieceCID(t *testing.T, marker byte) string {
	t.Helper()

	pieceCID, err := commcid.PieceCommitmentV1ToCID(bytes.Repeat([]byte{marker}, 32))
	require.NoError(t, err)
	return pieceCID.String()
}
