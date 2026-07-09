package wallet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/types"
)

const rolePDP = "pdp"

type CreatedKey struct {
	Address       string `json:"address"`
	PrivateKeyHex string `json:"privateKeyHex"`
	FilAddress    string `json:"filAddress,omitempty"`
}

type Status struct {
	Configured  bool   `json:"configured"`
	Address     string `json:"address,omitempty"`
	FilAddress  string `json:"filAddress,omitempty"`
	Balance     string `json:"balance,omitempty"`
	Funded      bool   `json:"funded"`
	ActorExists bool   `json:"actorExists"`
}

// ParsePrivateKeyMaterial accepts a hex secp256k1 private key or a lotus wallet export
// (hex-encoded KeyInfo JSON, or KeyInfo JSON itself) and returns the 32-byte private key.
func ParsePrivateKeyMaterial(input string) ([]byte, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, xerrors.Errorf("private key cannot be empty")
	}

	// Lotus wallet export is hex-encoded KeyInfo JSON.
	if decoded, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(input, "0x"), "0X")); err == nil {
		if len(decoded) == 32 {
			return decoded, nil
		}
		if key, err := keyInfoPrivateKey(decoded); err == nil {
			return key, nil
		}
	}

	// Raw KeyInfo JSON (e.g. after xxd -r -p on lotus wallet export).
	if key, err := keyInfoPrivateKey([]byte(input)); err == nil {
		return key, nil
	}

	// Plain hex private key (with or without 0x).
	normalized, err := NormalizeHexPrivateKey(input)
	if err != nil {
		return nil, xerrors.Errorf("unrecognized private key format (expected hex key or lotus wallet export): %w", err)
	}
	return hex.DecodeString(normalized)
}

func keyInfoPrivateKey(raw []byte) ([]byte, error) {
	var ki types.KeyInfo
	if err := json.Unmarshal(raw, &ki); err != nil {
		return nil, err
	}
	if len(ki.PrivateKey) == 0 {
		return nil, xerrors.Errorf("key info has empty private key")
	}
	// Delegated/secp256k1 keys are 32 bytes; tolerate longer encodings by taking the last 32.
	if len(ki.PrivateKey) == 32 {
		return ki.PrivateKey, nil
	}
	if len(ki.PrivateKey) > 32 {
		return ki.PrivateKey[len(ki.PrivateKey)-32:], nil
	}
	return nil, xerrors.Errorf("private key length %d is too short", len(ki.PrivateKey))
}

func NormalizeHexPrivateKey(hexPrivateKey string) (string, error) {
	hexPrivateKey = strings.TrimSpace(hexPrivateKey)
	hexPrivateKey = strings.TrimPrefix(hexPrivateKey, "0x")
	hexPrivateKey = strings.TrimPrefix(hexPrivateKey, "0X")
	if hexPrivateKey == "" {
		return "", xerrors.Errorf("private key cannot be empty")
	}
	if _, err := hex.DecodeString(hexPrivateKey); err != nil {
		return "", xerrors.Errorf("failed to decode private key: %w", err)
	}
	return hexPrivateKey, nil
}

func AddressFromPrivateKeyBytes(privateKeyBytes []byte) (string, error) {
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return "", xerrors.Errorf("invalid private key: %w", err)
	}
	return crypto.PubkeyToAddress(privateKey.PublicKey).Hex(), nil
}

func HasPDPKey(ctx context.Context, db *harmonydb.DB) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM eth_keys WHERE role = $1)`, rolePDP).Scan(&exists)
	return exists, err
}

func PDPKeyStatus(ctx context.Context, db *harmonydb.DB) (Status, error) {
	var address string
	err := db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = $1 LIMIT 1`, rolePDP).Scan(&address)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Status{Configured: false}, nil
		}
		return Status{}, err
	}
	return Status{Configured: true, Address: address}, nil
}

func InsertPDPKey(ctx context.Context, db *harmonydb.DB, privateKeyBytes []byte) (string, error) {
	address, err := AddressFromPrivateKeyBytes(privateKeyBytes)
	if err != nil {
		return "", err
	}

	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var exists bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM eth_keys WHERE role = $1)`, rolePDP).Scan(&exists); err != nil {
			return false, xerrors.Errorf("checking existing pdp key: %w", err)
		}
		if exists {
			return false, xerrors.Errorf("a PDP wallet is already configured")
		}
		if _, err := tx.Exec(`INSERT INTO eth_keys (address, private_key, role) VALUES ($1, $2, $3)`, address, privateKeyBytes, rolePDP); err != nil {
			return false, xerrors.Errorf("inserting pdp key: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return "", err
	}
	return address, nil
}

func ImportPDPKeyHex(ctx context.Context, db *harmonydb.DB, keyMaterial string) (string, error) {
	privateKeyBytes, err := ParsePrivateKeyMaterial(keyMaterial)
	if err != nil {
		return "", err
	}
	return InsertPDPKey(ctx, db, privateKeyBytes)
}

func CreatePDPKey(ctx context.Context, db *harmonydb.DB) (*CreatedKey, error) {
	if created, err := createPDPKeyLocal(ctx, db); err != nil {
		return nil, err
	} else if created != nil {
		return created, nil
	}

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, xerrors.Errorf("generating private key: %w", err)
	}
	privateKeyBytes := crypto.FromECDSA(privateKey)
	address, err := InsertPDPKey(ctx, db, privateKeyBytes)
	if err != nil {
		return nil, err
	}
	return &CreatedKey{
		Address:       address,
		PrivateKeyHex: hex.EncodeToString(privateKeyBytes),
	}, nil
}
