package keysmith

import (
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	// FOC_VERSION is the only foc/v value accepted in app_metadata.
	FOC_VERSION = 2

	// NODE_GROUP_KEY is the root grant node.
	NODE_GROUP_KEY = "groupKey"

	// NODE_GROUP_PREFIX precedes a group path in a grant node.
	NODE_GROUP_PREFIX = "group:"

	// NODE_PIECE_PREFIX precedes a piece salt in a grant node.
	NODE_PIECE_PREFIX = "piece:"
)

// eip712DomainWire is the EIP-712 domain. json tags are the signed names.
type eip712DomainWire struct {
	Name    string `json:"name" eip712:"string"`
	Version string `json:"version" eip712:"string"`
}

// readGrantWire is one EIP-712 ReadGrant. json tags are the signed names.
// groupKeyId is the EIP-712 name of the group key; the envelope calls it foc/gkid.
type readGrantWire struct {
	V                  *big.Int `json:"v" eip712:"uint8"`
	Node               string   `json:"node" eip712:"string"`
	Owner              string   `json:"owner" eip712:"address"`
	GroupKeyID         []byte   `json:"groupKeyId" eip712:"bytes16"`
	Bearer             string   `json:"bearer" eip712:"address"`
	MaxDepth           *big.Int `json:"maxDepth" eip712:"uint8"`
	ChainID            *big.Int `json:"chainId" eip712:"uint256"`
	KeyCommitment      []byte   `json:"keyCommitment" eip712:"bytes16"`
	GroupKeyCommitment []byte   `json:"groupKeyCommitment" eip712:"bytes16"`
	Expires            *big.Int `json:"expires" eip712:"uint64"`
}

// readRequestWire is one EIP-712 ReadRequest. json tags are the signed names.
type readRequestWire struct {
	ChainID  *big.Int `json:"chainId" eip712:"uint256"`
	PieceCID []byte   `json:"pieceCid" eip712:"bytes"`
	Expires  *big.Int `json:"expires" eip712:"uint64"`
}

// jsonTag reads a json name. "-" and an empty tag are skipped.
// omitempty marks a key that may be absent.
func jsonTag(f reflect.StructField) (name string, optional, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "" || tag == "-" {
		return "", false, true
	}
	name, opt, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return "", false, true
	}
	return name, opt == "omitempty", false
}

// eip712Struct is msg as a struct. The caller passes a struct or a pointer to one.
func eip712Struct(msg any) reflect.Value {
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		panic("eip712 value must be a struct")
	}
	return v
}

// eip712Types lists msg's tagged fields in source order.
// Each field has a json name and an eip712 solidity type. The caller passes a struct.
func eip712Types(msg any) []apitypes.Type {
	v := eip712Struct(msg)
	t := v.Type()
	out := make([]apitypes.Type, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, skip := jsonTag(f)
		if skip {
			continue
		}
		sol := f.Tag.Get("eip712")
		if sol == "" {
			panic("eip712 type missing on " + f.Name)
		}
		out = append(out, apitypes.Type{Name: name, Type: sol})
	}
	return out
}

// eip712Message maps msg's json names to its field values.
// The caller passes the same struct shape eip712Types described.
func eip712Message(msg any) apitypes.TypedDataMessage {
	v := eip712Struct(msg)
	t := v.Type()
	out := make(apitypes.TypedDataMessage, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, skip := jsonTag(f)
		if skip {
			continue
		}
		out[name] = v.Field(i).Interface()
	}
	return out
}
