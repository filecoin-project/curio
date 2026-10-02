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
	"github.com/filecoin-project/curio/lib/storiface"
)

// errNoPieceDrive is returned when every drive denies piece park, so there
// is nothing to seed a hash space onto.
var errNoPieceDrive = xerrors.New("no drive accepts piece park")

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
		openOK, err := fileExists(filepath.Join(d.Root, DIR_OPEN, layoutFile))
		if err != nil {
			return hashspacesolver.State{}, err
		}
		aclOK, err := fileExists(filepath.Join(d.Root, DIR_ACL, layoutFile))
		if err != nil {
			return hashspacesolver.State{}, err
		}
		var hasBoth, hasNeither bool
		switch {
		case openOK && aclOK:
			hasBoth = true
		case !openOK && !aclOK:
			hasNeither = true
		default:
			return hashspacesolver.State{}, xerrors.Errorf("%s has a layout for only one hash space", d.Root)
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
		seedCaps, err := pieceCaps(drives, caps)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		positive := false
		for _, c := range seedCaps {
			if c > 0 {
				positive = true
				break
			}
		}
		if !positive {
			return hashspacesolver.State{}, errNoPieceDrive
		}
		st, err := seedState(caps, seedCaps)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		if err := writeState(drives, st, make([][2]int64, len(drives))); err != nil {
			return hashspacesolver.State{}, err
		}
		return st, nil
	case nBoth == len(drives):
		// loadState
		perSpace, _, err := readOwned(drives, nil)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		return stateFromOwned(caps, perSpace)
	default:
		// arriveNew
		perSpace, used, err := readOwned(drives, both)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		st, err := stateFromOwned(caps, perSpace)
		if err != nil {
			return hashspacesolver.State{}, err
		}
		for i, ok := range both {
			if ok {
				continue
			}
			deny, err := deniesPiecePark(drives[i].Root)
			if err != nil {
				return hashspacesolver.State{}, err
			}
			if deny {
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
	b, err := os.ReadFile(filepath.Join(d.Root, sectorStoreFile))
	var maxStorage uint64
	if err != nil {
		if !os.IsNotExist(err) {
			return 0, xerrors.Errorf("reading sectorstore.json in %s: %w", d.Root, err)
		}
	} else {
		var meta struct {
			MaxStorage uint64
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			return 0, xerrors.Errorf("decoding sectorstore.json in %s: %w", d.Root, err)
		}
		maxStorage = meta.MaxStorage
	}
	if maxStorage > 0 && maxStorage < uint64(fsCap) {
		return int64(maxStorage), nil
	}
	return fsCap, nil
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

// pieceCaps is caps with drives that deny piece park zeroed, so seeding and
// arrival skip them.
func pieceCaps(drives []Drive, caps []int64) ([]int64, error) {
	out := append([]int64(nil), caps...)
	for i, d := range drives {
		deny, err := deniesPiecePark(d.Root)
		if err != nil {
			return nil, err
		}
		if deny {
			out[i] = 0
		}
	}
	return out, nil
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

func seedState(caps, seedCaps []int64) (hashspacesolver.State, error) {
	open, err := seedSpace(seedCaps)
	if err != nil {
		return hashspacesolver.State{}, err
	}
	acl, err := seedSpace(seedCaps)
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
			// intToHash
			raw := num.Bytes()
			end = make([]byte, HASH_BYTES)
			if len(raw) > HASH_BYTES {
				copy(end, raw[len(raw)-HASH_BYTES:])
			} else {
				copy(end[HASH_BYTES-len(raw):], raw)
			}
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
		// sortOwned
		for i := 1; i < len(rs); i++ {
			j := i
			for j > 0 && bytes.Compare(rs[j].end, rs[j-1].end) < 0 {
				rs[j], rs[j-1] = rs[j-1], rs[j]
				j--
			}
		}
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

func writeState(drives []Drive, st hashspacesolver.State, used [][2]int64) error {
	if len(st.Spaces) != 2 {
		return xerrors.Errorf("expected 2 hash spaces, got %d", len(st.Spaces))
	}
	now := time.Now().UTC().Truncate(time.Second)
	kinds := []string{DIR_OPEN, DIR_ACL}
	for i, d := range drives {
		for s, kind := range kinds {
			// hashRangesFor
			if s < 0 || s >= len(st.Spaces) {
				return xerrors.Errorf("unknown space %d", s)
			}
			sp := st.Spaces[s]
			ranges := make([]HashRange, 0)
			for ri, r := range sp.Ranges {
				if sp.Owner[ri] != i {
					continue
				}
				start := hashspacesolver.StartHash(sp.Ranges, ri)
				ranges = append(ranges, HashRange{
					Start: hexEncode(start),
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
