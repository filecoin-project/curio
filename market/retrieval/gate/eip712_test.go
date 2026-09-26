package gate

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestVoucherSignRecoverRoundTrip(t *testing.T) {
	key, _ := crypto.GenerateKey()
	priv := crypto.FromECDSA(key)
	addr := crypto.PubkeyToAddress(key.PublicKey)
	chainID := big.NewInt(314159)
	vc := common.HexToAddress("0x00000000000000000000000000000000000000AA")

	v := RetrievalVoucher{Grantee: addr, Scope: 42, IssuedAt: 1000, Deadline: 2000}
	sig, err := SignVoucher(v, chainID, vc, priv)
	require.NoError(t, err)
	got, err := RecoverVoucherSigner(v, chainID, vc, sig)
	require.NoError(t, err)
	require.Equal(t, addr, got)

	v2 := v
	v2.Scope = 43
	got2, _ := RecoverVoucherSigner(v2, chainID, vc, sig)
	require.NotEqual(t, addr, got2)

	// verifyingContract domain separation
	got3, _ := RecoverVoucherSigner(v, chainID, common.HexToAddress("0x00000000000000000000000000000000000000BB"), sig)
	require.NotEqual(t, addr, got3)
	// chainId binding
	got4, _ := RecoverVoucherSigner(v, big.NewInt(1), vc, sig)
	require.NotEqual(t, addr, got4)
}

func TestProofSignRecoverRoundTrip(t *testing.T) {
	key, _ := crypto.GenerateKey()
	priv := crypto.FromECDSA(key)
	addr := crypto.PubkeyToAddress(key.PublicKey)
	chainID := big.NewInt(314159)
	vc := common.HexToAddress("0x00000000000000000000000000000000000000AA")

	p := RetrievalProof{Scope: 42, Resource: "bafkzcibxyz", Deadline: 9999}
	sig, err := SignProof(p, chainID, vc, priv)
	require.NoError(t, err)
	got, err := RecoverProofSigner(p, chainID, vc, sig)
	require.NoError(t, err)
	require.Equal(t, addr, got)

	p2 := p
	p2.Resource = "bafkzcibother"
	got2, _ := RecoverProofSigner(p2, chainID, vc, sig)
	require.NotEqual(t, addr, got2, "resource binding")
}
