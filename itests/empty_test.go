package itests

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/tasks/seal"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"
)

func TestXYZ(t *testing.T) {
	ctx := context.Background()
	e := NewEnsemble(t)
	v, err := e.Full.Version(ctx)
	require.NoError(t, err)
	t.Log(v)

	require.NoError(t, err)

	cdr := path.Join(e.workDir, "curio")
	err = os.Mkdir(cdr, 0755)
	require.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(cdr)
	}()

	maddr := e.genesis.miners[0].ID

	token, err := e.Full.AuthNew(ctx, api.AllPermissions)
	require.NoError(t, err)

	fapi := fmt.Sprintf("%s:%s", string(token), e.Full.ListenAddr)

	err = deps.CreateMinerConfig(ctx, e.Full.FullNode, e.db, []string{maddr.String()}, fapi)
	require.NoError(t, err)

	var titles []string
	err = e.db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	require.NoError(t, err)
	require.Contains(t, titles, "base")
	baseCfg := config.DefaultCurioConfig()
	var baseText string

	err = e.db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	require.NoError(t, err)
	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	require.NotNil(t, baseCfg.Addresses)
	require.GreaterOrEqual(t, len(baseCfg.Addresses), 1)

	require.Contains(t, baseCfg.Addresses[0].MinerAddresses, maddr.String())

	capi, enginerTerm, closure, finishCh := ConstructCurioTest(ctx, t, e.workDir, cdr, e.db, e.Full.FullNode, maddr, baseCfg)
	defer enginerTerm()
	defer closure()

	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)

	mi, err := e.Full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	nv, err := e.Full.StateNetworkVersion(ctx, types.EmptyTSK)
	require.NoError(t, err)

	wpt := mi.WindowPoStProofType
	spt, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, false)
	require.NoError(t, err)

	//dl, err := e.Full.StateMinerProvingDeadline(ctx, maddr, types.EmptyTSK)
	//require.NoError(t, err)

	dl, err := e.Full.StateSectorPartition(ctx, maddr, 0, types.EmptyTSK)
	require.NoError(t, err)

	log.Infof("SECTOR 0 is at DEADLINE %d", dl.Deadline)

	start := time.Now()

	for time.Since(start) < (10 * time.Minute) {
		dll, err := e.Full.StateMinerProvingDeadline(ctx, maddr, types.EmptyTSK)
		require.NoError(t, err)
		log.Infof("CURRENT DEADLINE %d", dll.Index)
		time.Sleep(2 * time.Second)
	}

	num, err := seal.AllocateSectorNumbers(ctx, e.Full.FullNode, e.db, maddr, 1, func(tx *harmonydb.Tx, numbers []abi.SectorNumber) (bool, error) {
		for _, n := range numbers {
			_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) values ($1, $2, $3)", mid, n, spt)
			if err != nil {
				return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
			}
		}
		return true, nil
	})
	require.NoError(t, err)
	require.Len(t, num, 1)

	var sectorParamsArr []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
	}

	require.Eventuallyf(t, func() bool {
		h, err := e.Full.ChainHead(ctx)
		require.NoError(t, err)
		t.Logf("head: %d", h.Height())

		err = e.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number
		FROM sectors_sdr_pipeline
		WHERE after_commit_msg_success = True`)
		require.NoError(t, err)
		return len(sectorParamsArr) == 1
	}, 10*time.Minute, 1*time.Second, "sector did not finish sealing in 10 minutes")

	require.Equal(t, sectorParamsArr[0].SectorNumber, int64(2))
	require.Equal(t, sectorParamsArr[0].SpID, int64(mid))

	_ = capi.Shutdown(ctx)

	<-finishCh
}
