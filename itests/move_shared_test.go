package itests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonydb/testutil"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

const metaFile = "sectorstore.json"

func createTestStorage(t *testing.T, p string, seal bool, att ...*paths.Local) storiface.ID {
	if err := os.MkdirAll(p, 0755); err != nil {
		if !os.IsExist(err) {
			require.NoError(t, err)
		}
	}

	cfg := &storiface.LocalStorageMeta{
		ID:       storiface.ID(uuid.New().String()),
		Weight:   10,
		CanSeal:  seal,
		CanStore: !seal,
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(p, metaFile), b, 0644))

	for _, s := range att {
		require.NoError(t, s.OpenPath(context.Background(), p))
	}

	return cfg.ID
}

func TestMoveShared(t *testing.T) {
	t.Parallel()
	logging.SetAllLoggers(logging.LevelDebug)

	sharedITestID := testutil.SetupTestDB(t)

	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)

	index := paths.NewDBIndex(nil, db)

	ctx := context.Background()

	dir := t.TempDir()

	openRepo := func(dir string) paths.LocalStorage {
		bls := &paths.BasicLocalStorage{PathToJSON: filepath.Join(t.TempDir(), "storage.json")}
		return bls
	}

	// setup two repos with two storage paths:
	// repo 1 with both paths
	// repo 2 with one path (shared)

	lr1 := openRepo(filepath.Join(dir, "l1"))
	lr2 := openRepo(filepath.Join(dir, "l2"))

	mux1 := mux.NewRouter()
	mux2 := mux.NewRouter()
	hs1 := httptest.NewServer(mux1)
	hs2 := httptest.NewServer(mux2)

	ls1, err := paths.NewLocal(ctx, lr1, index, hs1.URL+"/remote")
	require.NoError(t, err)
	ls2, err := paths.NewLocal(ctx, lr2, index, hs2.URL+"/remote")
	require.NoError(t, err)

	dirStor := filepath.Join(dir, "stor")
	dirSeal := filepath.Join(dir, "seal")

	id1 := createTestStorage(t, dirStor, false, ls1, ls2)
	id2 := createTestStorage(t, dirSeal, true, ls1)

	rs1, err := paths.NewRemote(ls1, index, nil, 20, &paths.DefaultPartialFileHandler{})
	require.NoError(t, err)
	rs2, err := paths.NewRemote(ls2, index, nil, 20, &paths.DefaultPartialFileHandler{})
	require.NoError(t, err)
	_ = rs2
	mux1.PathPrefix("/").Handler(&paths.FetchHandler{Local: ls1, PfHandler: &paths.DefaultPartialFileHandler{}})
	mux2.PathPrefix("/").Handler(&paths.FetchHandler{Local: ls2, PfHandler: &paths.DefaultPartialFileHandler{}})

	// add a sealed replica file to the sealing (non-shared) path

	s1ref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  12,
			Number: 1,
		},
		ProofType: abi.RegisteredSealProof_StackedDrg2KiBV1,
	}

	sp, sid, err := rs1.AcquireSector(ctx, s1ref, storiface.FTNone, storiface.FTSealed, storiface.PathSealing, storiface.AcquireMove)
	require.NoError(t, err)
	require.Equal(t, id2, storiface.ID(sid.Sealed))

	data := make([]byte, 2032)
	data[1] = 54
	require.NoError(t, os.WriteFile(sp.Sealed, data, 0666))
	fmt.Println("write to ", sp.Sealed)

	require.NoError(t, index.StorageDeclareSector(ctx, storiface.ID(sid.Sealed), s1ref.ID, storiface.FTSealed, true))

	// move to the shared path from the second node (remote move / delete)

	require.NoError(t, rs2.MoveStorage(ctx, s1ref, storiface.FTSealed))

	// check that the file still exists
	sp, sid, err = rs2.AcquireSector(ctx, s1ref, storiface.FTSealed, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	require.NoError(t, err)
	require.Equal(t, id1, storiface.ID(sid.Sealed))
	fmt.Println("read from ", sp.Sealed)

	read, err := os.ReadFile(sp.Sealed)
	require.NoError(t, err)
	require.EqualValues(t, data, read)
}
