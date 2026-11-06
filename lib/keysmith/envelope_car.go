package keysmith

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
)

const encryptedFeeName = "encrypted.fee"

// isCARPrefix reports whether b begins with a CARv1 or CARv2 header.
// The caller peeks enough bytes for a typical header; a truncated peek is false.
func isCARPrefix(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	_, err := carv2.NewBlockReader(bytes.NewReader(b))
	return err == nil
}

// labelsFromCAR reads a one-file metadata-first CAR and returns the FEE labels
// at the start of that file. The caller has established the stream begins with a CAR.
func labelsFromCAR(r io.Reader) (Labels, error) {
	br, err := carv2.NewBlockReader(r, carv2.ZeroLengthSectionAsEOF(true))
	if err != nil {
		return Labels{}, fmt.Errorf("%w: piece is not a CAR: %v", ErrInvalidEnvelope, err)
	}
	if len(br.Roots) != 1 {
		return Labels{}, fmt.Errorf("%w: CAR must have exactly one root", ErrInvalidEnvelope)
	}
	root := br.Roots[0]

	nodes := map[string][]byte{}
	structureBytes := 0
	var fileCID cid.Cid
	fileResolved := false
	var fileBuf []byte
	var leafOrder []cid.Cid
	leafIdx := 0

	if root.Prefix().Codec == cid.Raw {
		fileCID = root
		fileResolved = true
		leafOrder = []cid.Cid{root}
	}

	for {
		blk, err := br.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return Labels{}, fmt.Errorf("%w: reading CAR block: %v", ErrInvalidEnvelope, err)
		}
		c := blk.Cid()
		data := blk.RawData()

		switch c.Prefix().Codec {
		case cid.DagProtobuf:
			structureBytes += len(data)
			if structureBytes > maxCARStructureSize {
				return Labels{}, fmt.Errorf("%w: CAR metadata exceeds size limit", ErrInvalidEnvelope)
			}
			nodes[c.KeyString()] = data
			if !fileResolved {
				fc, ready, err := resolveCARFile(root, nodes)
				if err != nil {
					return Labels{}, err
				}
				if ready {
					fileCID = fc
					fileResolved = true
					inline, order, err := filePayloadPlan(fileCID, nodes)
					if err != nil {
						return Labels{}, err
					}
					leafOrder = order
					if len(inline) > 0 {
						return LabelsFromPrefix(inline)
					}
				}
			}
		case cid.Raw:
			if !fileResolved {
				return Labels{}, fmt.Errorf("%w: CAR is not metadata-first", ErrInvalidEnvelope)
			}
			nodes[c.KeyString()] = data
			for leafIdx < len(leafOrder) {
				want := leafOrder[leafIdx]
				raw, ok := nodes[want.KeyString()]
				if !ok {
					break
				}
				fileBuf = append(fileBuf, raw...)
				leafIdx++
				if len(fileBuf) > maxEnvelopeSize {
					return Labels{}, fmt.Errorf("%w: truncated envelope", ErrInvalidEnvelope)
				}
				labels, needMore, err := tryLabelsFromPrefix(fileBuf)
				if err != nil {
					return Labels{}, err
				}
				if !needMore {
					return labels, nil
				}
			}
		default:
			return Labels{}, fmt.Errorf("%w: unexpected CAR block codec", ErrInvalidEnvelope)
		}
	}

	if !fileResolved {
		if root.Prefix().Codec == cid.Raw {
			return Labels{}, fmt.Errorf("%w: CAR is missing the root block", ErrInvalidEnvelope)
		}
		return Labels{}, fmt.Errorf("%w: CAR is missing the UnixFS file", ErrInvalidEnvelope)
	}
	if len(leafOrder) == 0 {
		return Labels{}, fmt.Errorf("%w: UnixFS file has no payload", ErrInvalidEnvelope)
	}
	if leafIdx < len(leafOrder) {
		return Labels{}, fmt.Errorf("%w: CAR is missing file leaf blocks", ErrInvalidEnvelope)
	}
	return Labels{}, fmt.Errorf("%w: truncated envelope", ErrInvalidEnvelope)
}

