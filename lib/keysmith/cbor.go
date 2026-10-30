package keysmith

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

const (
	maxEnvelopeSize = 1 << 20 // 1 MiB, the FEE envelope ceiling
	maxSafeInteger  = 1<<53 - 1
)

// decMode rejects ambiguous CBOR. Keysmith and FEE both require definite
// lengths, minimal integers, and valid UTF-8. Tags stay allowed: a FEE
// envelope is a COSE tag.
var decMode cbor.DecMode

// presDec is the presentation decoder. Tags are forbidden there.
var presDec cbor.DecMode

// presEnc is the core deterministic encoder a presentation is compared against.
var presEnc cbor.EncMode

func init() {
	var err error
	decMode, err = cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		IndefLength:      cbor.IndefLengthForbidden,
		IntDec:           cbor.IntDecConvertNone,
		MaxNestedLevels:  256,
		MaxArrayElements: 10000,
		MaxMapPairs:      10000,
		UTF8:             cbor.UTF8RejectInvalid,
		NaN:              cbor.NaNDecodeForbidden,
		Inf:              cbor.InfDecodeForbidden,
		BignumTag:        cbor.BignumTagForbidden,
	}.DecMode()
	if err != nil {
		panic(fmt.Sprintf("foc cbor mode: %v", err))
	}
	presDec, err = cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		IndefLength:      cbor.IndefLengthForbidden,
		TagsMd:           cbor.TagsForbidden,
		IntDec:           cbor.IntDecConvertNone,
		MaxNestedLevels:  64,
		MaxArrayElements: 64,
		MaxMapPairs:      32,
		UTF8:             cbor.UTF8RejectInvalid,
		NaN:              cbor.NaNDecodeForbidden,
		Inf:              cbor.InfDecodeForbidden,
	}.DecMode()
	if err != nil {
		panic(fmt.Sprintf("foc presentation cbor mode: %v", err))
	}
	presEnc, err = cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		panic(fmt.Sprintf("foc presentation cbor encoder: %v", err))
	}
}

// canonicalCBOR decodes one CBOR item and requires b to be that item's core
// deterministic encoding. The caller passes a complete presentation, with no
// trailing bytes. Integers outside ±(2^53−1) and floats are non-canonical.
func canonicalCBOR(b []byte) (any, error) {
	var value any
	rest, err := presDec.UnmarshalFirst(b, &value)
	if err != nil || len(rest) != 0 {
		return nil, errNotCanonical
	}
	enc, err := presEnc.Marshal(value)
	if err != nil || !bytes.Equal(enc, b) || !safeCBORNumbers(value) {
		return nil, errNotCanonical
	}
	return value, nil
}

var errNotCanonical = fmt.Errorf("not canonical")

// safeCBORNumbers reports whether every integer in v fits in a JavaScript
// safe integer and no float is present. The caller has already decoded v.
func safeCBORNumbers(v any) bool {
	switch n := v.(type) {
	case uint64:
		return n <= maxSafeInteger
	case int64:
		return n >= -maxSafeInteger && n <= maxSafeInteger
	case int:
		return int64(n) >= -maxSafeInteger && int64(n) <= maxSafeInteger
	case float32, float64:
		return false
	case *big.Int:
		return n != nil && n.IsInt64() && n.Int64() >= -maxSafeInteger && n.Int64() <= maxSafeInteger
	case big.Int:
		return n.IsInt64() && n.Int64() >= -maxSafeInteger && n.Int64() <= maxSafeInteger
	case []any:
		for _, e := range n {
			if !safeCBORNumbers(e) {
				return false
			}
		}
		return true
	case map[any]any:
		for k, e := range n {
			if !safeCBORNumbers(k) || !safeCBORNumbers(e) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func asMap(v any) (map[any]any, bool) {
	m, ok := v.(map[any]any)
	return m, ok
}

func asInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case uint64:
		if n > uint64(maxSafeInteger) {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}

func asUint(v any) (uint64, bool) {
	n, ok := asInt(v)
	if !ok || n < 0 {
		return 0, false
	}
	return uint64(n), true
}

// lookup finds an integer map key. CBOR positive and negative integers decode
// as different Go types, so both are checked.
func lookup(m map[any]any, key int64) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	if key >= 0 {
		if v, ok := m[uint64(key)]; ok {
			return v, true
		}
	}
	return nil, false
}
