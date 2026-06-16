package wallet

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const rolePDP = "pdp"

type CreatedKey struct {
	Address       string `json:"address"`
	PrivateKeyHex string `json:"privateKeyHex"`
	FilAddress    string `json:"filAddress,omitempty"`
}

type Status struct {
	Configured bool   `json:"configured"`
	Address    string `json:"address,omitempty"`
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

func ImportPDPKeyHex(ctx context.Context, db *harmonydb.DB, hexPrivateKey string) (string, error) {
	normalized, err := NormalizeHexPrivateKey(hexPrivateKey)
	if err != nil {
		return "", err
	}
	privateKeyBytes, err := hex.DecodeString(normalized)
	if err != nil {
		return "", xerrors.Errorf("failed to decode private key: %w", err)
	}
	return InsertPDPKey(ctx, db, privateKeyBytes)
}

func CreatePDPKey(ctx context.Context, db *harmonydb.DB) (*CreatedKey, error) {
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
