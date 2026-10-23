package keysmith

import (
	"errors"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
)

// ErrInvalidEnvelope means the piece prefix is not a FEE envelope with the
// Keysmith labels a secure data set requires.
var ErrInvalidEnvelope = errors.New("invalid encryption envelope")

const (
	tagEncrypt0 = 16
	tagEncrypt  = 96

	labelAppMetadata = int64(-65792)
)

// Labels are the foc fields carried in a FEE envelope's protected app_metadata.
// Groups is optional; the other fields are required and Version is 1.
type Labels struct {
	Version   int64
	Owner     string
	GroupKey  string
	Groups    string
	HasGroups bool
	Salt      string
}

// Piece returns the labels a grant check compares against the envelope.
func (l Labels) Piece() PieceLabels {
	out := PieceLabels{Owner: l.Owner, GroupKey: l.GroupKey, Salt: l.Salt}
	if l.HasGroups {
		groups := l.Groups
		out.Groups = &groups
	}
	return out
}

// ReadEnvelopeLabels reads the FEE envelope at the front of r.
// The decoder stops at the end of the CBOR item. Ciphertext after it is not read.
// The item is at most 1 MiB.
func ReadEnvelopeLabels(r io.Reader) (Labels, error) {
	src := &prefixReader{r: r}
	dec := decMode.NewDecoder(io.LimitReader(src, int64(maxEnvelopeSize)))
	var item cbor.RawMessage
	err := dec.Decode(&item)
	if err != nil {
		if src.ioErr != nil {
			return Labels{}, src.ioErr
		}
		if errors.Is(err, io.EOF) {
			return Labels{}, fmt.Errorf("%w: empty piece", ErrInvalidEnvelope)
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Labels{}, fmt.Errorf("%w: truncated envelope", ErrInvalidEnvelope)
		}
		return Labels{}, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	return labelsFromItem(item)
}

// ReadEnvelopeLabelsAt reads the FEE envelope at offset 0 of r.
// It does not move a seek position on r.
func ReadEnvelopeLabelsAt(r io.ReaderAt) (Labels, error) {
	return ReadEnvelopeLabels(io.NewSectionReader(r, 0, int64(maxEnvelopeSize)))
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

// LabelsFromPrefix parses a FEE envelope from the leading bytes of a piece.
// Bytes after the envelope may be ciphertext and are not an error.
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

// labelsFromMetadata requires foc/v, foc/own, foc/groupKey, and foc/salt. foc/groups may be absent.
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
	groupKey, err := requiredText(meta, "foc/groupKey")
	if err != nil {
		return Labels{}, err
	}
	salt, err := requiredText(meta, "foc/salt")
	if err != nil {
		return Labels{}, err
	}
	out := Labels{Version: 1, Owner: owner, GroupKey: groupKey, Salt: salt}
	if raw, ok := meta["foc/groups"]; ok {
		groups, ok := raw.(string)
		if !ok || groups == "" {
			return Labels{}, fmt.Errorf("%w: foc/groups must be a non-empty string", ErrInvalidEnvelope)
		}
		out.Groups = groups
		out.HasGroups = true
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
