package proof

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/snadrus/must"

	"github.com/filecoin-project/filecoin-ffi/cgo"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"

	poseidondst "github.com/filecoin-project/curio/lib/proof/poseidon"
)

func TestPoStVproofVerify(t *testing.T) {
	//logging.SetDebugLogging()

	vpdata, err := os.ReadFile("testgolden/post.vproof")
	if err != nil {
		t.Fatal(err)
	}

	vp, err := DecodeFallbackPoStSectorProof(bytes.NewReader(vpdata))
	if err != nil {
		t.Fatal(err)
	}

	testMarshal = true
	fmt.Println(string(must.One(json.MarshalIndent(vp, "", "  "))))
	testMarshal = false

	pvidata, err := os.ReadFile("testgolden/postpub.json")
	if err != nil {
		t.Fatal(err)
	}

	var pvi proof.WindowPoStVerifyInfo
	err = json.Unmarshal(pvidata, &pvi)
	if err != nil {
		t.Fatal(err)
	}

	pvi.Proofs[0].ProofBytes = vpdata

	ok, err := VerifyWindowPoStVanilla(pvi)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("ok", ok)
}

func TestPoStVproofProofVerify(t *testing.T) {
	fmt.Printf("%x", must.One(toProverID(1000)))
}

func toProverID(minerID abi.ActorID) (cgo.ByteArray32, error) {
	maddr, err := address.NewIDAddress(uint64(minerID))
	if err != nil {
		panic(err)
	}

	return cgo.AsByteArray32(maddr.Payload()), nil
}

func TestPoseidonHashMulti(t *testing.T) {
	/*
			f743111428ecda9b74a18826f1cc62d48ae1510de17e58645997559f86442f31
		    197a6efa37086e3a37108f8460c52bc810e2781aff0091bfd7b74e3f6f752f02
		    01c2c12f7930bbb59e3b36dae1c694b33ac6b3f199ec32a8665fadb8338f4f21
		    bbe672a853f65cdb2563339ca61d6ad648473b4b4ce44bf3a48486424b865a2d
		    937d57295f488cea8099f5525958f1af39c6241614b917835292e8e2314cf908
		    3272570cd8ba38c1183629ca72d5e9149d76786ed22c1d591db3d2bccfa7d308
		    2d3fc90e11aaf601bd1beae80fc3aba15a9e1f886d8140533ce36cda3dd9f936
		    0a017e264d0728bfcd2edb3c6c60cc5a05a02af31a71cdd7bb2da27616f5f630

			into
			0737ca506df7d4544f8512df35aaa07ed8a40691e96ae868c23320220fec6866
	*/

	ins := [8]PoseidonDomain{}
	copy(ins[0][:], must.One(hex.DecodeString("f743111428ecda9b74a18826f1cc62d48ae1510de17e58645997559f86442f31")))
	copy(ins[1][:], must.One(hex.DecodeString("197a6efa37086e3a37108f8460c52bc810e2781aff0091bfd7b74e3f6f752f02")))
	copy(ins[2][:], must.One(hex.DecodeString("01c2c12f7930bbb59e3b36dae1c694b33ac6b3f199ec32a8665fadb8338f4f21")))
	copy(ins[3][:], must.One(hex.DecodeString("bbe672a853f65cdb2563339ca61d6ad648473b4b4ce44bf3a48486424b865a2d")))
	copy(ins[4][:], must.One(hex.DecodeString("937d57295f488cea8099f5525958f1af39c6241614b917835292e8e2314cf908")))
	copy(ins[5][:], must.One(hex.DecodeString("3272570cd8ba38c1183629ca72d5e9149d76786ed22c1d591db3d2bccfa7d308")))
	copy(ins[6][:], must.One(hex.DecodeString("2d3fc90e11aaf601bd1beae80fc3aba15a9e1f886d8140533ce36cda3dd9f936")))
	copy(ins[7][:], must.One(hex.DecodeString("0a017e264d0728bfcd2edb3c6c60cc5a05a02af31a71cdd7bb2da27616f5f630")))

	expected := must.One(hex.DecodeString("0737ca506df7d4544f8512df35aaa07ed8a40691e96ae868c23320220fec6866"))

	out := poseidonHashMulti[poseidondst.Arity8](ins[:])

	fmt.Printf("out: %x\n", out[:])
	fmt.Printf("expected: %x\n", expected)

	if !bytes.Equal(out[:], expected) {
		t.Fatal("hash mismatch")
	}
}
