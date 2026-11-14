package remoteblockstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

// spaceProbe records every look at one hash space.
type spaceProbe struct {
	files map[cid.Cid][]byte
	calls []string
}

func (s *spaceProbe) HasPiece(context.Context, cid.Cid) (bool, error) {
	s.calls = append(s.calls, "has")
	return false, nil
}

func (s *spaceProbe) ReadPiece(_ context.Context, pc cid.Cid, _ int64) (storiface.Reader, error) {
	s.calls = append(s.calls, "read")
	data, ok := s.files[pc]
	if !ok {
		return nil, nil
	}
	return &spaceFile{Reader: bytes.NewReader(data)}, nil
}

type spaceFile struct {
	*bytes.Reader
}

func (f *spaceFile) Close() error { return nil }

type onePieceIdx struct {
	piece     cid.Cid
	block     cid.Cid
	blockSize uint64
}

func (i *onePieceIdx) PiecesContainingMultihash(_ context.Context, m multihash.Multihash) ([]indexstore.PieceInfo, error) {
	if !bytes.Equal(m, i.block.Hash()) {
		return nil, nil
	}
	return []indexstore.PieceInfo{{PieceCid: i.piece, BlockSize: i.blockSize}}, nil
}

func (i *onePieceIdx) GetOffset(context.Context, cid.Cid, multihash.Multihash) (uint64, error) {
	return 0, nil
}

// carSection is one CARv1 block section: uvarint length, CID, data.
func carSection(c cid.Cid, data []byte) []byte {
	body := append(c.Bytes(), data...)
	buf := make([]byte, binary.MaxVarintLen64+len(body))
	n := binary.PutUvarint(buf, uint64(len(body)))
	n += copy(buf[n:], body)
	return buf[:n]
}

func TestGetDoesNotReadACLPieces(t *testing.T) {
	ctx := context.Background()
	payload := []byte("open-block")
	sum, err := multihash.Sum(payload, multihash.SHA2_256, -1)
	require.NoError(t, err)
	block := cid.NewCidV1(cid.Raw, sum)
	v1, err := cid.Decode("baga6ea4seaqomqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)
	piece, err := commcid.PieceCidV2FromV1(v1, 127)
	require.NoError(t, err)
	section := carSection(block, payload)

	open := &spaceProbe{files: map[cid.Cid][]byte{piece: section}}
	acl := &spaceProbe{files: map[cid.Cid][]byte{piece: carSection(block, []byte("acl-block"))}}
	cpr := cachedreader.NewCachedPieceReader(nil, nil, nil, nil)
	cpr.SetOpenPieceReader(open)
	cpr.SetACLPieceReader(acl)

	bs := NewRemoteBlockstore(&onePieceIdx{piece: piece, block: block, blockSize: uint64(len(payload))}, nil, cpr)
	got, err := bs.Get(ctx, block)
	require.NoError(t, err)
	require.Equal(t, payload, got.RawData())
	require.Empty(t, acl.calls)
	require.NotEmpty(t, open.calls)

	want, err := blocks.NewBlockWithCid(payload, block)
	require.NoError(t, err)
	require.True(t, got.Cid().Equals(want.Cid()))
}
