package hashfolder

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/hashspacesolver"
	"github.com/filecoin-project/curio/lib/storiface"
)

// errNoPieceDrive is returned when every drive denies piece park, so there
// is nothing to seed a hash space onto.
var errNoPieceDrive = xerrors.New("no drive accepts piece park")

// FirstSetup writes the layout for both spaces on every drive.
//
// With no layouts, it seeds one capacity-weighted range per disk and checks
// that assignment with hashspacesolver.Validate. A drive added beside
// existing layouts is presented with EventEmptyMountConnected. Existing layouts are
// reloaded rather than seeded again.
func FirstSetup(drives []Drive) (hashspacesolver.State, error) {
	if len(drives) == 0 {
		return hashspacesolver.State{}, xerrors.Errorf("no drives")
	}
	caps := make([]int64, len(drives))
	hasLayout := make([]bool, len(drives))
	// A whole-circle claim from migratePiecePark counts as no layout, keeping
	// the bytes it counted.
	claimUsed := make([][2]int64, len(drives))
	var nLayout int
	for i, d := range drives {
		if d.Root == "" {
			return hashspacesolver.State{}, xerrors.Errorf("drive %d has an empty root", i)
		}
		if d.StorageID == "" {
			return hashspacesolver.State{}, xerrors.Errorf("drive %s has no storage ID", d.Root)
		}
		cap, err := capacityOf(d)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		caps[i] = cap
		claim, err := isClaim(d.Root)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		if claim {
			if claimUsed[i], err = claimedBytes(d.Root); err != nil {
				return hashspacesolver.State{}, err
			}
			continue
		}
		if hasLayout[i], err = hasBothLayouts(d.Root); err != nil {
			return hashspacesolver.State{}, err
		}
		if hasLayout[i] {
			nLayout++
		}
	}
	switch nLayout {
	case 0:
		return seedState(drives, caps, claimUsed)
	case len(drives):
		perSpace, _, err := readOwned(drives, nil)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		return stateFromOwned(drives, perSpace)
	default:
		return arriveNew(drives, hasLayout, claimUsed)
	}
}

// claimedBytes is the used count of each space's claim layout on root.
func claimedBytes(root string) ([2]int64, error) {
	var used [2]int64
	for s, kind := range spaceKinds {
		ok, err := layoutExists(root, kind)
		if err != nil {
			return used, err
		}
		if !ok {
			continue
		}
		if _, used[s], err = accountedLayout(root, kind); err != nil {
			return used, err
		}
	}
	return used, nil
}

// hasBothLayouts reports whether root has a layout for both spaces. A layout
// for only one space is an error.
func hasBothLayouts(root string) (bool, error) {
	openOK, err := layoutExists(root, DIR_OPEN)
	if err != nil {
		return false, err
	}
	aclOK, err := layoutExists(root, DIR_ACL)
	if err != nil {
		return false, err
	}
	if openOK != aclOK {
		return false, xerrors.Errorf("%s has a layout for only one hash space", root)
	}
	return openOK, nil
}

// seedState cuts one capacity-weighted range per space for each drive that
// accepts piece park, validates the result, and writes every drive's
// layouts with used as their counters. caps and used are indexed like drives.
func seedState(drives []Drive, caps []int64, used [][2]int64) (hashspacesolver.State, error) {
	seedCaps := append([]int64(nil), caps...)
	anyPositive := false
	for i, d := range drives {
		deny, err := deniesPiecePark(d.Root)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		if deny {
			seedCaps[i] = 0
		}
		if seedCaps[i] > 0 {
			anyPositive = true
		}
	}
	if !anyPositive {
		return hashspacesolver.State{}, errNoPieceDrive
	}
	open, err := seedSpace(drives, seedCaps)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	acl, err := seedSpace(drives, seedCaps)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	mounts, err := mountPoints(drives)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	st, err := hashspacesolver.Apply(hashspacesolver.State{
		MountPoints: mounts,
		HashSpaces:  [2][]hashspacesolver.Range{open, acl},
	}, nil)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	if err := hashspacesolver.Validate(st); err != nil {
		return hashspacesolver.State{}, err
	}
	if err := writeState(drives, st, used); err != nil {
		return hashspacesolver.State{}, err
	}
	return st, nil
}

// arriveNew loads the ranges of drives with layouts, presents every other
// drive that accepts piece park with EventEmptyMountConnected, and writes
// the result. A drive without a layout keeps its claimUsed counters.
func arriveNew(drives []Drive, hasLayout []bool, claimUsed [][2]int64) (hashspacesolver.State, error) {
	perSpace, used, err := readOwned(drives, hasLayout)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	st, err := stateFromOwned(drives, perSpace)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	for i, ok := range hasLayout {
		if ok {
			continue
		}
		used[i] = claimUsed[i]
		deny, err := deniesPiecePark(drives[i].Root)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		if deny {
			continue
		}
		res, err := hashspacesolver.Solve(st, hashspacesolver.Event{
			Kind:      hashspacesolver.EventEmptyMountConnected,
			StorageID: drives[i].StorageID,
		})
		if err != nil {
			return hashspacesolver.State{}, xerrors.Errorf("empty mount %s: %w", drives[i].StorageID, err)
		}
		st = res.State
	}
	if err := writeState(drives, st, used); err != nil {
		return hashspacesolver.State{}, err
	}
	return st, nil
}

