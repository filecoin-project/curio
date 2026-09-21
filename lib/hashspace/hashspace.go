// Package hashspace stores piece files in two CID-hash namespaces,
// open-pieces and acl-pieces. Each namespace implements HashSpace.
// layout.json at each space root records that folder's used bytes, the
// "2,-" split, and the hash ranges the folder owns.
//
// FirstSetup assigns ranges by presenting every drive to hashspacesolver.
// WriteCID creates a file only when that CID is not already stored.
// lib/paths does not call this package.
package hashspace

import (
	"encoding/hex"
	"io"
	"path/filepath"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/commcidv2"
)

const (
	// SPLIT is the layout.json split. The first two novel characters are the
	// directory and the remainder is the file name, so ab/cdefg hashes as abcdefg.
	SPLIT = "2,-"

	// DIR_OPEN and DIR_ACL are the space folders on each storage root.
	DIR_OPEN = "open-pieces"
	DIR_ACL  = "acl-pieces"

	// HASH_BYTES is the piece-commitment width used for layout ranges.
	HASH_BYTES = 32

	// FLUSH_INTERVAL is how often a loaded space rewrites layout.json.
	// It matches the storage heartbeat cadence.
	FLUSH_INTERVAL = 10 * time.Second

	layoutFile      = "layout.json"
	sectorStoreFile = "sectorstore.json"
)

// HashSpace is the only caller-facing surface for one namespace.
type HashSpace interface {
	WriteCID(c cid.Cid) (io.WriteCloser, error)
	DeleteCID(c cid.Cid) error
	ReadCIDFileFrom(c cid.Cid) (ReadSeekFile, error)
}

// ReadSeekFile is the CID file returned by ReadCIDFileFrom.
type ReadSeekFile interface {
	io.Reader
	io.Seeker
	io.Closer
}

// Drive is one attached storage root. Capacity, when set, is the solver
// disk size in bytes. When zero, capacity is the filesystem size from
// statfs, capped by sectorstore.json MaxStorage when that field is non-zero.
type Drive struct {
	Root     string
	Capacity int64
}

func novelOf(c cid.Cid) (string, []byte, error) {
	digest, err := commitmentOf(c)
	if err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(digest), digest, nil
}

func commitmentOf(c cid.Cid) ([]byte, error) {
	if !c.Defined() {
		return nil, xerrors.Errorf("undefined piece cid")
	}
	if commcidv2.IsPieceCidV2(c) {
		v1, _, err := commcid.PieceCidV1FromV2(c)
		if err != nil {
			return nil, xerrors.Errorf("piece cid v2: %w", err)
		}
		c = v1
	}
	digest, err := commcid.CIDToPieceCommitmentV1(c)
	if err != nil {
		var dataErr error
		digest, dataErr = commcid.CIDToDataCommitmentV1(c)
		if dataErr != nil {
			return nil, xerrors.Errorf("piece commitment: %w", err)
		}
	}
	if len(digest) != HASH_BYTES {
		return nil, xerrors.Errorf("piece commitment is %d bytes", len(digest))
	}
	out := make([]byte, HASH_BYTES)
	copy(out, digest)
	return out, nil
}

func piecePath(root, kind, novel string) (string, error) {
	if len(novel) < 3 {
		return "", xerrors.Errorf("hash %q is shorter than split %s", novel, SPLIT)
	}
	return filepath.Join(root, kind, novel[:2], novel[2:]), nil
}

func decodeHash(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, xerrors.Errorf("decode hash: %w", err)
	}
	if len(b) != HASH_BYTES {
		return nil, xerrors.Errorf("hash is %d bytes, want %d", len(b), HASH_BYTES)
	}
	return b, nil
}
