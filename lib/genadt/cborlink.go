package genadt

import (
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/ipfs/go-cid"
	ipldcbor "github.com/ipfs/go-ipld-cbor"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// Store is a small interface that merges ipldcbor.IpldStore with a context.
type Store interface {
	Context() context.Context
	ipldcbor.IpldStore
}

// CborLink holds an optional in-memory value of type T and a cid.Cid link pointing
// to that value when stored in an IPLD blockstore.
type CborLink[T CborT] struct {
	cid cid.Cid
	val T
}

func Link[T CborT](cid cid.Cid) *CborLink[T] {
	return &CborLink[T]{cid: cid}
}

// MarshalCBOR encodes this link the same way a bare CID is encoded in DAG-CBOR/Filecoin:
//   - A CBOR Tag(42)
//   - Followed by a CBOR byte-string: [ 0x00, <raw CID bytes> ]
func (cl *CborLink[T]) MarshalCBOR(w io.Writer) error {
	return cbg.CborCid(cl.cid).MarshalCBOR(w)
}

// UnmarshalCBOR decodes a link from the standard CBOR Tag(42) + byte-string format.
func (cl *CborLink[T]) UnmarshalCBOR(r io.Reader) error {
	var cc cbg.CborCid
	if err := cc.UnmarshalCBOR(r); err != nil {
		return err
	}
	cl.cid = cid.Cid(cc)
	return nil
}

// Cid returns the cid for this link (which may be cid.Undef if unset).
func (cl *CborLink[T]) Cid() cid.Cid {
	return cl.cid
}

// Load fetches and decodes the linked value from the provided Store, caching it in cl.val.
// Returns the loaded/cached value. If the link is cid.Undef, returns an error.
func (cl *CborLink[T]) Load(st Store) (T, error) {
	var zero T
	if !cl.cid.Defined() {
		return zero, errors.New("CborLink has no CID (undefined link)")
	}
	if !isZero(cl.val) {
		// Already loaded/cached
		return cl.val, nil
	}
	// Need to allocate a new T instance for decoding.
	// If T is always a pointer type (like *MyStruct), reflect can do it:
	val := newValue[T]()
	// Or if T is a struct by value, you might do val := new(T).(T)

	if err := st.Get(st.Context(), cl.cid, val); err != nil {
		return zero, err
	}
	cl.val = val
	return val, nil
}

// Store takes a value `val`, encodes it into the Store, sets cl.cid to the resulting CID,
// and also caches the value in cl.val.
func (cl *CborLink[T]) Store(st Store, val T) error {
	c, err := st.Put(st.Context(), val)
	if err != nil {
		return err
	}
	cl.cid = c
	cl.val = val
	return nil
}

// newValue uses reflection to allocate a new zero value of T.
// This works if T is always a pointer-to-struct type.
// If T can be a non-pointer type, you might handle differently.
func newValue[T any]() T {
	typ := reflect.TypeOf((*T)(nil)).Elem()
	// typ is "T" itself; if T is a pointer type, typ.Kind() == reflect.Pointer.
	valPtr := reflect.New(typ.Elem()) // allocate struct under pointer
	return valPtr.Interface().(T)
}

func isZero[T any](x T) bool {
	var zero T
	return reflect.DeepEqual(x, zero)
}
