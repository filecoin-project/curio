package keysmith

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/fxamacker/cbor/v2"
	blockservice "github.com/ipfs/boxo/blockservice"
	bstore "github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	unixfsio "github.com/ipfs/boxo/ipld/unixfs/io"
	ds "github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	carv2 "github.com/ipld/go-car/v2"
)

// ErrInvalidEnvelope means the piece is not a CAR whose single file starts
// with a FEE envelope carrying the Keysmith labels a secure data set requires.
var ErrInvalidEnvelope = errors.New("invalid encryption envelope")

const (
	tagEncrypt0 = 16
	tagEncrypt  = 96

	labelAppMetadata = int64(-65792)

	encryptedFeeName = "encrypted.fee"
)

// Labels are the foc fields carried in a FEE envelope's protected app_metadata.
// Scope is optional; the other fields are required and Version is 1.
type Labels struct {
	Version  int64
	Owner    string
	Keyspace string
	Scope    string
	HasScope bool
	Salt     string
}

// Piece returns the labels a grant check compares against the envelope.
func (l Labels) Piece() PieceLabels {
	out := PieceLabels{Owner: l.Owner, Keyspace: l.Keyspace, Salt: l.Salt}
	if l.HasScope {
		scope := l.Scope
		out.Scope = &scope
	}
	return out
}

// ReadEnvelopeLabels reads the FEE envelope from r.
// The piece is a CAR with exactly one UnixFS file. When the root is a
// directory, that file is named encrypted.fee. The envelope is the first
// CBOR item in the file and is at most 1 MiB.
func ReadEnvelopeLabels(r io.Reader) (labels Labels, err error) {
	src := &prefixReader{r: r}
	// A disk error on the piece prefix replaces whatever envelope error the parse produced.
	defer func() {
		if src.ioErr != nil {
			labels, err = Labels{}, src.ioErr
		}
	}()

	ctx := context.Background()
	br, err := carv2.NewBlockReader(src, carv2.ZeroLengthSectionAsEOF(true))
	if err != nil {
		return Labels{}, fmt.Errorf("%w: piece is not a CAR: %v", ErrInvalidEnvelope, err)
	}
	if len(br.Roots) != 1 {
		return Labels{}, fmt.Errorf("%w: CAR must have exactly one root", ErrInvalidEnvelope)
	}
	bs := bstore.NewBlockstore(dssync.MutexWrap(ds.NewMapDatastore()))
	for {
		blk, err := br.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return Labels{}, fmt.Errorf("%w: reading CAR block: %v", ErrInvalidEnvelope, err)
		}
		if err := bs.Put(ctx, blk); err != nil {
			return Labels{}, err
		}
	}
	dag := merkledag.NewDAGService(blockservice.New(bs, offline.Exchange(bs)))
	root, err := dag.Get(ctx, br.Roots[0])
	if err != nil {
		return Labels{}, fmt.Errorf("%w: CAR is missing the root block: %v", ErrInvalidEnvelope, err)
	}

	file := root
	if pn, ok := root.(*merkledag.ProtoNode); ok {
		fsn, err := unixfs.FSNodeFromBytes(pn.Data())
		if err != nil {
			return Labels{}, fmt.Errorf("%w: CAR root is not UnixFS: %v", ErrInvalidEnvelope, err)
		}
		switch fsn.Type() {
		case unixfs.TFile, unixfs.TRaw:
		case unixfs.TDirectory:
			links := pn.Links()
			if len(links) != 1 {
				return Labels{}, fmt.Errorf("%w: CAR directory must contain exactly one file", ErrInvalidEnvelope)
			}
			if links[0].Name != encryptedFeeName {
				return Labels{}, fmt.Errorf("%w: CAR file must be named %s", ErrInvalidEnvelope, encryptedFeeName)
			}
			child, err := links[0].GetNode(ctx, dag)
			if err != nil {
				return Labels{}, fmt.Errorf("%w: CAR is missing the UnixFS file: %v", ErrInvalidEnvelope, err)
			}
			file = child
			if cpn, ok := child.(*merkledag.ProtoNode); ok {
				cfsn, err := unixfs.FSNodeFromBytes(cpn.Data())
				if err != nil {
					return Labels{}, fmt.Errorf("%w: %s is not UnixFS: %v", ErrInvalidEnvelope, encryptedFeeName, err)
				}
				switch cfsn.Type() {
				case unixfs.TFile, unixfs.TRaw:
				default:
					return Labels{}, fmt.Errorf("%w: %s is not a file", ErrInvalidEnvelope, encryptedFeeName)
				}
			}
		default:
			return Labels{}, fmt.Errorf("%w: CAR root must be a file or a one-entry directory", ErrInvalidEnvelope)
		}
	}

	dr, err := unixfsio.NewDagReader(ctx, file, dag)
	if err != nil {
		return Labels{}, fmt.Errorf("%w: UnixFS file has no payload: %v", ErrInvalidEnvelope, err)
	}
	defer func() { _ = dr.Close() }()
	fileReader := io.LimitReader(dr, int64(maxEnvelopeSize))
	var buf []byte
	tmp := make([]byte, 256)
	for {
		n, readErr := fileReader.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			var item cbor.RawMessage
			_, perr := decMode.UnmarshalFirst(buf, &item)
			if perr == nil {
				return labelsFromItem(item)
			}
			msg := perr.Error()
			truncated := errors.Is(perr, io.EOF) || errors.Is(perr, io.ErrUnexpectedEOF) ||
				bytes.Contains([]byte(msg), []byte("unexpected EOF")) ||
				bytes.Contains([]byte(msg), []byte("EOF"))
			if !truncated {
				return Labels{}, fmt.Errorf("%w: envelope is not CBOR: %v", ErrInvalidEnvelope, perr)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return Labels{}, fmt.Errorf("%w: truncated envelope", ErrInvalidEnvelope)
			}
			return Labels{}, readErr
		}
	}
}

