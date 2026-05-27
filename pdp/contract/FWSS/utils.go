package FWSS

import (
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
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

const clientTerminationNotSupportedVersion = "1.2.0"

// SupportsClientTermination reports whether FWSS supports
// terminateService(uint256,bytes) for client-authorized termination.
func SupportsClientTermination(opts *bind.CallOpts, serviceAddr common.Address, ethClient ethchain.EthClient) (bool, error) {
	supported, err := VersionAfter(opts, serviceAddr, ethClient, clientTerminationNotSupportedVersion)
	if err != nil {
		return false, err
	}
	return supported, nil
}

func getFWSSVersion(opts *bind.CallOpts, serviceAddr common.Address, ethClient ethchain.EthClient) (string, error) {
	fwss, err := NewFilecoinWarmStorageServiceCaller(serviceAddr, ethClient)
	if err != nil {
		return "", xerrors.Errorf("failed to bind FWSS contract: %w", err)
	}
	version, err := fwss.VERSION(opts)
	return version, err
}

// VersionAtLeast reports whether the FWSS contract VERSION is greater than or equal to minimum.
func VersionAtLeast(opts *bind.CallOpts, serviceAddr common.Address, ethClient ethchain.EthClient, minimum string) (bool, error) {
	version, err := getFWSSVersion(opts, serviceAddr, ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to get FWSS version: %w", err)
	}
	return versionStringAtLeast(version, minimum)
}

// VersionAfter reports whether the FWSS contract VERSION is strictly greater than maximum.
func VersionAfter(opts *bind.CallOpts, serviceAddr common.Address, ethClient ethchain.EthClient, maximum string) (bool, error) {
	version, err := getFWSSVersion(opts, serviceAddr, ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to get FWSS version: %w", err)
	}
	return versionStringAfter(version, maximum)
}

func versionStringAtLeast(version, minimum string) (bool, error) {
	current, err := parseVersion(version)
	if err != nil {
		return false, xerrors.Errorf("invalid FWSS version %q: %w", version, err)
	}
	required, err := parseVersion(minimum)
	if err != nil {
		return false, xerrors.Errorf("invalid minimum FWSS version %q: %w", minimum, err)
	}
	for i := range current {
		if current[i] > required[i] {
			return true, nil
		}
		if current[i] < required[i] {
			return false, nil
		}
	}
	return true, nil
}

func versionStringAfter(version, maximum string) (bool, error) {
	current, err := parseVersion(version)
	if err != nil {
		return false, xerrors.Errorf("invalid FWSS version %q: %w", version, err)
	}
	required, err := parseVersion(maximum)
	if err != nil {
		return false, xerrors.Errorf("invalid maximum FWSS version %q: %w", maximum, err)
	}
	for i := range current {
		if current[i] > required[i] {
			return true, nil
		}
		if current[i] < required[i] {
			return false, nil
		}
	}
	return false, nil
}

func parseVersion(version string) ([3]int, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return [3]int{}, xerrors.Errorf("expected major.minor.patch")
	}
	var parsed [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, err
		}
		parsed[i] = n
	}
	return parsed, nil
}
