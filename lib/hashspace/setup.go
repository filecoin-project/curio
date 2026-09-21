package hashspace

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

// FirstSetup writes layout.json for both spaces on every drive.
//
// With no layouts, it seeds one capacity-weighted range per disk and checks
// that assignment with hashspacesolver.Validate. A drive added beside
// existing layouts is presented with EventArrive. Existing layouts are
// reloaded rather than seeded again.
func FirstSetup(drives []Drive) (hashspacesolver.State, error) {
	if len(drives) == 0 {
		return hashspacesolver.State{}, xerrors.Errorf("no drives")
	}
	caps := make([]int64, len(drives))
	both := make([]bool, len(drives))
	var nBoth, nNeither int
	for i, d := range drives {
		if d.Root == "" {
			return hashspacesolver.State{}, xerrors.Errorf("drive %d has an empty root", i)
		}
		cap, err := capacityOf(d)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		caps[i] = cap
		hasBoth, hasNeither, err := layoutPresence(d.Root)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		both[i] = hasBoth
		if hasBoth {
			nBoth++
		}
		if hasNeither {
			nNeither++
		}
	}
	switch {
	case nNeither == len(drives):
		st, err := seedState(caps)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		if err := writeState(drives, st, make([][2]int64, len(drives))); err != nil {
			return hashspacesolver.State{}, err
		}
		return st, nil
	case nBoth == len(drives):
		return loadState(drives, caps)
	default:
		return arriveNew(drives, caps, both)
	}
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

func layoutPresence(root string) (hasBoth, hasNeither bool, err error) {
	openOK, err := fileExists(filepath.Join(root, DIR_OPEN, layoutFile))
	if err != nil {
		return false, false, err
	}
	aclOK, err := fileExists(filepath.Join(root, DIR_ACL, layoutFile))
	if err != nil {
		return false, false, err
	}
	if openOK && aclOK {
		return true, false, nil
	}
	if !openOK && !aclOK {
		return false, true, nil
	}
	return false, false, xerrors.Errorf("%s has a layout for only one hash space", root)
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

func seedState(caps []int64) (hashspacesolver.State, error) {
	open, err := seedSpace(caps)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	acl, err := seedSpace(caps)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	st := hashspacesolver.State{
		Disks:  append([]int64(nil), caps...),
		Spaces: []hashspacesolver.Space{open, acl},
	}
	st, err = hashspacesolver.Apply(st, nil)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	if err := hashspacesolver.Validate(st); err != nil {
		return hashspacesolver.State{}, err
	}
	return st, nil
}

func seedSpace(capacities []int64) (hashspacesolver.Space, error) {
	var total int64
	for i, c := range capacities {
		if c < 0 {
			return hashspacesolver.Space{}, xerrors.Errorf("disk %d has negative capacity", i)
		}
		if total > math.MaxInt64-c {
			return hashspacesolver.Space{}, xerrors.Errorf("disk capacities overflow")
		}
		total += c
	}
	if total <= 0 {
		return hashspacesolver.Space{}, xerrors.Errorf("drives have no capacity")
	}
	span := new(big.Int).Lsh(big.NewInt(1), HASH_BYTES*8)
	var prefix int64
	var ranges []hashspacesolver.Range
	var owners []int
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
				return hashspacesolver.Space{}, xerrors.Errorf("disk %d capacity does not advance the hash cut", i)
			}
		}
		if _, ok := seen[string(end)]; ok {
			return hashspacesolver.Space{}, xerrors.Errorf("disk %d repeats a hash cut", i)
		}
		seen[string(end)] = struct{}{}
		ranges = append(ranges, hashspacesolver.Range{EndHash: end, Size: 0})
		owners = append(owners, i)
		if prefix >= total {
			break
		}
	}
	return hashspacesolver.Space{Ranges: ranges, Owner: owners}, nil
}

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

func loadState(drives []Drive, caps []int64) (hashspacesolver.State, error) {
	perSpace, _, err := readOwned(drives, nil)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	return stateFromOwned(caps, perSpace)
}