func capacityOf(d Drive) (int64, error) {
	if d.Capacity < 0 {
		return 0, xerrors.Errorf("negative capacity for %s", d.Root)
	}
	if d.Capacity > 0 {
		return d.Capacity, nil
	}
	fsCap, err := filesystemCapacity(d.Root)
	if err != nil {
		return 0, err
	}
	maxStorage, err := readMaxStorage(d.Root)
	if err != nil {
		return 0, err
	}
	if maxStorage > 0 && maxStorage < uint64(fsCap) {
		return int64(maxStorage), nil
	}
	return fsCap, nil
}

// readMaxStorage returns MaxStorage from root's sectorstore.json. A missing
// file or field is zero.
func readMaxStorage(root string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(root, sectorStoreFile))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, xerrors.Errorf("reading sectorstore.json in %s: %w", root, err)
	}
	var meta struct {
		MaxStorage uint64
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return 0, xerrors.Errorf("decoding sectorstore.json in %s: %w", root, err)
	}
	return meta.MaxStorage, nil
}

// effectiveCapacity is the capacity the solver may use on d. It is the
// filesystem's free space plus the hash-space bytes already stored there,
// and never more than the configured capacity. Other files on the mount are
// immovable, so they only reduce the free space.
func effectiveCapacity(d Drive, hashUsed int64) (int64, error) {
	limit, err := capacityOf(d)
	if err != nil {
		return 0, err
	}
	free, err := filesystemFree(d.Root)
	if err != nil {
		return 0, err
	}
	if hashUsed < 0 {
		hashUsed = 0
	}
	if free < 0 {
		free = 0
	}
	if hashUsed > math.MaxInt64-free || hashUsed+free >= limit {
		return limit, nil
	}
	return hashUsed + free, nil
}

// solverFree is the FreeSpace handed to the solver. The solver adds the
// hash-space bytes already on the mount, which brings its capacity back to
// effectiveCapacity.
func solverFree(d Drive, hashUsed int64) (int64, error) {
	budget, err := effectiveCapacity(d, hashUsed)
	if err != nil {
		return 0, err
	}
	if hashUsed >= budget {
		return 0, nil
	}
	return budget - hashUsed, nil
}

// deniesPiecePark reports whether sectorstore.json refuses piece-park files.
// A missing file accepts them.
func deniesPiecePark(root string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(root, sectorStoreFile))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, xerrors.Errorf("reading sectorstore.json in %s: %w", root, err)
	}
	var meta storiface.LocalStorageMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return false, xerrors.Errorf("decoding sectorstore.json in %s: %w", root, err)
	}
	return !storiface.FTPiece.Allowed(meta.AllowTypes, meta.DenyTypes), nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func mountPoints(drives []Drive) ([]hashspacesolver.MountPoint, error) {
	out := make([]hashspacesolver.MountPoint, len(drives))
	for i, d := range drives {
		free, err := solverFree(d, 0)
		if err != nil {
			return nil, err
		}
		out[i] = hashspacesolver.MountPoint{FreeSpace: free, StorageID: d.StorageID}
	}
	return out, nil
}

// seedSpace cuts one range per drive with positive capacity, sized by that
// capacity. capacities is indexed like drives.
func seedSpace(drives []Drive, capacities []int64) ([]hashspacesolver.Range, error) {
	var total int64
	for i, c := range capacities {
		if c < 0 {
			return nil, xerrors.Errorf("disk %d has negative capacity", i)
		}
		if total > math.MaxInt64-c {
			return nil, xerrors.Errorf("disk capacities overflow")
		}
		total += c
	}
	if total <= 0 {
		return nil, xerrors.Errorf("drives have no capacity")
	}
	span := new(big.Int).Lsh(big.NewInt(1), HASH_BYTES*8)
	var prefix int64
	var ranges []hashspacesolver.Range
	seen := map[string]struct{}{}
	for i, c := range capacities {
		if c == 0 {
			continue
		}
		prefix += c
		var end []byte
		if prefix >= total {
			end = make([]byte, HASH_BYTES)
		} else {
			num := new(big.Int).Mul(big.NewInt(prefix), span)
			num.Quo(num, big.NewInt(total))
			end = intToHash(num)
			if isZeroHash(end) {
				return nil, xerrors.Errorf("disk %d capacity does not advance the hash cut", i)
			}
		}
		if _, ok := seen[string(end)]; ok {
			return nil, xerrors.Errorf("disk %d repeats a hash cut", i)
		}
		seen[string(end)] = struct{}{}
		ranges = append(ranges, hashspacesolver.Range{EndHash: end, Size: 0, StorageID: drives[i].StorageID})
		if prefix >= total {
			break
		}
	}
	hashspacesolver.LinkStarts(ranges)
	return ranges, nil
}

