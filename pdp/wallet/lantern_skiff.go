//go:build skiff

package wallet

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"

	lanternwallet "github.com/Reiers/lantern/wallet"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func CreatePDPKeyLantern(ctx context.Context, db *harmonydb.DB) (*CreatedKey, error) {
	repoPath := os.Getenv("CURIO_REPO_PATH")
	if repoPath == "" {
		repoPath = "~/.curio"
	}
	repoPath, err := homedir.Expand(repoPath)
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(repoPath, "lantern")

	w, err := lanternwallet.New(ctx, dataDir, "")
	if err != nil {
		return nil, xerrors.Errorf("lantern wallet: %w", err)
	}

	filAddr, err := w.NewAddress(ctx, lanternwallet.KTDelegated)
	if err != nil {
		return nil, xerrors.Errorf("creating delegated lantern address: %w", err)
	}

	ki, err := w.Export(ctx, filAddr)
	if err != nil {
		return nil, xerrors.Errorf("exporting lantern key: %w", err)
	}

	address, err := InsertPDPKey(ctx, db, ki.PrivateKey)
	if err != nil {
		return nil, err
	}

	return &CreatedKey{
		Address:       address,
		PrivateKeyHex: hex.EncodeToString(ki.PrivateKey),
		FilAddress:    filAddr.String(),
	}, nil
}