// resolveCARFile finds the single UnixFS file under root.
// ready is false when a needed dag-pb node is not in nodes yet.
func resolveCARFile(root cid.Cid, nodes map[string][]byte) (cid.Cid, bool, error) {
	if root.Prefix().Codec == cid.Raw {
		return root, true, nil
	}
	if root.Prefix().Codec != cid.DagProtobuf {
		return cid.Undef, false, fmt.Errorf("%w: CAR root is not UnixFS", ErrInvalidEnvelope)
	}
	raw, ok := nodes[root.KeyString()]
	if !ok {
		return cid.Undef, false, nil
	}
	nd, err := merkledag.DecodeProtobuf(raw)
	if err != nil {
		return cid.Undef, false, fmt.Errorf("%w: CAR root is not dag-pb: %v", ErrInvalidEnvelope, err)
	}
	fsn, err := unixfs.FSNodeFromBytes(nd.Data())
	if err != nil {
		return cid.Undef, false, fmt.Errorf("%w: CAR root is not UnixFS: %v", ErrInvalidEnvelope, err)
	}
	switch fsn.Type() {
	case unixfs.TFile, unixfs.TRaw:
		return root, true, nil
	case unixfs.TDirectory:
		links := nd.Links()
		if len(links) != 1 {
			return cid.Undef, false, fmt.Errorf("%w: CAR directory must contain exactly one file", ErrInvalidEnvelope)
		}
		if links[0].Name != encryptedFeeName {
			return cid.Undef, false, fmt.Errorf("%w: CAR file must be named %s", ErrInvalidEnvelope, encryptedFeeName)
		}
		target := links[0].Cid
		if target.Prefix().Codec == cid.Raw {
			return target, true, nil
		}
		if target.Prefix().Codec != cid.DagProtobuf {
			return cid.Undef, false, fmt.Errorf("%w: %s is not a UnixFS file", ErrInvalidEnvelope, encryptedFeeName)
		}
		childRaw, ok := nodes[target.KeyString()]
		if !ok {
			return cid.Undef, false, nil
		}
		child, err := merkledag.DecodeProtobuf(childRaw)
		if err != nil {
			return cid.Undef, false, fmt.Errorf("%w: %s is not dag-pb: %v", ErrInvalidEnvelope, encryptedFeeName, err)
		}
		childFS, err := unixfs.FSNodeFromBytes(child.Data())
		if err != nil {
			return cid.Undef, false, fmt.Errorf("%w: %s is not UnixFS: %v", ErrInvalidEnvelope, encryptedFeeName, err)
		}
		switch childFS.Type() {
		case unixfs.TFile, unixfs.TRaw:
			return target, true, nil
		default:
			return cid.Undef, false, fmt.Errorf("%w: %s is not a file", ErrInvalidEnvelope, encryptedFeeName)
		}
	default:
		return cid.Undef, false, fmt.Errorf("%w: CAR root must be a file or a one-entry directory", ErrInvalidEnvelope)
	}
}

// filePayloadPlan returns inline file bytes when the node carries them, or the
// ordered raw-leaf CIDs that make up the file. The caller has resolved fileCID.
func filePayloadPlan(fileCID cid.Cid, nodes map[string][]byte) ([]byte, []cid.Cid, error) {
	if fileCID.Prefix().Codec == cid.Raw {
		raw, ok := nodes[fileCID.KeyString()]
		if !ok {
			return nil, []cid.Cid{fileCID}, nil
		}
		return raw, nil, nil
	}
	raw, ok := nodes[fileCID.KeyString()]
	if !ok {
		return nil, nil, fmt.Errorf("%w: CAR is missing the UnixFS file", ErrInvalidEnvelope)
	}
	nd, err := merkledag.DecodeProtobuf(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: file node is not dag-pb: %v", ErrInvalidEnvelope, err)
	}
	fsn, err := unixfs.FSNodeFromBytes(nd.Data())
	if err != nil {
		return nil, nil, fmt.Errorf("%w: file node is not UnixFS: %v", ErrInvalidEnvelope, err)
	}
	links := nd.Links()
	inline := fsn.Data()
	if len(links) == 0 {
		if len(inline) == 0 {
			return nil, nil, fmt.Errorf("%w: UnixFS file has no payload", ErrInvalidEnvelope)
		}
		return inline, nil, nil
	}
	order := make([]cid.Cid, 0, len(links))
	for _, l := range links {
		if l.Cid.Prefix().Codec != cid.Raw {
			return nil, nil, fmt.Errorf("%w: UnixFS file leaves must be raw", ErrInvalidEnvelope)
		}
		order = append(order, l.Cid)
	}
	return nil, order, nil
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
