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
	"errors"
	"io"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/commcidv2"
)

const (
	// SPLIT is the layout.json split. The first two hex characters of the CID
	// hash are the directory and the rest is the file name: abcdefg is ab/cdefg.
	SPLIT = "2,-"

	// DIR_OPEN and DIR_ACL are the space folders on each storage root.
	DIR_OPEN = "open-pieces"
	DIR_ACL  = "acl-pieces"

	// HASH_BYTES is the CID hash width used for layout ranges.
	HASH_BYTES = 32

	// FLUSH_INTERVAL is how often a loaded space rewrites layout.json.
	// It matches the storage heartbeat cadence.
	FLUSH_INTERVAL = 10 * time.Second

	layoutFile      = "layout.json"
	sectorStoreFile = "sectorstore.json"
)

// ErrCrossDevice reports that a rename would cross filesystems, so the bytes
// must be copied instead.
var ErrCrossDevice = errors.New("rename crosses filesystems")

func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

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

// Drive is one attached storage root. StorageID is the storage path ID from
// sectorstore.json and the database; it names the drive's mountpoint in the
// solver. Capacity, when set, is the solver disk size in bytes. When zero,
// capacity is the filesystem size from statfs, capped by sectorstore.json
// MaxStorage when that field is non-zero.
type Drive struct {
	StorageID string
	Root      string
	Capacity  int64
}

func cidHashHex(c cid.Cid) (string, []byte, error) {
	digest, err := CIDHash(c)
	if err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(digest), digest, nil
}

// CIDHash is the tree root carried by piece CID v2 c, which places c on the
// hash circle and names its file. Hash spaces are keyed by piece CID v2 only.
func CIDHash(c cid.Cid) ([]byte, error) {
	if !c.Defined() {
		return nil, xerrors.Errorf("undefined piece cid")
	}
	if !commcidv2.IsPieceCidV2(c) {
		return nil, xerrors.Errorf("hash spaces take piece cid v2, got %s", c)
	}
	digest, _, err := commcid.PieceCidV2ToDataCommitment(c)
	if err != nil {
		return nil, xerrors.Errorf("cid hash: %w", err)
	}
	if len(digest) != HASH_BYTES {
		return nil, xerrors.Errorf("cid hash is %d bytes", len(digest))
	}
	out := make([]byte, HASH_BYTES)
	copy(out, digest)
	return out, nil
}

func piecePath(root, kind, hexHash string) (string, error) {
	if len(hexHash) < 3 {
		return "", xerrors.Errorf("hash %q is shorter than split %s", hexHash, SPLIT)
	}
	return filepath.Join(root, kind, hexHash[:2], hexHash[2:]), nil
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
