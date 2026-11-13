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
	ipld "github.com/ipfs/go-ipld-format"
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
func ReadEnvelopeLabels(r io.Reader) (Labels, error) {
	src := &prefixReader{r: r}
	labels, err := labelsFromCAR(src)
	if src.ioErr != nil {
		return Labels{}, src.ioErr
	}
	return labels, err
}

// ReadEnvelopeLabelsAt reads the FEE envelope at offset 0 of r.
// It does not move a seek position on r.
func ReadEnvelopeLabelsAt(r io.ReaderAt) (Labels, error) {
	return ReadEnvelopeLabels(io.NewSectionReader(r, 0, math.MaxInt64))
}

// labelsFromCAR reads the FEE at the start of the single UnixFS file in a CAR.
// The caller has the piece bytes. A stream that is not a CAR is an invalid envelope.
func labelsFromCAR(r io.Reader) (Labels, error) {
	ctx := context.Background()
	br, err := carv2.NewBlockReader(r, carv2.ZeroLengthSectionAsEOF(true))
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
	file, err := feeFileNode(ctx, dag, root)
	if err != nil {
		return Labels{}, err
	}
	dr, err := unixfsio.NewDagReader(ctx, file, dag)
	if err != nil {
		return Labels{}, fmt.Errorf("%w: UnixFS file has no payload: %v", ErrInvalidEnvelope, err)
	}
	defer func() { _ = dr.Close() }()
	return labelsFromReader(io.LimitReader(dr, int64(maxEnvelopeSize)))
}

// feeFileNode returns the UnixFS file the envelope is read from.
// The caller has loaded the CAR, and nd is its root.
func feeFileNode(ctx context.Context, dag ipld.NodeGetter, nd ipld.Node) (ipld.Node, error) {
	pn, ok := nd.(*merkledag.ProtoNode)
	if !ok {
		return nd, nil
	}
	fsn, err := unixfs.FSNodeFromBytes(pn.Data())
	if err != nil {
		return nil, fmt.Errorf("%w: CAR root is not UnixFS: %v", ErrInvalidEnvelope, err)
	}
	switch fsn.Type() {
	case unixfs.TFile, unixfs.TRaw:
		return nd, nil
	case unixfs.TDirectory:
		links := pn.Links()
		if len(links) != 1 {
			return nil, fmt.Errorf("%w: CAR directory must contain exactly one file", ErrInvalidEnvelope)
		}
		if links[0].Name != encryptedFeeName {
			return nil, fmt.Errorf("%w: CAR file must be named %s", ErrInvalidEnvelope, encryptedFeeName)
		}
		child, err := links[0].GetNode(ctx, dag)
		if err != nil {
			return nil, fmt.Errorf("%w: CAR is missing the UnixFS file: %v", ErrInvalidEnvelope, err)
		}
		return unixfsFile(child)
	default:
		return nil, fmt.Errorf("%w: CAR root must be a file or a one-entry directory", ErrInvalidEnvelope)
	}
}

// unixfsFile checks that nd is a UnixFS file.
// The caller has resolved the encrypted.fee link.
func unixfsFile(nd ipld.Node) (ipld.Node, error) {
	pn, ok := nd.(*merkledag.ProtoNode)
	if !ok {
		return nd, nil
	}
	fsn, err := unixfs.FSNodeFromBytes(pn.Data())
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not UnixFS: %v", ErrInvalidEnvelope, encryptedFeeName, err)
	}
	switch fsn.Type() {
	case unixfs.TFile, unixfs.TRaw:
		return nd, nil
	default:
		return nil, fmt.Errorf("%w: %s is not a file", ErrInvalidEnvelope, encryptedFeeName)
	}
}

// labelsFromReader parses the FEE at the start of a UnixFS file.
// The caller has the file reader. Bytes after the CBOR item are ciphertext.
func labelsFromReader(r io.Reader) (Labels, error) {
	var buf []byte
	tmp := make([]byte, 256)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			labels, needMore, perr := tryLabelsFromPrefix(buf)
			if perr != nil {
				return Labels{}, perr
			}
			if !needMore {
				return labels, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Labels{}, fmt.Errorf("%w: truncated envelope", ErrInvalidEnvelope)
			}
			return Labels{}, err
		}
	}
}

// tryLabelsFromPrefix parses a FEE envelope from b.
// needMore is true when b is a truncated CBOR item.
func tryLabelsFromPrefix(b []byte) (Labels, bool, error) {
	if len(b) == 0 {
		return Labels{}, true, nil
	}
	var item cbor.RawMessage
	_, err := decMode.UnmarshalFirst(b, &item)
	if err != nil {
		if isTruncatedCBOR(err) {
			return Labels{}, true, nil
		}
		return Labels{}, false, fmt.Errorf("%w: envelope is not CBOR: %v", ErrInvalidEnvelope, err)
	}
	labels, err := labelsFromItem(item)
	return labels, false, err
}

// isTruncatedCBOR reports whether err means the CBOR item is incomplete.
// A nil error is not truncation.
func isTruncatedCBOR(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := err.Error()
	return bytes.Contains([]byte(msg), []byte("unexpected EOF")) ||
		bytes.Contains([]byte(msg), []byte("EOF"))
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
	return labelsFromMetadata(meta)
}

// labelsFromMetadata requires foc/v, foc/own, foc/ks, and foc/salt. foc/scope may be absent.
func labelsFromMetadata(meta map[any]any) (Labels, error) {
	version, ok := meta["foc/v"]
	if !ok {
		return Labels{}, fmt.Errorf("%w: foc/v is missing", ErrInvalidEnvelope)
	}
	v, ok := asUint(version)
	if !ok || v != 1 {
		return Labels{}, fmt.Errorf("%w: foc/v must be 1", ErrInvalidEnvelope)
	}
	owner, err := requiredText(meta, "foc/own")
	if err != nil {
		return Labels{}, err
	}
	keyspace, err := requiredText(meta, "foc/ks")
	if err != nil {
		return Labels{}, err
	}
	salt, err := requiredText(meta, "foc/salt")
	if err != nil {
		return Labels{}, err
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

func requiredText(meta map[any]any, key string) (string, error) {
	raw, ok := meta[key]
	if !ok {
		return "", fmt.Errorf("%w: %s is missing", ErrInvalidEnvelope, key)
	}
	text, ok := raw.(string)
	if !ok || text == "" {
		return "", fmt.Errorf("%w: %s must be a non-empty string", ErrInvalidEnvelope, key)
	}
	return text, nil
}
