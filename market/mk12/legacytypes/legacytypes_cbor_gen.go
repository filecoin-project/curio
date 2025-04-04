// Code generated by github.com/whyrusleeping/cbor-gen. DO NOT EDIT.

package legacytypes

import (
	"fmt"
	"io"
	"math"
	"sort"

	abi "github.com/filecoin-project/go-state-types/abi"
	crypto "github.com/filecoin-project/go-state-types/crypto"
	cid "github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf
var _ = cid.Undef
var _ = math.E
var _ = sort.Sort

func (t *SignedStorageAsk) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{162}); err != nil {
		return err
	}

	// t.Ask (legacytypes.StorageAsk) (struct)
	if len("Ask") > 8192 {
		return xerrors.Errorf("Value in field \"Ask\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Ask"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Ask")); err != nil {
		return err
	}

	if err := t.Ask.MarshalCBOR(cw); err != nil {
		return err
	}

	// t.Signature (crypto.Signature) (struct)
	if len("Signature") > 8192 {
		return xerrors.Errorf("Value in field \"Signature\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Signature"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Signature")); err != nil {
		return err
	}

	if err := t.Signature.MarshalCBOR(cw); err != nil {
		return err
	}
	return nil
}

func (t *SignedStorageAsk) UnmarshalCBOR(r io.Reader) (err error) {
	*t = SignedStorageAsk{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("SignedStorageAsk: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 9)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 8192)
		if err != nil {
			return err
		}

		if !ok {
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		// t.Ask (legacytypes.StorageAsk) (struct)
		case "Ask":

			{

				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}
					t.Ask = new(StorageAsk)
					if err := t.Ask.UnmarshalCBOR(cr); err != nil {
						return xerrors.Errorf("unmarshaling t.Ask pointer: %w", err)
					}
				}

			}
			// t.Signature (crypto.Signature) (struct)
		case "Signature":

			{

				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}
					t.Signature = new(crypto.Signature)
					if err := t.Signature.UnmarshalCBOR(cr); err != nil {
						return xerrors.Errorf("unmarshaling t.Signature pointer: %w", err)
					}
				}

			}

		default:
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
func (t *StorageAsk) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{168}); err != nil {
		return err
	}

	// t.Miner (address.Address) (struct)
	if len("Miner") > 8192 {
		return xerrors.Errorf("Value in field \"Miner\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Miner"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Miner")); err != nil {
		return err
	}

	if err := t.Miner.MarshalCBOR(cw); err != nil {
		return err
	}

	// t.Price (big.Int) (struct)
	if len("Price") > 8192 {
		return xerrors.Errorf("Value in field \"Price\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Price"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Price")); err != nil {
		return err
	}

	if err := t.Price.MarshalCBOR(cw); err != nil {
		return err
	}

	// t.SeqNo (uint64) (uint64)
	if len("SeqNo") > 8192 {
		return xerrors.Errorf("Value in field \"SeqNo\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("SeqNo"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("SeqNo")); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.SeqNo)); err != nil {
		return err
	}

	// t.Expiry (abi.ChainEpoch) (int64)
	if len("Expiry") > 8192 {
		return xerrors.Errorf("Value in field \"Expiry\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Expiry"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Expiry")); err != nil {
		return err
	}

	if t.Expiry >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Expiry)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Expiry-1)); err != nil {
			return err
		}
	}

	// t.Timestamp (abi.ChainEpoch) (int64)
	if len("Timestamp") > 8192 {
		return xerrors.Errorf("Value in field \"Timestamp\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Timestamp"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Timestamp")); err != nil {
		return err
	}

	if t.Timestamp >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Timestamp)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Timestamp-1)); err != nil {
			return err
		}
	}

	// t.MaxPieceSize (abi.PaddedPieceSize) (uint64)
	if len("MaxPieceSize") > 8192 {
		return xerrors.Errorf("Value in field \"MaxPieceSize\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("MaxPieceSize"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("MaxPieceSize")); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.MaxPieceSize)); err != nil {
		return err
	}

	// t.MinPieceSize (abi.PaddedPieceSize) (uint64)
	if len("MinPieceSize") > 8192 {
		return xerrors.Errorf("Value in field \"MinPieceSize\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("MinPieceSize"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("MinPieceSize")); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.MinPieceSize)); err != nil {
		return err
	}

	// t.VerifiedPrice (big.Int) (struct)
	if len("VerifiedPrice") > 8192 {
		return xerrors.Errorf("Value in field \"VerifiedPrice\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("VerifiedPrice"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("VerifiedPrice")); err != nil {
		return err
	}

	if err := t.VerifiedPrice.MarshalCBOR(cw); err != nil {
		return err
	}
	return nil
}

func (t *StorageAsk) UnmarshalCBOR(r io.Reader) (err error) {
	*t = StorageAsk{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("StorageAsk: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 13)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 8192)
		if err != nil {
			return err
		}

		if !ok {
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		// t.Miner (address.Address) (struct)
		case "Miner":

			{

				if err := t.Miner.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Miner: %w", err)
				}

			}
			// t.Price (big.Int) (struct)
		case "Price":

			{

				if err := t.Price.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Price: %w", err)
				}

			}
			// t.SeqNo (uint64) (uint64)
		case "SeqNo":

			{

				maj, extra, err = cr.ReadHeader()
				if err != nil {
					return err
				}
				if maj != cbg.MajUnsignedInt {
					return fmt.Errorf("wrong type for uint64 field")
				}
				t.SeqNo = uint64(extra)

			}
			// t.Expiry (abi.ChainEpoch) (int64)
		case "Expiry":
			{
				maj, extra, err := cr.ReadHeader()
				if err != nil {
					return err
				}
				var extraI int64
				switch maj {
				case cbg.MajUnsignedInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 positive overflow")
					}
				case cbg.MajNegativeInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 negative overflow")
					}
					extraI = -1 - extraI
				default:
					return fmt.Errorf("wrong type for int64 field: %d", maj)
				}

				t.Expiry = abi.ChainEpoch(extraI)
			}
			// t.Timestamp (abi.ChainEpoch) (int64)
		case "Timestamp":
			{
				maj, extra, err := cr.ReadHeader()
				if err != nil {
					return err
				}
				var extraI int64
				switch maj {
				case cbg.MajUnsignedInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 positive overflow")
					}
				case cbg.MajNegativeInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 negative overflow")
					}
					extraI = -1 - extraI
				default:
					return fmt.Errorf("wrong type for int64 field: %d", maj)
				}

				t.Timestamp = abi.ChainEpoch(extraI)
			}
			// t.MaxPieceSize (abi.PaddedPieceSize) (uint64)
		case "MaxPieceSize":

			{

				maj, extra, err = cr.ReadHeader()
				if err != nil {
					return err
				}
				if maj != cbg.MajUnsignedInt {
					return fmt.Errorf("wrong type for uint64 field")
				}
				t.MaxPieceSize = abi.PaddedPieceSize(extra)

			}
			// t.MinPieceSize (abi.PaddedPieceSize) (uint64)
		case "MinPieceSize":

			{

				maj, extra, err = cr.ReadHeader()
				if err != nil {
					return err
				}
				if maj != cbg.MajUnsignedInt {
					return fmt.Errorf("wrong type for uint64 field")
				}
				t.MinPieceSize = abi.PaddedPieceSize(extra)

			}
			// t.VerifiedPrice (big.Int) (struct)
		case "VerifiedPrice":

			{

				if err := t.VerifiedPrice.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.VerifiedPrice: %w", err)
				}

			}

		default:
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
func (t *Balance) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{162}); err != nil {
		return err
	}

	// t.Locked (big.Int) (struct)
	if len("Locked") > 8192 {
		return xerrors.Errorf("Value in field \"Locked\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Locked"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Locked")); err != nil {
		return err
	}

	if err := t.Locked.MarshalCBOR(cw); err != nil {
		return err
	}

	// t.Available (big.Int) (struct)
	if len("Available") > 8192 {
		return xerrors.Errorf("Value in field \"Available\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Available"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Available")); err != nil {
		return err
	}

	if err := t.Available.MarshalCBOR(cw); err != nil {
		return err
	}
	return nil
}

func (t *Balance) UnmarshalCBOR(r io.Reader) (err error) {
	*t = Balance{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("Balance: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 9)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 8192)
		if err != nil {
			return err
		}

		if !ok {
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		// t.Locked (big.Int) (struct)
		case "Locked":

			{

				if err := t.Locked.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Locked: %w", err)
				}

			}
			// t.Available (big.Int) (struct)
		case "Available":

			{

				if err := t.Available.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Available: %w", err)
				}

			}

		default:
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
func (t *AskRequest) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{161}); err != nil {
		return err
	}

	// t.Miner (address.Address) (struct)
	if len("Miner") > 8192 {
		return xerrors.Errorf("Value in field \"Miner\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Miner"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Miner")); err != nil {
		return err
	}

	if err := t.Miner.MarshalCBOR(cw); err != nil {
		return err
	}
	return nil
}

func (t *AskRequest) UnmarshalCBOR(r io.Reader) (err error) {
	*t = AskRequest{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("AskRequest: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 5)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 8192)
		if err != nil {
			return err
		}

		if !ok {
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		// t.Miner (address.Address) (struct)
		case "Miner":

			{

				if err := t.Miner.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Miner: %w", err)
				}

			}

		default:
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
func (t *AskResponse) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{161}); err != nil {
		return err
	}

	// t.Ask (legacytypes.SignedStorageAsk) (struct)
	if len("Ask") > 8192 {
		return xerrors.Errorf("Value in field \"Ask\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Ask"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Ask")); err != nil {
		return err
	}

	if err := t.Ask.MarshalCBOR(cw); err != nil {
		return err
	}
	return nil
}

func (t *AskResponse) UnmarshalCBOR(r io.Reader) (err error) {
	*t = AskResponse{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("AskResponse: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 3)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 8192)
		if err != nil {
			return err
		}

		if !ok {
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		// t.Ask (legacytypes.SignedStorageAsk) (struct)
		case "Ask":

			{

				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}
					t.Ask = new(SignedStorageAsk)
					if err := t.Ask.UnmarshalCBOR(cr); err != nil {
						return xerrors.Errorf("unmarshaling t.Ask pointer: %w", err)
					}
				}

			}

		default:
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
func (t *Protocol) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{162}); err != nil {
		return err
	}

	// t.Name (string) (string)
	if len("Name") > 8192 {
		return xerrors.Errorf("Value in field \"Name\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Name"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Name")); err != nil {
		return err
	}

	if len(t.Name) > 8192 {
		return xerrors.Errorf("Value in field t.Name was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Name))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string(t.Name)); err != nil {
		return err
	}

	// t.Addresses ([][]uint8) (slice)
	if len("Addresses") > 8192 {
		return xerrors.Errorf("Value in field \"Addresses\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Addresses"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Addresses")); err != nil {
		return err
	}

	if len(t.Addresses) > 8192 {
		return xerrors.Errorf("Slice value in field t.Addresses was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Addresses))); err != nil {
		return err
	}
	for _, v := range t.Addresses {
		if len(v) > 2097152 {
			return xerrors.Errorf("Byte array in field v was too long")
		}

		if err := cw.WriteMajorTypeHeader(cbg.MajByteString, uint64(len(v))); err != nil {
			return err
		}

		if _, err := cw.Write(v); err != nil {
			return err
		}

	}
	return nil
}

func (t *Protocol) UnmarshalCBOR(r io.Reader) (err error) {
	*t = Protocol{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("Protocol: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 9)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 8192)
		if err != nil {
			return err
		}

		if !ok {
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		// t.Name (string) (string)
		case "Name":

			{
				sval, err := cbg.ReadStringWithMax(cr, 8192)
				if err != nil {
					return err
				}

				t.Name = string(sval)
			}
			// t.Addresses ([][]uint8) (slice)
		case "Addresses":

			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > 8192 {
				return fmt.Errorf("t.Addresses: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Addresses = make([][]uint8, extra)
			}

			for i := 0; i < int(extra); i++ {
				{
					var maj byte
					var extra uint64
					var err error
					_ = maj
					_ = extra
					_ = err

					maj, extra, err = cr.ReadHeader()
					if err != nil {
						return err
					}

					if extra > 2097152 {
						return fmt.Errorf("t.Addresses[i]: byte array too large (%d)", extra)
					}
					if maj != cbg.MajByteString {
						return fmt.Errorf("expected byte array")
					}

					if extra > 0 {
						t.Addresses[i] = make([]uint8, extra)
					}

					if _, err := io.ReadFull(cr, t.Addresses[i]); err != nil {
						return err
					}

				}
			}

		default:
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
func (t *QueryResponse) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{161}); err != nil {
		return err
	}

	// t.Protocols ([]legacytypes.Protocol) (slice)
	if len("Protocols") > 8192 {
		return xerrors.Errorf("Value in field \"Protocols\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("Protocols"))); err != nil {
		return err
	}
	if _, err := cw.WriteString(string("Protocols")); err != nil {
		return err
	}

	if len(t.Protocols) > 8192 {
		return xerrors.Errorf("Slice value in field t.Protocols was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Protocols))); err != nil {
		return err
	}
	for _, v := range t.Protocols {
		if err := v.MarshalCBOR(cw); err != nil {
			return err
		}

	}
	return nil
}

func (t *QueryResponse) UnmarshalCBOR(r io.Reader) (err error) {
	*t = QueryResponse{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("QueryResponse: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 9)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 8192)
		if err != nil {
			return err
		}

		if !ok {
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		// t.Protocols ([]legacytypes.Protocol) (slice)
		case "Protocols":

			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > 8192 {
				return fmt.Errorf("t.Protocols: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Protocols = make([]Protocol, extra)
			}

			for i := 0; i < int(extra); i++ {
				{
					var maj byte
					var extra uint64
					var err error
					_ = maj
					_ = extra
					_ = err

					{

						if err := t.Protocols[i].UnmarshalCBOR(cr); err != nil {
							return xerrors.Errorf("unmarshaling t.Protocols[i]: %w", err)
						}

					}

				}
			}

		default:
			// Field doesn't exist on this type, so ignore it
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
