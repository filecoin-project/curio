package pdp

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

const (
	// PieceUploadAuthHeader carries a dot-separated wallet proof:
	// dataSetId.nonce.expiry.0xSignature
	PieceUploadAuthHeader = "X-PDP-Upload-Auth"

	// addPiecesPermission matches AddPiecesPermission in synapse-core session-key/permissions.ts
	addPiecesPermission = "0x954bdc254591a7eab1b73f03842464d9283a08352772737094d710a4428fd183"
)

var errUploadAuthInvalid = errors.New("invalid upload auth")

// PieceUploadAuth carries the decoded wallet proof from X-PDP-Upload-Auth.
type PieceUploadAuth struct {
	DataSetID uint64
	Nonce     *big.Int
	Expiry    uint64
	Signature []byte
}

type cachedSessionAuth struct {
	expiry   time.Time
	valid    bool
	cachedAt time.Time
}

// UploadAuthVerifier validates PieceUploadAuth EIP-712 signatures.
type UploadAuthVerifier struct {
	ethClient ethchain.EthClient
	db        *harmonydb.DB
	validator AddPiecesValidator

	domainOnce sync.Once
	domain     apitypes.TypedDataDomain
	domainErr  error

	sessionCache   map[string]cachedSessionAuth
	sessionCacheMu sync.Mutex
}

func NewUploadAuthVerifier(ethClient ethchain.EthClient, db *harmonydb.DB, validator AddPiecesValidator) *UploadAuthVerifier {
	return &UploadAuthVerifier{
		ethClient:    ethClient,
		db:           db,
		validator:    validator,
		sessionCache: make(map[string]cachedSessionAuth),
	}
}

func fwssEIP712Types() apitypes.Types {
	return apitypes.Types{
		"EIP712Domain": {
			{Name: "name", Type: "string"},
			{Name: "version", Type: "string"},
			{Name: "chainId", Type: "uint256"},
			{Name: "verifyingContract", Type: "address"},
		},
		"MetadataEntry": {
			{Name: "key", Type: "string"},
			{Name: "value", Type: "string"},
		},
		"CreateDataSet": {
			{Name: "clientDataSetId", Type: "uint256"},
			{Name: "payee", Type: "address"},
			{Name: "metadata", Type: "MetadataEntry[]"},
		},
		"Cid": {
			{Name: "data", Type: "bytes"},
		},
		"PieceMetadata": {
			{Name: "pieceIndex", Type: "uint256"},
			{Name: "metadata", Type: "MetadataEntry[]"},
		},
		"AddPieces": {
			{Name: "clientDataSetId", Type: "uint256"},
			{Name: "nonce", Type: "uint256"},
			{Name: "pieceData", Type: "Cid[]"},
			{Name: "pieceMetadata", Type: "PieceMetadata[]"},
		},
		"SchedulePieceRemovals": {
			{Name: "clientDataSetId", Type: "uint256"},
			{Name: "pieceIds", Type: "uint256[]"},
		},
		"TerminateService": {
			{Name: "dataSetId", Type: "uint256"},
		},
		"PieceUploadAuth": {
			{Name: "dataSetId", Type: "uint256"},
			{Name: "nonce", Type: "uint256"},
			{Name: "expiry", Type: "uint256"},
		},
		"Permit": {
			{Name: "owner", Type: "address"},
			{Name: "spender", Type: "address"},
			{Name: "value", Type: "uint256"},
			{Name: "nonce", Type: "uint256"},
			{Name: "deadline", Type: "uint256"},
		},
	}
}

func (v *UploadAuthVerifier) storageDomain(ctx context.Context) (apitypes.TypedDataDomain, error) {
	v.domainOnce.Do(func() {
		fwssAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
		fwss, err := FWSS.NewFilecoinWarmStorageService(fwssAddr, v.ethClient)
		if err != nil {
			v.domainErr = xerrors.Errorf("bind FWSS contract: %w", err)
			return
		}
		d, err := fwss.Eip712Domain(contract.EthCallOpts(ctx))
		if err != nil {
			v.domainErr = xerrors.Errorf("read FWSS eip712 domain: %w", err)
			return
		}
		chainID := ethmath.HexOrDecimal256(*d.ChainId)
		v.domain = apitypes.TypedDataDomain{
			Name:              d.Name,
			Version:           d.Version,
			ChainId:           &chainID,
			VerifyingContract: d.VerifyingContract.Hex(),
		}
	})
	return v.domain, v.domainErr
}

// ParsePieceUploadAuthHeader decodes the X-PDP-Upload-Auth header value.
func ParsePieceUploadAuthHeader(raw string) (*PieceUploadAuth, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty header", errUploadAuthInvalid)
	}

	parts := strings.SplitN(raw, ".", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("%w: expected dataSetId.nonce.expiry.signature", errUploadAuthInvalid)
	}

	dataSetID, err := parseUint64Decimal(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid dataSetId: %v", errUploadAuthInvalid, err)
	}
	nonce, ok := new(big.Int).SetString(parts[1], 10)
	if !ok {
		return nil, fmt.Errorf("%w: invalid nonce", errUploadAuthInvalid)
	}
	expiry, err := parseUint64Decimal(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid expiry: %v", errUploadAuthInvalid, err)
	}

	sigBytes, err := hex.DecodeString(strings.TrimPrefix(parts[3], "0x"))
	if err != nil || len(sigBytes) != 65 {
		return nil, fmt.Errorf("%w: invalid signature", errUploadAuthInvalid)
	}

	return &PieceUploadAuth{
		DataSetID: dataSetID,
		Nonce:     nonce,
		Expiry:    expiry,
		Signature: sigBytes,
	}, nil
}

