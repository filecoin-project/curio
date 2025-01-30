package genadt

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/ipfs/go-cid"
)

// -------------------------------------------------------------------------------------
// 1) The CborT interface (both Marshaler & Unmarshaler)
// -------------------------------------------------------------------------------------

type CborT interface {
	cbor.Marshaler
	cbor.Unmarshaler
}

// -------------------------------------------------------------------------------------
// 2) The Keyer interface for generics
// -------------------------------------------------------------------------------------

// Keyer maps a user-level key type `K` to a `abi.Keyer`, and
// also provides ParseKey to invert the string key back to `K`.
type Keyer[K any] interface {
	Keyer(K) abi.Keyer
	ParseKey(string) (K, error)
}

// -------------------------------------------------------------------------------------
// 3) Common Keyers with ParseKey implementations
// -------------------------------------------------------------------------------------

// --------------------------------------------------------------------
// AbiKeyer -- pass-through for any built-in `abi.Keyer` type
// (abi.CidKey, abi.AddrKey, etc.), with a type-switch to parse the string.
// --------------------------------------------------------------------
type AbiKeyer[K abi.Keyer] struct{}

func (AbiKeyer[K]) Keyer(k K) abi.Keyer {
	return k
}

// ParseKey attempts to do the inverse of `Key()` for several built-in
// `abi.Keyer` types. If you have more, just extend the switch.
func (AbiKeyer[K]) ParseKey(s string) (K, error) {
	var zero K
	switch any(zero).(type) {

	case abi.CidKey:
		// Parse back from a cid "KeyString"
		c, err := cid.Parse(s)
		if err != nil {
			return zero, err
		}
		// Convert `abi.CidKey(c)` to K.
		// We do `any(...)` then type-assert to K to avoid direct cast errors.
		return any(abi.CidKey(c)).(K), nil

	case abi.AddrKey:
		// parse back from bytes
		a, err := address.NewFromBytes([]byte(s))
		if err != nil {
			return zero, err
		}
		return any(abi.AddrKey(a)).(K), nil

	case abi.IdAddrKey:
		a, err := address.NewFromBytes([]byte(s))
		if err != nil {
			return zero, err
		}
		return any(abi.IdAddrKey(a)).(K), nil

	default:
		return zero, fmt.Errorf("unsupported type for AbiKeyer: %T", zero)
	}
}

// --------------------------------------------------------------------
// CidKeyer adapts a `cid.Cid` using `abi.CidKey` and can parse
// the string key via cid.Parse(...) back to `cid.Cid`.
// --------------------------------------------------------------------
type CidKeyer struct{}

func (CidKeyer) Keyer(k cid.Cid) abi.Keyer {
	return abi.CidKey(k)
}

func (CidKeyer) ParseKey(s string) (cid.Cid, error) {
	return cid.Parse(s)
}

// --------------------------------------------------------------------
// AddressKeyer uses `abi.AddrKey` for arbitrary addresses
// --------------------------------------------------------------------
type AddressKeyer struct{}

func (AddressKeyer) Keyer(k address.Address) abi.Keyer {
	return abi.AddrKey(k)
}

func (AddressKeyer) ParseKey(s string) (address.Address, error) {
	return address.NewFromBytes([]byte(s))
}

// --------------------------------------------------------------------
// AddressPairKeyer uses `*abi.AddrPairKey` and round-trips via CBOR
// --------------------------------------------------------------------
type AddressPairKeyer struct{}

func (AddressPairKeyer) Keyer(k *abi.AddrPairKey) abi.Keyer {
	return k
}

// ParseKey inverts k.Key() which is done by k.MarshalCBOR(...).
func (AddressPairKeyer) ParseKey(s string) (*abi.AddrPairKey, error) {
	k := new(abi.AddrPairKey)
	// Because the original Key() was basically the raw bytes of CBOR,
	// we can treat s as []byte here.
	if err := k.UnmarshalCBOR(bytes.NewReader([]byte(s))); err != nil {
		return nil, err
	}
	return k, nil
}

// --------------------------------------------------------------------
// IntKeyer adapts int64 using `abi.IntKey`
// --------------------------------------------------------------------
type IntKeyer struct{}

func (IntKeyer) Keyer(k int64) abi.Keyer {
	return abi.IntKey(k)
}

func (IntKeyer) ParseKey(s string) (int64, error) {
	return abi.ParseIntKey(s)
}

// --------------------------------------------------------------------
// UIntKeyer adapts uint64 using `abi.UIntKey`
// --------------------------------------------------------------------
type UIntKeyer struct{}

func (UIntKeyer) Keyer(k uint64) abi.Keyer {
	return abi.UIntKey(k)
}

func (UIntKeyer) ParseKey(s string) (uint64, error) {
	return abi.ParseUIntKey(s)
}

// -------------------------------------------------------------------------------------
// 4) HamtMap interface (underlying ADT map like `*adt.Map`)
// -------------------------------------------------------------------------------------

