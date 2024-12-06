package ipniculib

import (
	"bufio"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime/datamodel"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-varint"
	"golang.org/x/xerrors"
)

var (
	EntryLinkproto = cidlink.LinkPrototype{
		Prefix: cid.Prefix{
			Version:  1,
			Codec:    uint64(multicodec.DagCbor),
			MhType:   uint64(multicodec.Sha2_256),
			MhLength: -1,
		},
	}
)

func NodeToLink(node datamodel.Node, lp datamodel.LinkPrototype) (datamodel.Link, error) {
	linkSystem := cidlink.DefaultLinkSystem()
	return linkSystem.ComputeLink(lp, node)
}

// SkipCarNode is a specialized version of carv2.SkipCarNode that skips the car node and returns the CID of the skipped node.
// Unlike the carv2 version this version never seeks with the assumption that it always operates on dense car files.
// It also does not need to know the absolute offset in the car file, only needs the reader to start at the beginning of a car entry.
func SkipCarNode(br *bufio.Reader) (cid.Cid, error) {
	sectionSize, err := LdReadSize(br, true, 32<<20)
	if err != nil {
		return cid.Undef, err
	}

	cidSize, c, err := cid.CidFromReader(io.LimitReader(br, int64(sectionSize)))
	if err != nil {
		return cid.Undef, err
	}

	blockSize := sectionSize - uint64(cidSize)
	readCnt, err := io.CopyN(io.Discard, br, int64(blockSize))
	if err != nil {
		if err == io.EOF {
			return cid.Undef, io.ErrUnexpectedEOF
		}
		return cid.Undef, err
	}

	if readCnt != int64(blockSize) {
		return cid.Undef, xerrors.New("unexpected length")
	}

	return c, nil
}

func LdReadSize(r *bufio.Reader, zeroLenAsEOF bool, maxReadBytes uint64) (uint64, error) {
	l, err := varint.ReadUvarint(r)
	if err != nil {
		// If the length of bytes read is non-zero when the error is EOF then signal an unclean EOF.
		if l > 0 && err == io.EOF {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	} else if l == 0 && zeroLenAsEOF {
		return 0, io.EOF
	}

	if l > maxReadBytes { // Don't OOM
		return 0, xerrors.Errorf("section size %d exceeds maximum allowed size %d", l, maxReadBytes)
	}
	return l, nil
}
