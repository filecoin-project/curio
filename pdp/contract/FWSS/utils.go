package FWSS

import (
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/mod/semver"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/ethchain"
)

const clientTerminationNotSupportedVersion = "v1.2.1"

// SupportsClientTermination reports whether FWSS supports
// terminateService(uint256,bytes) for client-authorized termination.
func SupportsClientTermination(opts *bind.CallOpts, serviceAddr common.Address, ethClient ethchain.EthClient) (bool, error) {
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