func parseUint64Decimal(raw string) (uint64, error) {
	v, ok := new(big.Int).SetString(raw, 10)
	if !ok || v.Sign() < 0 || !v.IsUint64() {
		return 0, fmt.Errorf("not a valid uint64")
	}
	return v.Uint64(), nil
}

func pieceUploadAuthTypedData(domain apitypes.TypedDataDomain, auth *PieceUploadAuth) apitypes.TypedData {
	return apitypes.TypedData{
		Types:       fwssEIP712Types(),
		PrimaryType: "PieceUploadAuth",
		Domain:      domain,
		Message: apitypes.TypedDataMessage{
			"dataSetId": new(big.Int).SetUint64(auth.DataSetID),
			"nonce":     auth.Nonce,
			"expiry":     new(big.Int).SetUint64(auth.Expiry),
		},
	}
}

func (v *UploadAuthVerifier) recoverSigner(ctx context.Context, auth *PieceUploadAuth) (common.Address, error) {
	domain, err := v.storageDomain(ctx)
	if err != nil {
		return common.Address{}, err
	}

	hash, _, err := apitypes.TypedDataAndHash(pieceUploadAuthTypedData(domain, auth))
	if err != nil {
		return common.Address{}, xerrors.Errorf("hash typed data: %w", err)
	}

	sig := make([]byte, 65)
	copy(sig, auth.Signature)
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return common.Address{}, fmt.Errorf("%w: recover signer: %v", errUploadAuthInvalid, err)
	}

	return crypto.PubkeyToAddress(*pubKey), nil
}

func (v *UploadAuthVerifier) Verify(ctx context.Context, auth *PieceUploadAuth) error {
	if auth == nil {
		return fmt.Errorf("%w: missing auth", errUploadAuthInvalid)
	}
	if auth.DataSetID == 0 {
		return fmt.Errorf("%w: dataSetId must be non-zero", errUploadAuthInvalid)
	}
	now := uint64(time.Now().Unix())
	if auth.Expiry < now {
		return fmt.Errorf("%w: expired", errUploadAuthInvalid)
	}

	signer, err := v.recoverSigner(ctx, auth)
	if err != nil {
		return err
	}

	payer, err := getDataSetPayer(ctx, v.db, v.validator, auth.DataSetID)
	if err != nil {
		return fmt.Errorf("%w: unknown data set payer: %v", errUploadAuthInvalid, err)
	}

	if signer == payer {
		return nil
	}

	ok, err := v.sessionKeyAuthorized(ctx, payer, signer)
	if err != nil {
		return fmt.Errorf("%w: session key lookup failed: %v", errUploadAuthInvalid, err)
	}
	if !ok {
		return fmt.Errorf("%w: signer not payer or authorized session key", errUploadAuthInvalid)
	}
	return nil
}

func (v *UploadAuthVerifier) sessionKeyAuthorized(ctx context.Context, payer, sessionKey common.Address) (bool, error) {
	cacheKey := payer.Hex() + ":" + sessionKey.Hex()
	now := time.Now()

	v.sessionCacheMu.Lock()
	if cached, ok := v.sessionCache[cacheKey]; ok && now.Sub(cached.cachedAt) < 30*time.Second {
		v.sessionCacheMu.Unlock()
		return cached.valid && cached.expiry.After(now), nil
	}
	v.sessionCacheMu.Unlock()

	expiry, err := authorizationExpiry(ctx, v.ethClient, payer, sessionKey, common.HexToHash(addPiecesPermission))
	if err != nil {
		return false, err
	}

	valid := expiry.After(now)
	v.sessionCacheMu.Lock()
	v.sessionCache[cacheKey] = cachedSessionAuth{
		expiry:   expiry,
		valid:    valid,
		cachedAt: now,
	}
	v.sessionCacheMu.Unlock()

	return valid, nil
}

func getDataSetPayer(ctx context.Context, db *harmonydb.DB, validator AddPiecesValidator, dataSetID uint64) (common.Address, error) {
	var payerStr *string
	err := db.QueryRow(ctx, `
		SELECT payer_address FROM pdp_data_sets WHERE id = $1
	`, dataSetID).Scan(&payerStr)
	if err != nil {
		return common.Address{}, fmt.Errorf("lookup data set %d: %w", dataSetID, err)
	}
	if payerStr != nil && *payerStr != "" {
		addr := common.HexToAddress(*payerStr)
		if addr != (common.Address{}) {
			return addr, nil
		}
	}

	if validator == nil {
		return common.Address{}, fmt.Errorf("payer not cached and no validator configured")
	}

	payer, err := validator.GetDataSetPayer(ctx, dataSetID)
	if err != nil {
		return common.Address{}, err
	}

	_, err = db.Exec(ctx, `
		UPDATE pdp_data_sets SET payer_address = $1 WHERE id = $2 AND (payer_address IS NULL OR payer_address = '')
	`, strings.ToLower(payer.Hex()), dataSetID)
	if err != nil {
		log.Warnw("failed to cache data set payer address", "dataSetId", dataSetID, "error", err)
	}

	return payer, nil
}

// FormatPieceUploadAuthHeader builds the header value for tests and pdptool.
func FormatPieceUploadAuthHeader(dataSetID uint64, nonce, expiry *big.Int, signature []byte) string {
	sigHex := "0x" + common.Bytes2Hex(signature)
	return fmt.Sprintf("%d.%s.%s.%s", dataSetID, nonce.String(), expiry.String(), sigHex)
}

// PieceUploadAuthDigest returns the EIP-712 digest for golden-vector cross-checks.
func PieceUploadAuthDigest(domain apitypes.TypedDataDomain, auth *PieceUploadAuth) ([]byte, error) {
	hash, _, err := apitypes.TypedDataAndHash(pieceUploadAuthTypedData(domain, auth))
	return hash, err
}
