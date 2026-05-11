package mk20

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	fcrypto "github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/lib/sigs"
)

const Authprefix = "CurioAuth "

// Auth verifies the custom authentication header by parsing its contents and validating the signature using the provided database connection.
func Auth(header string, requestMethod string, requestPath string, db *harmonydb.DB, cfg *config.CurioConfig) (bool, string, error) {
	keyType, pubKey, sig, err := parseCustomAuth(header)
	if err != nil {
		return false, "", xerrors.Errorf("parsing auth header: %w", err)
	}
	return verifySignature(db, keyType, pubKey, sig, requestMethod, requestPath, cfg)
}

func parseCustomAuth(header string) (keyType string, pubKey, sig []byte, err error) {
	if !strings.HasPrefix(header, Authprefix) {
		return "", nil, nil, errors.New("missing CustomAuth prefix")
	}

	parts := strings.SplitN(strings.TrimPrefix(header, Authprefix), ":", 3)
	if len(parts) != 3 {
		return "", nil, nil, errors.New("invalid auth format")
	}

	keyType = parts[0]
	pubKey, err = base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid pubkey base64: %w", err)
	}

	if len(pubKey) == 0 {
		return "", nil, nil, fmt.Errorf("invalid pubkey")
	}

	sig, err = base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid signature base64: %w", err)
	}

	if len(sig) == 0 {
		return "", nil, nil, fmt.Errorf("invalid signature")
	}

	return keyType, pubKey, sig, nil
}

func verifySignature(db *harmonydb.DB, keyType string, pubKey, signature []byte, requestMethod string, requestPath string, cfg *config.CurioConfig) (bool, string, error) {
	msg := authMessage(pubKey, requestMethod, requestPath, time.Now().UTC().Truncate(time.Minute))

	switch keyType {
	case "secp256k1", "bls", "delegated":
		return verifyFilSignature(db, pubKey, signature, msg, cfg)
	default:
		return false, "", fmt.Errorf("unsupported key type: %s", keyType)
	}
}

func authMessage(pubKey []byte, requestMethod string, requestPath string, timestamp time.Time) [32]byte {
	requestMethod = strings.ToUpper(requestMethod)
	if requestPath == "" {
		requestPath = "/"
	}
	return sha256.Sum256(bytes.Join([][]byte{
		pubKey,
		[]byte(requestMethod),
		[]byte(requestPath),
		[]byte(timestamp.UTC().Truncate(time.Minute).Format(time.RFC3339)),
	}, []byte{}))
}

func verifyFilSignature(db *harmonydb.DB, pubKey, signature []byte, msgs [32]byte, cfg *config.CurioConfig) (bool, string, error) {
	signs := &fcrypto.Signature{}
	err := signs.UnmarshalBinary(signature)
	if err != nil {
		return false, "", xerrors.Errorf("invalid signature")
	}
	addr, err := address.NewFromBytes(pubKey)
	if err != nil {
		return false, "", xerrors.Errorf("invalid filecoin pubkey")
	}

	err = sigs.Verify(signs, addr, msgs[:])
	if err != nil {
		return false, "", errors.New("invalid signature")
	}

	allowed, err := clientAllowed(context.Background(), db, addr.String(), cfg)
	if err != nil {
		return false, "", xerrors.Errorf("checking client allowed: %w", err)
	}
	if !allowed {
		return false, "", nil
	}
	return true, addr.String(), nil
}

func AuthenticateClient(db *harmonydb.DB, id, client string) (bool, error) {
	var allowed bool
	err := db.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM market_mk20_deal WHERE id = $1 AND client = $2)`, id, client).Scan(&allowed)
	if err != nil {
		return false, xerrors.Errorf("querying client: %w", err)
	}
	return allowed, nil
}

func clientAllowed(ctx context.Context, db *harmonydb.DB, client string, cfg *config.CurioConfig) (bool, error) {
	var allowed bool
	err := db.QueryRow(ctx, `SELECT allowed FROM market_mk20_clients WHERE client = $1`, client).Scan(&allowed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Client is not in the database
			return !cfg.Market.StorageMarketConfig.MK20.DenyUnknownClients, nil
		}
		return false, xerrors.Errorf("querying client: %w", err)
	}

	return allowed, nil
}