// ReadEnvelopeLabelsAt reads the FEE envelope at offset 0 of r.
// It does not move a seek position on r.
func ReadEnvelopeLabelsAt(r io.ReaderAt) (Labels, error) {
	return ReadEnvelopeLabels(io.NewSectionReader(r, 0, math.MaxInt64))
}

// prefixReader remembers a read failure that is not an EOF, so a disk error
// stays distinct from a malformed envelope.
type prefixReader struct {
	r     io.Reader
	ioErr error
}

func (p *prefixReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && p.ioErr == nil {
		p.ioErr = fmt.Errorf("reading piece prefix: %w", err)
		return n, p.ioErr
	}
	return n, err
}

// LabelsFromPrefix parses a FEE envelope from b.
// b is the start of the file inside the piece CAR. Bytes after the envelope
// may be ciphertext and are not an error.
func LabelsFromPrefix(b []byte) (Labels, error) {
	if len(b) == 0 {
		return Labels{}, fmt.Errorf("%w: empty piece", ErrInvalidEnvelope)
	}
	var item cbor.RawMessage
	if _, err := decMode.UnmarshalFirst(b, &item); err != nil {
		return Labels{}, fmt.Errorf("%w: envelope is not CBOR: %v", ErrInvalidEnvelope, err)
	}
	return labelsFromItem(item)
}

// labelsFromItem parses one COSE encryption item. The caller has a complete item.
func labelsFromItem(item []byte) (Labels, error) {
	var tag cbor.RawTag
	if err := decMode.Unmarshal(item, &tag); err != nil || (tag.Number != tagEncrypt0 && tag.Number != tagEncrypt) {
		return Labels{}, fmt.Errorf("%w: envelope is not a COSE encryption tag", ErrInvalidEnvelope)
	}
	var fields []cbor.RawMessage
	if err := decMode.Unmarshal(tag.Content, &fields); err != nil || len(fields) < 3 {
		return Labels{}, fmt.Errorf("%w: envelope body is not a COSE array", ErrInvalidEnvelope)
	}
	var protected []byte
	if err := decMode.Unmarshal(fields[0], &protected); err != nil || len(protected) == 0 {
		return Labels{}, fmt.Errorf("%w: protected header is not a byte string", ErrInvalidEnvelope)
	}
	var header any
	if err := decMode.Unmarshal(protected, &header); err != nil {
		return Labels{}, fmt.Errorf("%w: protected header is not CBOR: %v", ErrInvalidEnvelope, err)
	}
	hdr, ok := asMap(header)
	if !ok {
		return Labels{}, fmt.Errorf("%w: protected header is not a map", ErrInvalidEnvelope)
	}
	rawMeta, ok := lookup(hdr, labelAppMetadata)
	if !ok {
		return Labels{}, fmt.Errorf("%w: app_metadata is missing", ErrInvalidEnvelope)
	}
	meta, ok := asMap(rawMeta)
	if !ok {
		return Labels{}, fmt.Errorf("%w: app_metadata is not a map", ErrInvalidEnvelope)
	}
	version, ok := meta["foc/v"]
	if !ok {
		return Labels{}, fmt.Errorf("%w: foc/v is missing", ErrInvalidEnvelope)
	}
	v, ok := asUint(version)
	if !ok || v != 1 {
		return Labels{}, fmt.Errorf("%w: foc/v must be 1", ErrInvalidEnvelope)
	}
	ownerRaw, ok := meta["foc/own"]
	if !ok {
		return Labels{}, fmt.Errorf("%w: foc/own is missing", ErrInvalidEnvelope)
	}
	owner, ok := ownerRaw.(string)
	if !ok || owner == "" {
		return Labels{}, fmt.Errorf("%w: foc/own must be a non-empty string", ErrInvalidEnvelope)
	}
	keyspaceRaw, ok := meta["foc/ks"]
	if !ok {
		return Labels{}, fmt.Errorf("%w: foc/ks is missing", ErrInvalidEnvelope)
	}
	keyspace, ok := keyspaceRaw.(string)
	if !ok || keyspace == "" {
		return Labels{}, fmt.Errorf("%w: foc/ks must be a non-empty string", ErrInvalidEnvelope)
	}
	saltRaw, ok := meta["foc/salt"]
	if !ok {
		return Labels{}, fmt.Errorf("%w: foc/salt is missing", ErrInvalidEnvelope)
	}
	salt, ok := saltRaw.(string)
	if !ok || salt == "" {
		return Labels{}, fmt.Errorf("%w: foc/salt must be a non-empty string", ErrInvalidEnvelope)
	}
	out := Labels{Version: 1, Owner: owner, Keyspace: keyspace, Salt: salt}
	if raw, ok := meta["foc/scope"]; ok {
		scope, ok := raw.(string)
		if !ok || scope == "" {
			return Labels{}, fmt.Errorf("%w: foc/scope must be a non-empty string", ErrInvalidEnvelope)
		}
		out.Scope = scope
		out.HasScope = true
	}
	return out, nil
}
