package wallet

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/filecoin-project/lotus/chain/types"
)

func TestParsePrivateKeyMaterialHex(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	raw := crypto.FromECDSA(key)
	hexKey := hex.EncodeToString(raw)

	got, err := ParsePrivateKeyMaterial(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hexKey {
		t.Fatalf("got %x want %s", got, hexKey)
	}

	got, err = ParsePrivateKeyMaterial("0x" + hexKey)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hexKey {
		t.Fatalf("got %x want %s", got, hexKey)
	}
}

func TestParsePrivateKeyMaterialLotusExport(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	raw := crypto.FromECDSA(key)

	ki := types.KeyInfo{Type: types.KTSecp256k1, PrivateKey: raw}
	jsonBytes, err := json.Marshal(ki)
	if err != nil {
		t.Fatal(err)
	}

	// lotus wallet export prints hex-encoded KeyInfo JSON.
	export := hex.EncodeToString(jsonBytes)
	got, err := ParsePrivateKeyMaterial(export)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(raw) {
		t.Fatalf("got %x want %x", got, raw)
	}

	// Also accept the decoded JSON form.
	got, err = ParsePrivateKeyMaterial(string(jsonBytes))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(raw) {
		t.Fatalf("got %x want %x", got, raw)
	}
}
