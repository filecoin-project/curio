package paths

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

const pathSize = 16 << 20

type TestingLocalStorage struct {
	root string
	c    storiface.StorageConfig
}

func (t *TestingLocalStorage) DiskUsage(path string) (int64, error) {
	return 1, nil
}

func (t *TestingLocalStorage) GetStorage() (storiface.StorageConfig, error) {
	return t.c, nil
}

func (t *TestingLocalStorage) SetStorage(f func(*storiface.StorageConfig)) error {
	f(&t.c)
	return nil
}

func (t *TestingLocalStorage) Stat(path string) (fsutil.FsStat, error) {
	return fsutil.FsStat{
		Capacity:    pathSize,
		Available:   pathSize,
		FSAvailable: pathSize,
	}, nil
}

func (t *TestingLocalStorage) init(subpath string) error {
	path := filepath.Join(t.root, subpath)
	if err := os.Mkdir(path, 0755); err != nil {
		return err
	}

	metaFile := filepath.Join(path, MetaFile)

	meta := &storiface.LocalStorageMeta{
		ID:       storiface.ID(uuid.New().String()),
		Weight:   1,
		CanSeal:  true,
		CanStore: true,
	}

	mb, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(metaFile, mb, 0644); err != nil {
		return err
	}

	return nil
}

var _ LocalStorage = &TestingLocalStorage{}

func TestLocalStorage(t *testing.T) {
	ctx := context.TODO()

	root := t.TempDir()

	tstor := &TestingLocalStorage{
		root: root,
	}

	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	index := NewDBIndex(nil, db)

	st, err := NewLocal(ctx, tstor, index, "")
	require.NoError(t, err)

	p1 := "1"
	require.NoError(t, tstor.init("1"))

	err = st.OpenPath(ctx, filepath.Join(tstor.root, p1))
	require.NoError(t, err)

	// TODO: put more things here
}

func TestExistingLocalFile(t *testing.T) {
	root := t.TempDir()
	sid := abi.SectorID{Miner: 0, Number: 42}

	pieceDir := filepath.Join(root, storiface.FTPiece.String())
	require.NoError(t, os.MkdirAll(pieceDir, 0755))
	fpath := filepath.Join(pieceDir, storiface.SectorName(sid))
	require.NoError(t, os.WriteFile(fpath, []byte("piece-bytes"), 0644))

	emptyRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(emptyRoot, storiface.FTPiece.String()), 0755))

	st := &Local{
		paths: map[storiface.ID]*path{
			"local-1": {Local: root},
			"local-2": {Local: emptyRoot},
		},
	}

	got, ok := st.ExistingLocalFile(sid, storiface.FTPiece)
	require.True(t, ok)
	require.Equal(t, fpath, got)

	_, ok = st.ExistingLocalFile(abi.SectorID{Miner: 0, Number: 99}, storiface.FTPiece)
	require.False(t, ok)

	// Path that denies FTPiece should be skipped even if the file exists.
	st.paths["local-1"].DenyTypes = []string{storiface.FTPiece.String()}
	_, ok = st.ExistingLocalFile(sid, storiface.FTPiece)
	require.False(t, ok)
}

func TestExistingLocalFileReleasesLockWhenManyPathsMatch(t *testing.T) {
	st := &Local{paths: map[storiface.ID]*path{}}
	for i := 0; i < 6; i++ {
		st.paths[storiface.ID("path-"+string(rune('a'+i)))] = &path{Local: t.TempDir()}
	}

	_, ok := st.ExistingLocalFile(abi.SectorID{Miner: 0, Number: 1}, storiface.FTPiece)
	require.False(t, ok)

	if !st.localLk.TryLock() {
		t.Fatal("read lock still held after ExistingLocalFile")
	}
	n := len(st.paths)
	st.localLk.Unlock()
	require.Equal(t, 6, n)
}

