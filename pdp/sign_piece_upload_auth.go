package pdp

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	curiobuild "github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/pdp/contract"
)

// StorageEIP712DomainForBuild returns the FWSS storage EIP-712 domain for the current build.
func StorageEIP712DomainForBuild() apitypes.TypedDataDomain {
	chainID := storageChainIDForBuild()
	chainIDTyped := ethmath.HexOrDecimal256(*chainID)
	return apitypes.TypedDataDomain{
		Name:              "FilecoinWarmStorageService",
		Version:           "1",
		ChainId:           &chainIDTyped,
		VerifyingContract: contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService.Hex(),
	}
}

func storageChainIDForBuild() *big.Int {
	switch curiobuild.BuildType {
	case curiobuild.BuildMainnet:
		return big.NewInt(314)
	case curiobuild.BuildCalibnet:
		return big.NewInt(314159)
	default:
		return big.NewInt(314159)
	}
}

// SignPieceUploadAuth signs a PieceUploadAuth message for pdptool and tests.
func SignPieceUploadAuth(key *ecdsa.PrivateKey, domain apitypes.TypedDataDomain, dataSetID uint64, nonce, expiry *big.Int) ([]byte, error) {
	auth := &PieceUploadAuth{
		DataSetID: dataSetID,
		Nonce:     nonce,
		Expiry:    expiry.Uint64(),
	}
	hash, err := PieceUploadAuthDigest(domain, auth)
	if err != nil {
		return nil, err
	}
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		return nil, err
	}
	if sig[64] < 27 {
		sig[64] += 27
	}
	return sig, nil
}

// NewPieceUploadAuthHeader signs and formats X-PDP-Upload-Auth for the current build domain.
func NewPieceUploadAuthHeader(key *ecdsa.PrivateKey, dataSetID uint64, nonce, expiry *big.Int) (string, error) {
	sig, err := SignPieceUploadAuth(key, StorageEIP712DomainForBuild(), dataSetID, nonce, expiry)
	if err != nil {
		return "", err
	}
	return FormatPieceUploadAuthHeader(dataSetID, nonce, expiry, sig), nil
}

// DefaultUploadAuthExpiry returns a short-lived expiry suitable for uploads.
func DefaultUploadAuthExpiry() *big.Int {
	return big.NewInt(time.Now().Add(time.Hour).Unix())
}

// ParseUploadAuthPrivateKey parses a secp256k1 private key hex string.
func ParseUploadAuthPrivateKey(hexKey string) (*ecdsa.PrivateKey, error) {
	key, err := crypto.HexToECDSA(trimHexPrefix(hexKey))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	return key, nil
}

func trimHexPrefix(s string) string {
	if len(s) >= 2 && s[0:2] == "0x" {
		return s[2:]
	}
	return s
}

// RecoverPieceUploadAuthSigner returns the signer for a signed auth (tests).
func RecoverPieceUploadAuthSigner(ctx context.Context, verifier *UploadAuthVerifier, auth *PieceUploadAuth) (common.Address, error) {
	return verifier.recoverSigner(ctx, auth)
}
