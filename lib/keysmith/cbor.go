package keysmith

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

const (
	maxEnvelopeSize = 1 << 20 // 1 MiB, the FEE envelope ceiling
	maxSafeInteger  = 1<<53 - 1
)

// decMode rejects ambiguous CBOR. Keysmith and FEE both require definite
// lengths, minimal integers, and valid UTF-8.
var decMode cbor.DecMode

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