// intToHash writes v as a big-endian HASH_BYTES hash, keeping the low bytes
// when v is wider.
func intToHash(v *big.Int) []byte {
	raw := v.Bytes()
	out := make([]byte, HASH_BYTES)
	if len(raw) > HASH_BYTES {
		copy(out, raw[len(raw)-HASH_BYTES:])
		return out
	}
	copy(out[HASH_BYTES-len(raw):], raw)
	return out
}

func isZeroHash(h []byte) bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}

type ownedRange struct {
	start []byte
	end   []byte
	disk  int
	size  int64
}

// readOwned loads ranges for drives that already have layouts. hasLayout nil
// means every drive is present. used includes files newer than the layout.
func readOwned(drives []Drive, hasLayout []bool) ([][]ownedRange, [][2]int64, error) {
	perSpace := make([][]ownedRange, 2)
	used := make([][2]int64, len(drives))
	for i, d := range drives {
		if hasLayout != nil && !hasLayout[i] {
			continue
		}
		for s, kind := range []string{DIR_OPEN, DIR_ACL} {
			layout, folderUsed, err := accountedLayout(d.Root, kind)
			if err != nil {
				return nil, nil, err
			}
			used[i][s] = folderUsed
			sizes, err := sizesForRanges(filepath.Join(d.Root, kind), layout.Ranges, folderUsed)
			if err != nil {
				return nil, nil, err
			}
			for j, r := range layout.Ranges {
				start, err := decodeHash(r.Start)
				if err != nil {
					return nil, nil, xerrors.Errorf("%s %s range %d: %w", d.Root, kind, j, err)
				}
				end, err := decodeHash(r.End)
				if err != nil {
					return nil, nil, xerrors.Errorf("%s %s range %d: %w", d.Root, kind, j, err)
				}
				perSpace[s] = append(perSpace[s], ownedRange{
					start: start,
					end:   end,
					disk:  i,
					size:  sizes[j],
				})
			}
		}
	}
	return perSpace, used, nil
}

func accountedLayout(root, kind string) (Layout, int64, error) {
	layout, modTime, err := openLayout(root, kind)
	if err != nil {
		return Layout{}, 0, err
	}
	d := &disk{root: root, tracker: &sizeTracker{}}
	d.tracker.Set(layout.Used)
	if err := d.catchUp(kind, modTime); err != nil {
		return Layout{}, 0, err
	}
	return layout, d.tracker.Used(), nil
}

func stateFromOwned(drives []Drive, perSpace [][]ownedRange) (hashspacesolver.State, error) {
	mounts, err := mountPoints(drives)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	st := hashspacesolver.State{MountPoints: mounts}
	for s, rs := range perSpace {
		sort.Slice(rs, func(a, b int) bool { return bytes.Compare(rs[a].end, rs[b].end) < 0 })
		ranges := make([]hashspacesolver.Range, len(rs))
		for i, r := range rs {
			ranges[i] = hashspacesolver.Range{
				StartHash: append([]byte(nil), r.start...),
				EndHash:   append([]byte(nil), r.end...),
				Size:      r.size,
				StorageID: drives[r.disk].StorageID,
			}
		}
		st.HashSpaces[s] = ranges
	}
	// FreeSpace is the filesystem's free space, capped so that free space plus
	// the hash-space bytes on this mount is the solver capacity. Other files
	// on the mount stay outside that capacity.
	for i, d := range drives {
		var used int64
		for _, rs := range st.HashSpaces {
			for _, r := range rs {
				if r.StorageID == d.StorageID {
					used += r.Size
				}
			}
		}
		free, err := solverFree(d, used)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		st.MountPoints[i].FreeSpace = free
	}
	if err := hashspacesolver.Validate(st); err != nil {
		return hashspacesolver.State{}, err
	}
	return st, nil
}

func writeState(drives []Drive, st hashspacesolver.State, used [][2]int64) error {
	now := time.Now().UTC().Truncate(time.Second)
	kinds := []string{DIR_OPEN, DIR_ACL}
	for i, d := range drives {
		for s, kind := range kinds {
			ranges := make([]HashRange, 0)
			for _, r := range st.HashSpaces[s] {
				if r.StorageID != d.StorageID {
					continue
				}
				ranges = append(ranges, HashRange{
					Start: hexEncode(r.StartHash),
					End:   hexEncode(r.EndHash),
				})
			}
			var folderUsed int64
			if i < len(used) {
				folderUsed = used[i][s]
			}
			if err := writeLayout(d.Root, kind, Layout{
				Used:        folderUsed,
				CommittedAt: now,
				Split:       SPLIT,
				Ranges:      ranges,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func hexEncode(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