func TestDeclareableSectorEntrySkipsLeftovers(t *testing.T) {
	dir := t.TempDir()

	mustStat := func(path string) os.FileInfo {
		t.Helper()
		info, err := os.Stat(path)
		require.NoError(t, err)
		return info
	}

	sealed := filepath.Join(dir, "s-t01000-5")
	require.NoError(t, os.WriteFile(sealed, []byte("replica"), 0644))
	sid, ok := declareableSectorEntry("s-t01000-5", storiface.FTSealed, mustStat(sealed))
	require.True(t, ok)
	require.Equal(t, abi.SectorID{Miner: 1000, Number: 5}, sid)

	tmp := sealed + storiface.TempSuffix
	require.NoError(t, os.WriteFile(tmp, []byte("partial"), 0644))
	_, ok = declareableSectorEntry(filepath.Base(tmp), storiface.FTSealed, mustStat(tmp))
	require.False(t, ok)

	empty := filepath.Join(dir, "s-t01000-6")
	require.NoError(t, os.WriteFile(empty, nil, 0644))
	_, ok = declareableSectorEntry("s-t01000-6", storiface.FTSealed, mustStat(empty))
	require.False(t, ok)

	asDir := filepath.Join(dir, "s-t01000-7")
	require.NoError(t, os.Mkdir(asDir, 0755))
	_, ok = declareableSectorEntry("s-t01000-7", storiface.FTSealed, mustStat(asDir))
	require.False(t, ok)

	cache := filepath.Join(dir, "s-t01000-8")
	require.NoError(t, os.Mkdir(cache, 0755))
	sid, ok = declareableSectorEntry("s-t01000-8", storiface.FTCache, mustStat(cache))
	require.True(t, ok)
	require.Equal(t, abi.SectorID{Miner: 1000, Number: 8}, sid)

	cacheFile := filepath.Join(dir, "s-t01000-9")
	require.NoError(t, os.WriteFile(cacheFile, []byte("not-a-cache"), 0644))
	_, ok = declareableSectorEntry("s-t01000-9", storiface.FTCache, mustStat(cacheFile))
	require.False(t, ok)

	_, ok = declareableSectorEntry(FetchTempSubdir, storiface.FTSealed, mustStat(dir))
	require.False(t, ok)
}

type healthReportIndex struct {
	SectorIndex
	lk       sync.Mutex
	recorded []recordedHealthReport
	onReport func()
}

type recordedHealthReport struct {
	storiface.HealthReport
	reportedAt time.Time
	ctxErr     error
}

func (i *healthReportIndex) reports() []recordedHealthReport {
	i.lk.Lock()
	defer i.lk.Unlock()
	return slices.Clone(i.recorded)
}

func (i *healthReportIndex) StorageDropSector(context.Context, storiface.ID, abi.SectorID, storiface.SectorFileType) error {
	return nil
}

func (i *healthReportIndex) StorageReportHealth(ctx context.Context, _ storiface.ID, report storiface.HealthReport) error {
	i.lk.Lock()
	i.recorded = append(i.recorded, recordedHealthReport{
		HealthReport: report,
		reportedAt:   time.Now(),
		ctxErr:       ctx.Err(),
	})
	onReport := i.onReport
	i.lk.Unlock()
	if onReport != nil {
		onReport()
	}
	return ctx.Err()
}

func newHealthReportLocal(t *testing.T, ctx context.Context) (*Local, *healthReportIndex) {
	t.Helper()
	index := &healthReportIndex{}
	st, err := NewLocal(ctx, &TestingLocalStorage{}, index, "")
	require.NoError(t, err)
	st.localLk.Lock()
	st.paths["test"] = &path{Local: t.TempDir()}
	st.localLk.Unlock()
	return st, index
}