func arriveNew(drives []Drive, caps []int64, hasLayout []bool) (hashspacesolver.State, error) {
	perSpace, used, err := readOwned(drives, hasLayout)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	st, err := stateFromOwned(caps, perSpace)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	for i, ok := range hasLayout {
		if ok {
			continue
		}
		res, err := hashspacesolver.Solve(st, hashspacesolver.Event{
			Kind: hashspacesolver.EventArrive,
			Disk: i,
		})
		if err != nil {
			return hashspacesolver.State{}, xerrors.Errorf("arrive disk %d: %w", i, err)
		}
		st = res.State
	}
	if err := writeState(drives, st, used); err != nil {
		return hashspacesolver.State{}, err
	}
	return st, nil
}

// readOwned loads ranges for drives that already have layouts. hasLayout nil
// means every drive is present. used includes files newer than layout.json.
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
	removeLayoutTemp(root, kind)
	path := filepath.Join(root, kind, layoutFile)
	layout, err := readLayout(path)
	if err != nil {
		return Layout{}, 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Layout{}, 0, err
	}
	d := &disk{root: root, tracker: &sizeTracker{}}
	d.tracker.Set(layout.Used)
	if err := d.catchUp(kind, info.ModTime()); err != nil {
		return Layout{}, 0, err
	}
	return layout, d.tracker.Used(), nil
}

func stateFromOwned(caps []int64, perSpace [][]ownedRange) (hashspacesolver.State, error) {
	spaces := make([]hashspacesolver.Space, len(perSpace))
	for s, rs := range perSpace {
		sortOwned(rs)
		ranges := make([]hashspacesolver.Range, len(rs))
		owners := make([]int, len(rs))
		for i, r := range rs {
			var prev []byte
			if i == 0 {
				prev = rs[len(rs)-1].end
			} else {
				prev = rs[i-1].end
			}
			if !bytes.Equal(prev, r.start) {
				return hashspacesolver.State{}, xerrors.Errorf("space %d ranges do not tile", s)
			}
			ranges[i] = hashspacesolver.Range{EndHash: append([]byte(nil), r.end...), Size: r.size}
			owners[i] = r.disk
		}
		spaces[s] = hashspacesolver.Space{Ranges: ranges, Owner: owners}
	}
	st := hashspacesolver.State{Disks: append([]int64(nil), caps...), Spaces: spaces}
	if err := hashspacesolver.Validate(st); err != nil {
		return hashspacesolver.State{}, err
	}
	return st, nil
}

func sortOwned(rs []ownedRange) {
	for i := 1; i < len(rs); i++ {
		j := i
		for j > 0 && bytes.Compare(rs[j].end, rs[j-1].end) < 0 {
			rs[j], rs[j-1] = rs[j-1], rs[j]
			j--
		}
	}
}

func writeState(drives []Drive, st hashspacesolver.State, used [][2]int64) error {
	if len(st.Spaces) != 2 {
		return xerrors.Errorf("expected 2 hash spaces, got %d", len(st.Spaces))
	}
	now := time.Now().UTC().Truncate(time.Second)
	kinds := []string{DIR_OPEN, DIR_ACL}
	for i, d := range drives {
		for s, kind := range kinds {
			ranges, err := hashRangesFor(st, s, i)
			if err != nil {
				return err
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

func hashRangesFor(st hashspacesolver.State, space, disk int) ([]HashRange, error) {
	if space < 0 || space >= len(st.Spaces) {
		return nil, xerrors.Errorf("unknown space %d", space)
	}
	sp := st.Spaces[space]
	out := make([]HashRange, 0)
	for i, r := range sp.Ranges {
		if sp.Owner[i] != disk {
			continue
		}
		start := hashspacesolver.StartHash(sp.Ranges, i)
		out = append(out, HashRange{
			Start: hexEncode(start),
			End:   hexEncode(r.EndHash),
		})
	}
	return out, nil
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
