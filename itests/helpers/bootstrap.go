package helpers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/go-units"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/createminer"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"
)

func BootstrapNetwork(t *testing.T, ctx context.Context, opts ...interface{}) (*kit.TestFullNode, *kit.TestMiner, *kit.Ensemble, string) {

	finalOpts := append([]interface{}{
		kit.LatestActorsAt(-1),
		kit.PresealSectors(32),
		kit.ThroughRPC(),
	}, opts...)

	full, miner, esemble := kit.EnsembleMinimal(t, finalOpts...)

	esemble.Start()
	blockTime := 100 * time.Millisecond
	esemble.BeginMining(blockTime)

	full.WaitTillChain(ctx, kit.HeightAtLeast(15))

	err := miner.LogSetLevel(ctx, "*", "ERROR")
	require.NoError(t, err)

	err = full.LogSetLevel(ctx, "*", "ERROR")
	require.NoError(t, err)

	fapi, err := FullNodeAPIInfo(ctx, full, full.ListenAddr)
	require.NoError(t, err)

	require.NoError(t, miner.LogSetLevel(ctx, "miner", "ERROR"))
	require.NoError(t, miner.LogSetLevel(ctx, "wdpost", "ERROR"))
	require.NoError(t, miner.LogSetLevel(ctx, "advmgr", "ERROR"))

	require.NoError(t, full.LogSetLevel(ctx, "chain", "ERROR"))
	require.NoError(t, full.LogSetLevel(ctx, "consensus-common", "ERROR"))
	require.NoError(t, full.LogSetLevel(ctx, "sub", "ERROR"))
	require.NoError(t, full.LogSetLevel(ctx, "chainstore", "ERROR"))
	require.NoError(t, full.LogSetLevel(ctx, "messagepool", "ERROR"))
	require.NoError(t, full.LogSetLevel(ctx, "fullnode", "ERROR"))

	return full, miner, esemble, fapi
}

func SectorSizeFromString(size string) (abi.SectorSize, error) {
	sectorSizeInt, err := units.RAMInBytes(size)
	if err != nil {
		return 0, xerrors.Errorf("parse sector size %q: %w", size, err)
	}
	return abi.SectorSize(sectorSizeInt), nil
}

func FullNodeAPIInfo(ctx context.Context, full v1api.FullNode, listenAddr multiaddr.Multiaddr) (string, error) {
	token, err := full.AuthNew(ctx, lapi.AllPermissions)
	if err != nil {
		return "", xerrors.Errorf("create fullnode auth token: %w", err)
	}
	return fmt.Sprintf("%s:%s", string(token), listenAddr.String()), nil
}

func BootstrapNetworkWithNewMiner(t *testing.T, ctx context.Context, minerSize string, opts ...interface{}) (*kit.TestFullNode, *kit.TestMiner, *harmonydb.DB, address.Address) {
	full, miner, _, fapi := BootstrapNetwork(t, ctx, opts...)

	sharedITestID := harmonydb.ITestNewID()
	t.Logf("sharedITestID: %s", sharedITestID)

	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID, true)
	require.NoError(t, err)

	var titles []string
	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	require.NoError(t, err)
	require.NotEmpty(t, titles)
	require.NotContains(t, titles, "base")

	addr := miner.OwnerKey.Address

	maddr, err := CreateStorageMinerAndConfig(ctx, full, db, addr, minerSize, 0, fapi)
	require.NoError(t, err)

	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	require.NoError(t, err)
	require.Contains(t, titles, "base")
	baseCfg, err := LoadBaseConfigFromDB(ctx, db)
	require.NoError(t, err)

	require.NotNil(t, baseCfg.Addresses)
	require.GreaterOrEqual(t, len(baseCfg.Addresses.Get()), 1)

	require.Contains(t, baseCfg.Addresses.Get()[0].MinerAddresses, maddr.String())

	return full, miner, db, maddr
}

func CreateStorageMinerAndConfig(
	ctx context.Context,
	full v1api.FullNode,
	db *harmonydb.DB,
	owner address.Address,
	sectorSize string,
	confidence uint64,
	fapi string,
) (address.Address, error) {
	maddr, err := CreateStorageMiner(ctx, full, owner, sectorSize, confidence)
	if err != nil {
		return address.Undef, err
	}

	if err := deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, fapi); err != nil {
		return address.Undef, xerrors.Errorf("create miner config: %w", err)
	}

	return maddr, nil
}

func CreateStorageMiner(
	ctx context.Context,
	full v1api.FullNode,
	owner address.Address,
	sectorSize string,
	confidence uint64,
) (address.Address, error) {
	ss, err := SectorSizeFromString(sectorSize)
	if err != nil {
		return address.Undef, xerrors.Errorf("parse sector size %q: %w", sectorSize, err)
	}

	maddr, err := createminer.CreateStorageMiner(ctx, full, owner, owner, owner, ss, confidence)
	if err != nil {
		return address.Undef, xerrors.Errorf("create storage miner: %w", err)
	}
	return maddr, nil
}
