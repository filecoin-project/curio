package contract

import (
	"context"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"golang.org/x/xerrors"
)

// This file is hand-maintained, unlike the generated PDPVerifier bindings. It
// carries the removal-queue entrypoint and custom errors added by
// https://github.com/FilOzone/pdp/pull/297, which are not yet in the checked-in
// ABI. Delete it and use the generated bindings once PDPVerifier.abi is
// regenerated against a release containing processPieceDeletions.
const removalQueueABIJSON = `[
	{"type":"function","name":"processPieceDeletions","inputs":[{"name":"setId","type":"uint256","internalType":"uint256"},{"name":"removalCount","type":"uint256","internalType":"uint256"}],"outputs":[],"stateMutability":"nonpayable"},
	{"type":"error","name":"OnlyStorageProvider","inputs":[]},
	{"type":"error","name":"InvalidPieceDeletionBatch","inputs":[]},
	{"type":"error","name":"EmptyRemovalBatch","inputs":[]},
	{"type":"error","name":"PendingPieceDeletions","inputs":[{"name":"count","type":"uint256","internalType":"uint256"}]},
	{"type":"error","name":"NoPiecesToProve","inputs":[]}
]`

var removalQueueABI abi.ABI

func init() {
	parsed, err := abi.JSON(strings.NewReader(removalQueueABIJSON))
	if err != nil {
		panic("failed to parse PDPVerifier removal-queue ABI fragment: " + err.Error())
	}
	removalQueueABI = parsed
}

// RemovalQueueABI exposes the hand-maintained fragment so callers can resolve
// the new custom errors for revert classification.
func RemovalQueueABI() *abi.ABI {
	return &removalQueueABI
}

// PackProcessPieceDeletions builds calldata draining removalCount entries from
// the tail of a data set's scheduled-removal queue.
func PackProcessPieceDeletions(setID, removalCount *big.Int) ([]byte, error) {
	data, err := removalQueueABI.Pack("processPieceDeletions", setID, removalCount)
	if err != nil {
		return nil, xerrors.Errorf("packing processPieceDeletions: %w", err)
	}
	return data, nil
}

// PieceDeletionProcessingMinVersion is the lowest PDPVerifier VERSION that
// exposes processPieceDeletions. Below it, nextProvingPeriod still drains the
// removal queue itself and Curio must not attempt to drain it separately.
//
// TODO: confirm against the release that lands FilOzone/pdp#297. The currently
// deployed contract reports "3.4.0"; this is a placeholder for the next bump.
const PieceDeletionProcessingMinVersion = "3.5.0"

// SupportsPieceDeletionProcessing reports whether the deployed PDPVerifier
// exposes processPieceDeletions.
//
// The value is read rather than cached for the process lifetime: PDPVerifier is
// a UUPS proxy and can be upgraded in place underneath a running Curio.
func SupportsPieceDeletionProcessing(ctx context.Context, verifier *PDPVerifier) (bool, error) {
	version, err := verifier.VERSION(EthCallOpts(ctx))
	if err != nil {
		return false, xerrors.Errorf("reading PDPVerifier VERSION: %w", err)
	}

	cmp, err := compareSemver(version, PieceDeletionProcessingMinVersion)
	if err != nil {
		return false, xerrors.Errorf("comparing PDPVerifier version %q: %w", version, err)
	}
	return cmp >= 0, nil
}

// compareSemver orders two dotted version strings, returning -1, 0 or 1. Any
// pre-release or build suffix is ignored: "3.5.0-rc1" compares equal to "3.5.0",
// which is the conservative choice for a feature gate on a release candidate.
func compareSemver(a, b string) (int, error) {
	aParts, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	bParts, err := parseSemver(b)
	if err != nil {
		return 0, err
	}

	for i := range aParts {
		switch {
		case aParts[i] < bParts[i]:
			return -1, nil
		case aParts[i] > bParts[i]:
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemver(v string) ([3]int, error) {
	var out [3]int

	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}

	fields := strings.Split(v, ".")
	if len(fields) == 0 || len(fields) > 3 {
		return out, xerrors.Errorf("malformed version %q", v)
	}

	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return out, xerrors.Errorf("malformed version %q: component %q is not a number", v, field)
		}
		if n < 0 {
			return out, xerrors.Errorf("malformed version %q: negative component", v)
		}
		out[i] = n
	}
	return out, nil
}