type HamtMap interface {
	// MarshalCBOR serializes the map to CBOR format.
	MarshalCBOR(w io.Writer) error

	// Root returns the root CID of the underlying HAMT.
	Root() (cid.Cid, error)

	// Put adds a value `v` with key `k` to the map.
	Put(k abi.Keyer, v cbor.Marshaler) error

	// Get retrieves the value at `k` into `out`.
	Get(k abi.Keyer, out cbor.Unmarshaler) (bool, error)

	// Has checks whether the key exists in the map.
	Has(k abi.Keyer) (bool, error)

	// PutIfAbsent sets key `k` to value `v` if it is not already present.
	PutIfAbsent(k abi.Keyer, v cbor.Marshaler) (bool, error)

	// TryDelete removes the value at `k`, returning whether it was present.
	TryDelete(k abi.Keyer) (bool, error)

	// Delete removes the value at `k`, expecting it to exist.
	Delete(k abi.Keyer) error

	// ForEach iterates over all key-value pairs in the map.
	ForEach(out cbor.Unmarshaler, fn func(key string) error) error

	// CollectKeys returns all keys in the map as a slice.
	CollectKeys() ([]string, error)

	// Pop retrieves and removes the entry for `k`.
	Pop(k abi.Keyer, out cbor.Unmarshaler) (bool, error)

	// IsEmpty checks whether the map contains any elements.
	IsEmpty() (bool, error)
}

// -------------------------------------------------------------------------------------
// 5) The generic Map wrapper with typed iteration
//
//    M is the underlying map type (e.g. *adt.Map) that implements HamtMap.
//    K is the user-level key type (e.g. cid.Cid, address.Address, etc.).
//    R is the Keyer that knows how to go K -> abi.Keyer + parse string -> K.
//    V is a type that implements both cbor.Marshaler and cbor.Unmarshaler.
// -------------------------------------------------------------------------------------

type Map[M HamtMap, K any, R Keyer[K], V CborT] struct {
	Sub M

	VType reflect.Type
}

func New[M HamtMap, K any, R Keyer[K], V CborT](sub M) *Map[M, K, R, V] {
	// V is already a pointer to a struct, so we .Elem twice to get the struct type.
	m := &Map[M, K, R, V]{}
	m.Sub = sub
	m.VType = reflect.TypeOf((*V)(nil)).Elem().Elem()
	return m
}

// -------------------------------------
// Basic pass-through methods:
// -------------------------------------

func (m *Map[M, K, R, V]) MarshalCBOR(w io.Writer) error {
	return m.Sub.MarshalCBOR(w)
}

func (m *Map[M, K, R, V]) Root() (cid.Cid, error) {
	return m.Sub.Root()
}

func (m *Map[M, K, R, V]) IsEmpty() (bool, error) {
	return m.Sub.IsEmpty()
}

func (m *Map[M, K, R, V]) CollectKeys() ([]string, error) {
	return m.Sub.CollectKeys()
}

// -------------------------------------
// Put / Get / Has / etc.
// -------------------------------------

func (m *Map[M, K, R, V]) Put(k K, v V) error {
	var r R
	return m.Sub.Put(r.Keyer(k), v)
}

func (m *Map[M, K, R, V]) Get(k K, out cbor.Unmarshaler) (bool, error) {
	var r R
	return m.Sub.Get(r.Keyer(k), out)
}

// GetValue is a typed convenience method that returns (value, found, error).
func (m *Map[M, K, R, V]) GetValue(k K) (V, bool, error) {
	var r R

	val := reflect.New(m.VType).Interface().(V)

	found, err := m.Sub.Get(r.Keyer(k), val)
	return val, found, err
}

func (m *Map[M, K, R, V]) Has(k K) (bool, error) {
	var r R
	return m.Sub.Has(r.Keyer(k))
}

func (m *Map[M, K, R, V]) PutIfAbsent(k K, v V) (bool, error) {
	var r R
	return m.Sub.PutIfAbsent(r.Keyer(k), v)
}

func (m *Map[M, K, R, V]) Delete(k K) error {
	var r R
	return m.Sub.Delete(r.Keyer(k))
}

func (m *Map[M, K, R, V]) TryDelete(k K) (bool, error) {
	var r R
	return m.Sub.TryDelete(r.Keyer(k))
}

func (m *Map[M, K, R, V]) Pop(k K, out cbor.Unmarshaler) (bool, error) {
	var r R
	return m.Sub.Pop(r.Keyer(k), out)
}

// -------------------------------------
// ForEach with typed K/V
// -------------------------------------

// ForEach iterates over all string keys in the map, parses them into `K`,
// fetches the corresponding `V`, and calls `fn(k, v)`.
func (m *Map[M, K, R, V]) ForEach(fn func(k K, v V) error) error {
	var r R
	val := reflect.New(m.VType).Interface().(V)

	return m.Sub.ForEach(val, func(skey string) error {
		// Convert from string -> typed key
		k, err := r.ParseKey(skey)
		if err != nil {
			return err
		}
		// Now fetch the typed value
		_, err = m.Sub.Get(r.Keyer(k), val)
		if err != nil {
			return err
		}
		return fn(k, val)
	})
}
