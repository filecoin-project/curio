package itests

//import (
//	"context"
//	"fmt"
//	"os"
//	"testing"
//	"time"
//
//	"github.com/docker/go-units"
//	logging "github.com/ipfs/go-log/v2"
//	"github.com/stretchr/testify/require"
//	"golang.org/x/xerrors"
//
//	"github.com/filecoin-project/go-address"
//	"github.com/filecoin-project/go-state-types/abi"
//
//	"github.com/filecoin-project/curio/deps"
//	"github.com/filecoin-project/curio/deps/config"
//	"github.com/filecoin-project/curio/harmony/harmonydb"
//	"github.com/filecoin-project/curio/tasks/seal"
//
//	"github.com/filecoin-project/lotus/api"
//	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
//	"github.com/filecoin-project/lotus/chain/types"
//	"github.com/filecoin-project/lotus/cli/spcli"
//	"github.com/filecoin-project/lotus/itests/kit"
//)
//
//func TestCurioNewActor(t *testing.T) {
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	_ = logging.SetLogLevel("*", "DEBUG")
//
//	full, miner, ensemble := kit.EnsembleMinimal(t,
//		kit.LatestActorsAt(-1),
//		kit.MockProofs(),
//		kit.WithSectorIndexDB(),
//	)
//
//	ensemble.Start()
//	blockTime := 100 * time.Millisecond
//	ensemble.BeginMiningMustPost(blockTime)
//
//	addr := miner.OwnerKey.Address
//	sectorSizeInt, err := units.RAMInBytes("8MiB")
//	require.NoError(t, err)
//
//	sharedITestID := harmonydb.ITestNewID()
//	dbConfig := config.HarmonyDB{
//		Hosts:    []string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
//		Database: "yugabyte",
//		Username: "yugabyte",
//		Password: "yugabyte",
//		Port:     "5433",
//	}
//	db, err := harmonydb.NewFromConfigWithITestID(t, dbConfig, sharedITestID)
//	require.NoError(t, err)
//
//	var titles []string
//	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
//	require.NoError(t, err)
//	require.NotEmpty(t, titles)
//	require.NotContains(t, titles, "base")
//
//	maddr, err := spcli.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0)
//	require.NoError(t, err)
//
//	err = deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, "FULL NODE API STRING")
//	require.NoError(t, err)
//
//	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
//	require.NoError(t, err)
//	require.Contains(t, titles, "base")
//	baseCfg := config.DefaultCurioConfig()
//	var baseText string
//
//	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
//	require.NoError(t, err)
//	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
//	require.NoError(t, err)
//
//	require.NotNil(t, baseCfg.Addresses)
//	require.GreaterOrEqual(t, len(baseCfg.Addresses), 1)
//
//	require.Contains(t, baseCfg.Addresses[0].MinerAddresses, maddr.String())
//}
//
//func TestCurioHappyPath(t *testing.T) {
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	full, miner, esemble := kit.EnsembleMinimal(t,
//		kit.LatestActorsAt(-1),
//		kit.WithSectorIndexDB(),
//		kit.PresealSectors(32),
//		kit.ThroughRPC(),
//	)
//
//	esemble.Start()
//	blockTime := 100 * time.Millisecond
//	esemble.BeginMining(blockTime)
//
//	full.WaitTillChain(ctx, kit.HeightAtLeast(15))
//
//	err := miner.LogSetLevel(ctx, "*", "ERROR")
//	require.NoError(t, err)
//
//	err = full.LogSetLevel(ctx, "*", "ERROR")
//	require.NoError(t, err)
//
//	token, err := full.AuthNew(ctx, api.AllPermissions)
//	require.NoError(t, err)
//
//	fapi := fmt.Sprintf("%s:%s", string(token), full.ListenAddr)
//
//	sharedITestID := harmonydb.ITestNewID()
//	dbConfig := config.HarmonyDB{
//		Hosts:    []string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
//		Database: "yugabyte",
//		Username: "yugabyte",
//		Password: "yugabyte",
//		Port:     "5433",
//	}
//	db, err := harmonydb.NewFromConfigWithITestID(t, dbConfig, sharedITestID)
//	require.NoError(t, err)
//
//	defer db.ITestDeleteAll()
//
//	var titles []string
//	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
//	require.NoError(t, err)
//	require.NotEmpty(t, titles)
//	require.NotContains(t, titles, "base")
//
//	addr := miner.OwnerKey.Address
//	sectorSizeInt, err := units.RAMInBytes("2KiB")
//	require.NoError(t, err)
//
//	maddr, err := spcli.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0)
//	require.NoError(t, err)
//
//	err = deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, fapi)
//	require.NoError(t, err)
//
//	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
//	require.NoError(t, err)
//	require.Contains(t, titles, "base")
//	baseCfg := config.DefaultCurioConfig()
//	var baseText string
//
//	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
//	require.NoError(t, err)
//	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
//	require.NoError(t, err)
//
//	require.NotNil(t, baseCfg.Addresses)
//	require.GreaterOrEqual(t, len(baseCfg.Addresses), 1)
//
//	require.Contains(t, baseCfg.Addresses[0].MinerAddresses, maddr.String())
//
//	temp := os.TempDir()
//	dir, err := os.MkdirTemp(temp, "curio")
//	require.NoError(t, err)
//	defer func() {
//		_ = os.Remove(dir)
//	}()
//
//	capi, enginerTerm, closure, finishCh := ConstructCurioTest(ctx, t, dir, db, full, maddr, baseCfg)
//	defer enginerTerm()
//	defer closure()
//
//	mid, err := address.IDFromAddress(maddr)
//	require.NoError(t, err)
//
//	mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
//	require.NoError(t, err)
//
//	nv, err := full.StateNetworkVersion(ctx, types.EmptyTSK)
//	require.NoError(t, err)
//
//	wpt := mi.WindowPoStProofType
//	spt, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, false)
//	require.NoError(t, err)
//
//	num, err := seal.AllocateSectorNumbers(ctx, full, db, maddr, 1, func(tx *harmonydb.Tx, numbers []abi.SectorNumber) (bool, error) {
//		for _, n := range numbers {
//			_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) values ($1, $2, $3)", mid, n, spt)
//			if err != nil {
//				return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
//			}
//		}
//		return true, nil
//	})
//	require.NoError(t, err)
//	require.Len(t, num, 1)
//	// TODO: add DDO deal, f05 deal 2 MiB each in the sector
//
//	var sectorParamsArr []struct {
//		SpID         int64 `db:"sp_id"`
//		SectorNumber int64 `db:"sector_number"`
//	}
//
//	require.Eventuallyf(t, func() bool {
//		h, err := full.ChainHead(ctx)
//		require.NoError(t, err)
//		t.Logf("head: %d", h.Height())
//
//		err = db.Select(ctx, &sectorParamsArr, `
//		SELECT sp_id, sector_number
//		FROM sectors_sdr_pipeline
//		WHERE after_commit_msg_success = True`)
//		require.NoError(t, err)
//		return len(sectorParamsArr) == 1
//	}, 10*time.Minute, 1*time.Second, "sector did not finish sealing in 5 minutes")
//
//	require.Equal(t, sectorParamsArr[0].SectorNumber, int64(0))
//	require.Equal(t, sectorParamsArr[0].SpID, int64(mid))
//
//	_ = capi.Shutdown(ctx)
//
//	<-finishCh
//}
