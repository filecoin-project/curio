package FWSS

import (
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/mod/semver"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func EnsureServiceTermination(tx *harmonydb.Tx, dataSetID int64) error {
	n, err := tx.Exec(`INSERT INTO pdp_delete_data_set (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, dataSetID)
	if err != nil {
		return xerrors.Errorf("failed to insert into pdp_delete_data_set: %w", err)
	}
	if n != 1 && n != 0 {
		return xerrors.Errorf("expected to insert 0 or 1 rows, inserted %d", n)
	}
	return nil
}

const clientTerminationNotSupportedVersion = "v1.2.1"

// SupportsClientTermination reports whether FWSS supports
// terminateService(uint256,bytes) for client-authorized termination.
func SupportsClientTermination(opts *bind.CallOpts, serviceAddr common.Address, ethClient bind.ContractCaller) (bool, error) {
	fwss, err := NewFilecoinWarmStorageServiceCaller(serviceAddr, ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to bind FWSS contract: %w", err)
	}

	version, err := fwss.VERSION(opts)
	if err != nil {
		return false, xerrors.Errorf("failed to get version: %w", err)
	}

	return fwssSupportsClientTerminationVersion(version), nil
}

func fwssSupportsClientTerminationVersion(version string) bool {
	return semver.Compare(fwssSemver(version), clientTerminationNotSupportedVersion) > 0
}

func fwssSemver(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