func TestLocalHealthReportsCoalesce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger func(context.Context, *Local) error
	}{
		{
			name: "delete",
			trigger: func(ctx context.Context, st *Local) error {
				return st.removeSector(ctx, abi.SectorID{}, storiface.FTSealed, "test")
			},
		},
		{
			name: "move",
			trigger: func(ctx context.Context, st *Local) error {
				return st.MoveStorage(ctx, storiface.SectorRef{}, storiface.FTNone)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				st, index := newHealthReportLocal(t, ctx)
				requestCtx, cancelRequest := context.WithCancel(ctx)

				for range 10 {
					require.NoError(t, tc.trigger(requestCtx, st))
				}
				cancelRequest()
				synctest.Wait()
				require.Empty(t, index.reports(), "file operations must only trigger a delayed report")

				time.Sleep(STORAGE_REPORT_INTERVAL - HeartbeatInterval)
				require.NoError(t, st.removeSector(ctx, abi.SectorID{}, storiface.FTSealed, "test"))
				require.NoError(t, st.MoveStorage(ctx, storiface.SectorRef{}, storiface.FTNone))
				synctest.Wait()
				require.Empty(t, index.reports(), "deletes, moves and heartbeats must share the pending report")

				time.Sleep(HeartbeatInterval - time.Nanosecond)
				synctest.Wait()
				require.Empty(t, index.reports())
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				require.Len(t, index.reports(), 1, "repeated triggers must not postpone the first report")
				require.NoError(t, index.reports()[0].ctxErr, "report must use the worker context, not the finished request")
				require.EqualValues(t, pathSize, index.reports()[0].Stat.Available)

				require.NoError(t, tc.trigger(ctx, st))
				synctest.Wait()
				time.Sleep(STORAGE_REPORT_INTERVAL - time.Nanosecond)
				synctest.Wait()
				require.Len(t, index.reports(), 1)
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				require.Len(t, index.reports(), 2)
			})
		})
	}
}

func TestLocalHealthReportsPeriodic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, index := newHealthReportLocal(t, ctx)
		startedAt := time.Now()
		synctest.Wait()

		time.Sleep(HeartbeatInterval)
		synctest.Wait()
		require.Empty(t, index.reports(), "the periodic tick must only trigger a delayed report")
		time.Sleep(STORAGE_REPORT_INTERVAL - time.Nanosecond)
		synctest.Wait()
		require.Empty(t, index.reports())
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Len(t, index.reports(), 1)
		require.Less(t, index.reports()[0].reportedAt.Sub(startedAt), SkippedHeartbeatThresh, "storage must stay healthy until the first periodic report")

		time.Sleep(STORAGE_REPORT_INTERVAL + HeartbeatInterval)
		synctest.Wait()
		reports := index.reports()
		require.Len(t, reports, 2, "idle storage must keep reporting")
		gap := reports[1].reportedAt.Sub(reports[0].reportedAt)
		require.GreaterOrEqual(t, gap, STORAGE_REPORT_INTERVAL)
		require.Less(t, gap, SkippedHeartbeatThresh, "storage must stay healthy between periodic reports")
	})
}

func TestLocalHealthReportsCancelPending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		st, index := newHealthReportLocal(t, ctx)
		require.NoError(t, st.removeSector(ctx, abi.SectorID{}, storiface.FTSealed, "test"))
		synctest.Wait()
		time.Sleep(STORAGE_REPORT_INTERVAL / 2)
		synctest.Wait()
		require.Empty(t, index.reports())

		cancel()
		synctest.Wait()
		time.Sleep(STORAGE_REPORT_INTERVAL)
		synctest.Wait()
		require.Empty(t, index.reports())
	})
}

func TestLocalHealthReportsConcurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		st, index := newHealthReportLocal(t, ctx)
		release := make(chan struct{})
		index.lk.Lock()
		index.onReport = func() {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		index.lk.Unlock()

		for range 100 {
			go st.triggerReportStorage()
		}
		synctest.Wait()
		require.Empty(t, index.reports())
		time.Sleep(STORAGE_REPORT_INTERVAL)
		synctest.Wait()
		require.Len(t, index.reports(), 1)

		for range 100 {
			go st.triggerReportStorage()
		}
		synctest.Wait()
		time.Sleep(STORAGE_REPORT_INTERVAL)
		synctest.Wait()
		require.Len(t, index.reports(), 1, "a slow report must not overlap another report")

		close(release)
		synctest.Wait()
		time.Sleep(STORAGE_REPORT_INTERVAL - time.Nanosecond)
		synctest.Wait()
		require.Len(t, index.reports(), 1)
		time.Sleep(HeartbeatInterval + time.Nanosecond)
		synctest.Wait()
		require.Len(t, index.reports(), 2)
	})
}
