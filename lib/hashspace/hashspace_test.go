package hashspace

import (
	"bytes"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/fs2"
	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

func TestWriterCloseAbortAndDoubleClose(t *testing.T) {
	_, sp := loadOne(t, 1<<30)
	c := mustPiece(t, 0x11)
	payload := []byte("abcdefghij")

	w, err := sp.WriteCID(c)
	require.NoError(t, err)
	n, err := w.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
	require.Equal(t, int64(len(payload)), sp.Used())

	aborted := mustPiece(t, 0x12)
	aw, err := sp.WriteCID(aborted)
	require.NoError(t, err)
	_, err = aw.Write([]byte("nope"))
	require.NoError(t, err)
	require.NoError(t, aw.(interface{ Abort() error }).Abort())
	require.Equal(t, int64(len(payload)), sp.Used())
	final, err := piecePath(sp.disks[0].root, DIR_OPEN, mustNovel(t, aborted))
	require.NoError(t, err)
	_, err = os.Stat(final)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestFlushPersistsUsed(t *testing.T) {
	root, sp := loadOne(t, 1<<30)
	body := []byte("persisted")
	commit(t, sp, mustPiece(t, 0x33), body)
	require.NoError(t, sp.Flush())

	layout, err := readLayout(filepath.Join(root, DIR_OPEN, layoutFile))
	require.NoError(t, err)
	require.Equal(t, int64(len(body)), layout.Used)
	require.Equal(t, SPLIT, layout.Split)
	require.False(t, layout.CommittedAt.IsZero())
}

func TestWriteCIDExistingNoCounterChange(t *testing.T) {
	_, sp := loadOne(t, 1<<30)
	c := mustPiece(t, 0x21)
	body := []byte("piece")
	commit(t, sp, c, body)
	used := sp.Used()

	_, err := sp.WriteCID(c)
	require.ErrorIs(t, err, os.ErrExist)
	require.Equal(t, used, sp.Used())
}

func TestDeleteCIDSubtractsAndMissingDoesNotUnderflow(t *testing.T) {
	_, sp := loadOne(t, 1<<30)
	c := mustPiece(t, 0x22)
	body := []byte("12345")
	commit(t, sp, c, body)
	require.Equal(t, int64(len(body)), sp.Used())

	require.NoError(t, sp.DeleteCID(c))
	require.Zero(t, sp.Used())

	sp.disks[0].tracker.Set(50)
	missing := mustPiece(t, 0x23)
	err := sp.DeleteCID(missing)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, int64(50), sp.Used())
}

func TestTwoSpacesUsedAreIndependent(t *testing.T) {
	root := t.TempDir()
	_, err := FirstSetup([]Drive{{Root: root, Capacity: 1 << 30}})
	require.NoError(t, err)
	openSp := mustLoad(t, DIR_OPEN, root)
	aclSp := mustLoad(t, DIR_ACL, root)

	commit(t, openSp, mustPiece(t, 0x01), []byte("open"))
	commit(t, aclSp, mustPiece(t, 0x02), []byte("acl-data"))
	require.Equal(t, int64(4), openSp.Used())
	require.Equal(t, int64(8), aclSp.Used())

	require.NoError(t, aclSp.DeleteCID(mustPiece(t, 0x02)))
	require.Equal(t, int64(4), openSp.Used())
	require.Zero(t, aclSp.Used())
}

func TestFirstSetupSeedsEveryDrive(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(a, sectorStoreFile), []byte(`{"MaxStorage":4000}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(b, sectorStoreFile), []byte(`{"MaxStorage":12000}`), 0o644))

	st, err := FirstSetup([]Drive{{Root: a}, {Root: b}})
	require.NoError(t, err)
	require.Equal(t, []int64{4000, 12000}, st.Disks)
	require.NoError(t, hashspacesolver.Validate(st))
	require.Len(t, st.Spaces, 2)

	quarter := make([]byte, HASH_BYTES)
	quarter[0] = 0x40
	for _, sp := range st.Spaces {
		require.Len(t, sp.Ranges, 2)
		require.ElementsMatch(t, []int{0, 1}, append([]int(nil), sp.Owner...))
		var sawQuarter, sawZero bool
		for i, r := range sp.Ranges {
			start := hashspacesolver.StartHash(sp.Ranges, i)
			require.True(t, hashspacesolver.Contains(start, r.EndHash, r.EndHash))
			if !bytes.Equal(start, r.EndHash) {
				require.False(t, hashspacesolver.Contains(start, r.EndHash, start))
			}
			if isZeroHash(r.EndHash) {
				sawZero = true
			}
			if bytes.Equal(r.EndHash, quarter) {
				sawQuarter = true
			}
			owners := 0
			for j, other := range sp.Ranges {
				otherStart := hashspacesolver.StartHash(sp.Ranges, j)
				if hashspacesolver.Contains(otherStart, other.EndHash, r.EndHash) {
					owners++
				}
			}
			require.Equal(t, 1, owners)
		}
		require.True(t, sawQuarter)
		require.True(t, sawZero)
	}

	for _, root := range []string{a, b} {
		for _, kind := range []string{DIR_OPEN, DIR_ACL} {
			layout, err := readLayout(filepath.Join(root, kind, layoutFile))
			require.NoError(t, err)
			require.Equal(t, SPLIT, layout.Split)
			require.Zero(t, layout.Used)
			require.NotEmpty(t, layout.Ranges)
			require.False(t, layout.CommittedAt.IsZero())
		}
	}
}

func TestFirstSetupArriveDoesNotReseed(t *testing.T) {
	first := t.TempDir()
	_, err := FirstSetup([]Drive{{Root: first, Capacity: 100}})
	require.NoError(t, err)
	second := t.TempDir()
	st, err := FirstSetup([]Drive{
		{Root: first, Capacity: 100},
		{Root: second, Capacity: 100},
	})
	require.NoError(t, err)
	require.Equal(t, []int64{100, 100}, st.Disks)
	require.NoError(t, hashspacesolver.Validate(st))
	require.Len(t, st.Spaces[0].Ranges, 1)
	require.Len(t, st.Spaces[1].Ranges, 1)

	fresh, err := readLayout(filepath.Join(second, DIR_OPEN, layoutFile))
	require.NoError(t, err)
	require.Equal(t, SPLIT, fresh.Split)
	require.Empty(t, fresh.Ranges)
	require.Zero(t, fresh.Used)
}

func TestRestartKeepsUsedAndAddsOnlyNewerFiles(t *testing.T) {
	root := t.TempDir()
	space := filepath.Join(root, DIR_OPEN)
	require.NoError(t, os.MkdirAll(space, 0o755))
	full := strings.Repeat("0", HASH_BYTES*2)
	require.NoError(t, writeLayout(root, DIR_OPEN, Layout{
		Used:        100,
		CommittedAt: time.Now().UTC().Add(-time.Hour),
		Split:       SPLIT,
		Ranges:      []HashRange{{Start: full, End: full}},
	}))
	layoutInfo, err := os.Stat(filepath.Join(space, layoutFile))
	require.NoError(t, err)
	past := layoutInfo.ModTime().Add(-2 * time.Second)
	future := layoutInfo.ModTime().Add(2 * time.Second)

	oldShard := filepath.Join(space, "aa")
	require.NoError(t, os.MkdirAll(oldShard, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldShard, "old"), bytes.Repeat([]byte{'o'}, 10), 0o644))
	require.NoError(t, os.Chtimes(filepath.Join(oldShard, "old"), future, future))
	require.NoError(t, os.Chtimes(oldShard, past, past))

	newShard := filepath.Join(space, "bb")
	require.NoError(t, os.MkdirAll(newShard, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newShard, "stale"), bytes.Repeat([]byte{'s'}, 8), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(newShard, "fresh"), bytes.Repeat([]byte{'f'}, 7), 0o644))
	require.NoError(t, os.Chtimes(filepath.Join(newShard, "stale"), past, past))
	require.NoError(t, os.Chtimes(filepath.Join(newShard, "fresh"), future, future))
	require.NoError(t, os.Chtimes(newShard, future, future))

	sp := mustLoad(t, DIR_OPEN, root)
	require.Equal(t, int64(107), sp.Used())
}

func TestSplitTwoDashMatchesFS2AndRange(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ab"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ab", "cdefg"), bytes.Repeat([]byte{'x'}, 42), 0o644))

	res, err := fs2.SumFileSizesRange(dir, "abcdefe", "abcdefg", 0)
	require.NoError(t, err)
	require.Equal(t, uint64(42), res.Bytes)
	require.Equal(t, uint64(1), res.Files)
	require.True(t, hashspacesolver.Contains([]byte("abcdefe"), []byte("abcdefg"), []byte("abcdefg")))
	require.False(t, hashspacesolver.Contains([]byte("abcdefg"), []byte("abcdefh"), []byte("abcdefg")))

	c := mustPiece(t, 0x11)
	novel := mustNovel(t, c)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, novel[:2]), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, novel[:2], novel[2:]), bytes.Repeat([]byte{'p'}, 5), 0o644))
	low := novel[:len(novel)-1]
	res, err = fs2.SumFileSizesRange(dir, low, novel, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(5), res.Bytes)
	res, err = fs2.SumFileSizesRange(dir, novel, novel+"0", 0)
	require.NoError(t, err)
	require.Zero(t, res.Bytes)
}

func TestMultiRangeUsesFS2NotFolderUsed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ab"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "mn"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ab", "cdefg"), bytes.Repeat([]byte{'a'}, 42), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mn", "opqrs"), bytes.Repeat([]byte{'m'}, 9), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, layoutFile), bytes.Repeat([]byte{'L'}, 10), 0o644))

	ranges := []HashRange{{Start: "a", End: "b"}, {Start: "l", End: "n"}}
	sizes, err := sizesForRanges(dir, ranges, 999)
	require.NoError(t, err)
	require.Equal(t, []int64{42, 9}, sizes)

	one, err := sizesForRanges(dir, ranges[:1], 999)
	require.NoError(t, err)
	require.Equal(t, []int64{999}, one)
}

func TestWriteCIDPlacesOnOwningDiskWithoutChmod(t *testing.T) {
	low := t.TempDir()
	high := t.TempDir()
	lowEnd := hexHash(0x0f)
	zero := hexHash(0x00)
	for _, root := range []string{low, high} {
		var ranges []HashRange
		if root == low {
			ranges = []HashRange{{Start: zero, End: lowEnd}}
		} else {
			ranges = []HashRange{{Start: lowEnd, End: zero}}
		}
		require.NoError(t, writeLayout(root, DIR_OPEN, Layout{
			Used:        0,
			CommittedAt: time.Now().UTC(),
			Split:       SPLIT,
			Ranges:      ranges,
		}))
	}
	sp := mustLoad(t, DIR_OPEN, low, high)

	lowCID := mustPiece(t, 0x01)
	highCID := mustPiece(t, 0x10)
	commit(t, sp, lowCID, []byte("L"))
	commit(t, sp, highCID, []byte("HH"))

	lowPath, err := piecePath(low, DIR_OPEN, mustNovel(t, lowCID))
	require.NoError(t, err)
	highPath, err := piecePath(high, DIR_OPEN, mustNovel(t, highCID))
	require.NoError(t, err)
	_, err = os.Stat(lowPath)
	require.NoError(t, err)
	_, err = os.Stat(highPath)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(high, DIR_OPEN, mustNovel(t, lowCID)[:2], mustNovel(t, lowCID)[2:]))
	require.ErrorIs(t, err, os.ErrNotExist)

	info, err := os.Stat(lowPath)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o111)

	f, err := sp.ReadCIDFileFrom(highCID)
	require.NoError(t, err)
	_, err = f.Seek(1, io.SeekStart)
	require.NoError(t, err)
	buf := make([]byte, 1)
	_, err = f.Read(buf)
	require.NoError(t, err)
	require.Equal(t, []byte("H"), buf)
	require.NoError(t, f.Close())

	require.NoError(t, os.Remove(highPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(low, DIR_OPEN, mustNovel(t, highCID)[:2])), 0o755))
	probed := filepath.Join(low, DIR_OPEN, mustNovel(t, highCID)[:2], mustNovel(t, highCID)[2:])
	require.NoError(t, os.MkdirAll(filepath.Dir(probed), 0o755))
	require.NoError(t, os.WriteFile(probed, []byte("HH"), 0o644))
	f, err = sp.ReadCIDFileFrom(highCID)
	require.NoError(t, err)
	got, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, []byte("HH"), got)
	require.NoError(t, f.Close())
}

func TestNoPublicMkdirChmodOrDiskUsage(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	forbidden := map[string]struct{}{
		"Mkdir": {}, "MkdirAll": {}, "Chmod": {}, "EnsureRoot": {},
		"SetACL": {}, "Reserve": {}, "Solve": {}, "DiskUsage": {},
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		require.NotContains(t, string(src), "DiskUsage")
		require.NotContains(t, string(src), "curio/lib/paths")
		require.NotContains(t, string(src), "os.Chmod")
		file, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			_, bad := forbidden[fn.Name.Name]
			require.False(t, bad, name)
		}
	}
}

func TestPieceV2UsesSameDigestPath(t *testing.T) {
	v1 := mustPiece(t, 0x15)
	v2, err := commcid.PieceCidV2FromV1(v1, 127)
	require.NoError(t, err)
	require.Equal(t, mustNovel(t, v1), mustNovel(t, v2))
}

func loadOne(t *testing.T, cap int64) (string, *Space) {
	t.Helper()
	root := t.TempDir()
	_, err := FirstSetup([]Drive{{Root: root, Capacity: cap}})
	require.NoError(t, err)
	return root, mustLoad(t, DIR_OPEN, root)
}

func mustLoad(t *testing.T, kind string, roots ...string) *Space {
	t.Helper()
	sp, err := Load(kind, roots)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sp.Close() })
	return sp
}

func commit(t *testing.T, sp *Space, c cid.Cid, body []byte) {
	t.Helper()
	w, err := sp.WriteCID(c)
	require.NoError(t, err)
	_, err = w.Write(body)
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func mustPiece(t *testing.T, first byte) cid.Cid {
	t.Helper()
	digest := make([]byte, HASH_BYTES)
	digest[0] = first & 0x3f
	c, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)
	got, err := commitmentOf(c)
	require.NoError(t, err)
	require.Equal(t, digest, got)
	return c
}

func mustNovel(t *testing.T, c cid.Cid) string {
	t.Helper()
	novel, _, err := novelOf(c)
	require.NoError(t, err)
	return novel
}

func hexHash(first byte) string {
	b := make([]byte, HASH_BYTES)
	b[0] = first
	return hex.EncodeToString(b)
}
