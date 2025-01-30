package genadt

import (
	"context"
	"fmt"
	"github.com/filecoin-project/lotus/blockstore"
	cbornode "github.com/ipfs/go-ipld-cbor"
	"io"
	"sort"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v14/util/adt"
	"github.com/filecoin-project/go-state-types/cbor"

	"github.com/whyrusleeping/cbor-gen"
)

// ----------------------------------------------------------------------
// A simple CBOR test type for values (implements cbor.Marshaler & cbor.Unmarshaler).
// Demonstrates correct usage with typegen utilities.
// ----------------------------------------------------------------------
type testValue struct {
	Data string
}

// Ensure it meets the cbor.Marshaler and cbor.Unmarshaler interfaces:
var _ cbor.Marshaler = (*testValue)(nil)
var _ cbor.Unmarshaler = (*testValue)(nil)

// MarshalCBOR encodes testValue as a single-field CBOR map: { "Data": <string> }
func (tv *testValue) MarshalCBOR(w io.Writer) error {
	// Wrap writer so we can use typegen helper.
	cw := typegen.NewCborWriter(w)

	// We'll encode a single-field map with 1 entry: "Data" -> tv.Data
	if err := cw.WriteMajorTypeHeader(typegen.MajMap, 1); err != nil {
		return err
	}
	// Key: "Data"
	if err := typegen.CborWriteHeader(cw, typegen.MajTextString, uint64(len("Data"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("Data"); err != nil {
		return err
	}

	// Value: tv.Data
	if err := typegen.CborWriteHeader(cw, typegen.MajTextString, uint64(len(tv.Data))); err != nil {
		return err
	}
	if _, err := cw.WriteString(tv.Data); err != nil {
		return err
	}
	return nil
}

// UnmarshalCBOR decodes testValue as a single-field CBOR map: { "Data": <string> }
func (tv *testValue) UnmarshalCBOR(r io.Reader) error {
	cr := typegen.NewCborReader(r)
	maj, size, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	// We expect a map with 1 entry.
	if maj != typegen.MajMap || size != 1 {
		return fmt.Errorf("invalid CBOR input for testValue; expected a single-field map, got maj=%d size=%d", maj, size)
	}

	// Read the key, which must be "Data"
	key, err := typegen.ReadString(cr)
	if err != nil {
		return err
	}
	if key != "Data" {
		return fmt.Errorf("expected key 'Data', got %q", key)
	}

	// Read the string value
	val, err := typegen.ReadString(cr)
	if err != nil {
		return err
	}
	tv.Data = val
	return nil
}

// ----------------------------------------------------------------------
// Helper to create a new empty adt.Map with an in-memory store.
// ----------------------------------------------------------------------
func newAdtMap(t *testing.T) *adt.Map {
	bs := blockstore.NewMemory()
	is := cbornode.NewCborStore(bs)
	store := adt.WrapStore(context.Background(), adt.WrapStore(context.Background(), is))

	hamt, err := adt.MakeEmptyMap(store, 5)
	require.NoError(t, err, "failed to create empty adt.Map")
	return hamt
}

// Some sample keys for testing
var (
	sampleCid1, _ = cid.Parse("bafkqabkj7n5gi4aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	sampleCid2, _ = cid.Parse("bafkqbky7usfid5bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	sampleCid3, _ = cid.Parse("bafkqbkz7usfid5ccccccccccccccccccccccccccccccccc")

	sampleAddr1, _ = address.NewFromString("t1abcdefff")
	sampleAddr2, _ = address.NewFromString("t1fghijzzz")
	sampleAddr3, _ = address.NewFromString("t1klmnoppp")

	sampleValA = &testValue{Data: "valueA"}
	sampleValB = &testValue{Data: "valueB"}
	sampleValC = &testValue{Data: "valueC"}
)

// ----------------------------------------------------------------------
// 1) Example test with CidKeyer (key = cid.Cid).
// ----------------------------------------------------------------------
func TestMapWithCidKeyer(t *testing.T) {
	hamt := newAdtMap(t)
	myMap := New[*adt.Map, cid.Cid, CidKeyer, *testValue](hamt)

	// Initially empty
	empty, err := myMap.IsEmpty()
	require.NoError(t, err)
	require.True(t, empty, "map should start empty")

	// Put 2 items
	err = myMap.Put(sampleCid1, sampleValA)
	require.NoError(t, err)
	err = myMap.Put(sampleCid2, sampleValB)
	require.NoError(t, err)

	// Check emptiness
	empty, err = myMap.IsEmpty()
	require.NoError(t, err)
	require.False(t, empty)

	// Has?
	has, err := myMap.Has(sampleCid1)
	require.NoError(t, err)
	require.True(t, has)

	// GetValue
	val, found, err := myMap.GetValue(sampleCid1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "valueA", val.Data)

	// Not found case
	_, found, err = myMap.GetValue(sampleCid3)
	require.NoError(t, err)
	require.False(t, found, "should not find key that wasn't inserted")

	// PutIfAbsent
	inserted, err := myMap.PutIfAbsent(sampleCid1, sampleValC)
	require.NoError(t, err)
	require.False(t, inserted, "should not insert if key already present")

	inserted, err = myMap.PutIfAbsent(sampleCid3, sampleValC)
	require.NoError(t, err)
	require.True(t, inserted, "expected to insert new key")

	// Now we have 3 entries in total

	// ForEach
	collected := make(map[string]string)
	err = myMap.ForEach(func(k cid.Cid, v *testValue) error {
		collected[k.String()] = v.Data
		return nil
	})
	require.NoError(t, err)
	require.Len(t, collected, 3)
	require.Equal(t, "valueA", collected[sampleCid1.String()])
	require.Equal(t, "valueB", collected[sampleCid2.String()])
	require.Equal(t, "valueC", collected[sampleCid3.String()])

	// TryDelete
	removed, err := myMap.TryDelete(sampleCid2)
	require.NoError(t, err)
	require.True(t, removed)

	removed, err = myMap.TryDelete(sampleCid2)
	require.NoError(t, err)
	require.False(t, removed, "already removed")

	// Pop
	var out testValue
	found2, err := myMap.Pop(sampleCid3, &out)
	require.NoError(t, err)
	require.True(t, found2)
	require.Equal(t, "valueC", out.Data)

	// CollectKeys
	keys, err := myMap.CollectKeys()
	require.NoError(t, err)
	// only sampleCid1 left
	require.Len(t, keys, 1)

	// Delete
	err = myMap.Delete(sampleCid1)
	require.NoError(t, err)

	// Should be empty again
	isEmpty, err := myMap.IsEmpty()
	require.NoError(t, err)
	require.True(t, isEmpty)
}

// ----------------------------------------------------------------------
// 2) Example test with AbiKeyer[abi.CidKey] (key = abi.CidKey).
// ----------------------------------------------------------------------
func TestMapWithAbiCidKey(t *testing.T) {
	hamt := newAdtMap(t)
	myMap := New[*adt.Map, abi.CidKey, AbiKeyer[abi.CidKey], *testValue](hamt)

	// Insert some entries
	require.NoError(t, myMap.Put(abi.CidKey(sampleCid1), sampleValA))
	require.NoError(t, myMap.Put(abi.CidKey(sampleCid2), sampleValB))

	// Check retrieval
	v, found, err := myMap.GetValue(abi.CidKey(sampleCid2))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "valueB", v.Data)
}

// ----------------------------------------------------------------------
// 3) Example with AddressKeyer (key = address.Address).
// ----------------------------------------------------------------------
func TestMapWithAddressKeyer(t *testing.T) {
	hamt := newAdtMap(t)
	myMap := New[*adt.Map, address.Address, AddressKeyer, *testValue](hamt)

	// Insert a few
	require.NoError(t, myMap.Put(sampleAddr1, &testValue{"foo"}))
	require.NoError(t, myMap.Put(sampleAddr2, &testValue{"bar"}))
	require.NoError(t, myMap.Put(sampleAddr3, &testValue{"baz"}))

	// ForEach
	addrs := make([]string, 0)
	err := myMap.ForEach(func(k address.Address, v *testValue) error {
		addrs = append(addrs, k.String()+":"+v.Data)
		return nil
	})
	require.NoError(t, err)

	sort.Strings(addrs)
	require.Equal(t, []string{
		sampleAddr1.String() + ":foo",
		sampleAddr2.String() + ":bar",
		sampleAddr3.String() + ":baz",
	}, addrs)

	// CollectKeys
	keys, err := myMap.CollectKeys()
	require.NoError(t, err)
	require.Len(t, keys, 3)

	// Has
	has, err := myMap.Has(sampleAddr1)
	require.NoError(t, err)
	require.True(t, has)

	// Pop
	var popped testValue
	found, err := myMap.Pop(sampleAddr2, &popped)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "bar", popped.Data)
}

// ----------------------------------------------------------------------
// 4) Example with IntKeyer (key = int64).
// ----------------------------------------------------------------------
func TestMapWithIntKeyer(t *testing.T) {
	hamt := newAdtMap(t)
	myMap := New[*adt.Map, int64, IntKeyer, *testValue](hamt)

	require.NoError(t, myMap.Put(10, &testValue{"ten"}))
	require.NoError(t, myMap.Put(-5, &testValue{"minus-five"}))

	// GetValue
	val, found, err := myMap.GetValue(-5)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "minus-five", val.Data)

	// ForEach
	seen := make(map[int64]string)
	err = myMap.ForEach(func(k int64, v *testValue) error {
		seen[k] = v.Data
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[int64]string{
		10: "ten",
		-5: "minus-five",
	}, seen)
}

// ----------------------------------------------------------------------
// 5) Example with UIntKeyer (key = uint64).
// ----------------------------------------------------------------------
func TestMapWithUIntKeyer(t *testing.T) {
	hamt := newAdtMap(t)
	myMap := New[*adt.Map, uint64, UIntKeyer, *testValue](hamt)

	require.NoError(t, myMap.Put(100, &testValue{"one-hundred"}))
	require.NoError(t, myMap.Put(9999, &testValue{"nine-nine-nine-nine"}))

	val, found, err := myMap.GetValue(9999)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "nine-nine-nine-nine", val.Data)

	// TryDelete
	removed, err := myMap.TryDelete(100)
	require.NoError(t, err)
	require.True(t, removed)

	has, err := myMap.Has(100)
	require.NoError(t, err)
	require.False(t, has)
}

// ----------------------------------------------------------------------
// 6) Example with AddressPairKeyer (key = *abi.AddrPairKey).
// ----------------------------------------------------------------------
func TestMapWithAddrPairKeyer(t *testing.T) {
	hamt := newAdtMap(t)
	myMap := New[*adt.Map, *abi.AddrPairKey, AddressPairKeyer, *testValue](hamt)

	ap1 := abi.NewAddrPairKey(sampleAddr1, sampleAddr2)
	ap2 := abi.NewAddrPairKey(sampleAddr2, sampleAddr3)

	require.NoError(t, myMap.Put(ap1, &testValue{"ap1->(addr1,addr2)"}))
	require.NoError(t, myMap.Put(ap2, &testValue{"ap2->(addr2,addr3)"}))

	out, found, err := myMap.GetValue(ap1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "ap1->(addr1,addr2)", out.Data)

	// ForEach
	pairs := make(map[string]string)
	err = myMap.ForEach(func(k *abi.AddrPairKey, v *testValue) error {
		keyStr := fmt.Sprintf("%s-%s", k.First, k.Second)
		pairs[keyStr] = v.Data
		return nil
	})
	require.NoError(t, err)
	require.Len(t, pairs, 2)

	require.Equal(t, "ap1->(addr1,addr2)", pairs[fmt.Sprintf("%s-%s", sampleAddr1, sampleAddr2)])
	require.Equal(t, "ap2->(addr2,addr3)", pairs[fmt.Sprintf("%s-%s", sampleAddr2, sampleAddr3)])
}

// ----------------------------------------------------------------------
// Example: Testing reflection logic in GetValue is correct
// (only needed if your Map code does reflection on V).
// ----------------------------------------------------------------------
func TestReflectionLogic(t *testing.T) {
	hamt := newAdtMap(t)
	myMap := New[*adt.Map, cid.Cid, CidKeyer, *testValue](hamt)

	// Confirm VType is correct:
	require.NotNil(t, myMap.VType)
	require.Equal(t, "testValue", myMap.VType.Name())

	// Insert / retrieve
	require.NoError(t, myMap.Put(sampleCid1, &testValue{"reflection-check"}))
	tv, found, err := myMap.GetValue(sampleCid1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "reflection-check", tv.Data)
}
