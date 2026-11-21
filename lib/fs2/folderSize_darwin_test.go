//go:build darwin

package fs2

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestGetAttrListBulkError(t *testing.T) {
	attr := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT}
	_, err := getAttrListBulk(-1, &attr, make([]byte, 8192), 0)
	if !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid descriptor: got %v, want EBADF", err)
	}
}

// bulkEntry is one getattrlistbulk record parsed the same way as the scanner.
type bulkEntry struct {
	name     string
	objType  uint32
	size     int64
	hasSize  bool
	hasType  bool
	entryErr unix.Errno
}

// readBulk asks the kernel for the same attributes the scanner uses.
// options is passed through so a test can force ATTR_CMN_ERROR into the
// buffer with FSOPT_PACK_INVAL_ATTRS.
func readBulk(t *testing.T, dir string, options uint64) []bulkEntry {
	t.Helper()

	dirfd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := unix.Close(dirfd); cerr != nil {
			t.Error(cerr)
		}
	}()

	attr := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_NAME | unix.ATTR_CMN_ERROR | unix.ATTR_CMN_OBJTYPE,
		Fileattr:    unix.ATTR_FILE_DATALENGTH,
	}
	buf := make([]byte, 8192)
	var out []bulkEntry
	for {
		retcount, err := getAttrListBulk(dirfd, &attr, buf, options)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			t.Fatalf("getattrlistbulk: %v", err)
		}
		if retcount == 0 {
			return out
		}

		offset := 0
		for i := 0; i < retcount; i++ {
			if offset+4 > len(buf) {
				t.Fatal("truncated entry length")
			}
			length := int(binary.LittleEndian.Uint32(buf[offset:]))
			if length < 4 || offset+length > len(buf) {
				t.Fatalf("invalid entry length %d at %d", length, offset)
			}
			name, objType, size, hasSize, hasType, entryErr, perr := parseAttrEntry(buf[offset : offset+length])
			if perr != nil {
				t.Fatal(perr)
			}
			offset += length
			if name == "." || name == ".." {
				continue
			}
			out = append(out, bulkEntry{
				name:     name,
				objType:  objType,
				size:     size,
				hasSize:  hasSize,
				hasType:  hasType,
				entryErr: entryErr,
			})
		}
	}
}

func TestParseAttrEntryDarwin(t *testing.T) {
	dir := t.TempDir()
	const fileSize = 6
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Default options omit ATTR_CMN_ERROR on success. FSOPT_PACK_INVAL_ATTRS
	// packs a zero error word immediately after the returned bitmap, which is
	// the same slot a real per-entry errno occupies.
	for _, options := range []uint64{0, unix.FSOPT_PACK_INVAL_ATTRS} {
		entries := readBulk(t, dir, options)
		got := map[string]bulkEntry{}
		for _, e := range entries {
			if _, ok := got[e.name]; ok {
				t.Fatalf("options %#x: duplicate name %q", options, e.name)
			}
			got[e.name] = e
		}

		file, ok := got["hello.txt"]
		if !ok {
			t.Fatalf("options %#x: missing hello.txt in %+v", options, entries)
		}
		if !file.hasType || file.objType != vtypeReg || !file.hasSize || file.size != fileSize || file.entryErr != 0 {
			t.Fatalf("options %#x: hello.txt = %+v", options, file)
		}

		sub, ok := got["subdir"]
		if !ok {
			t.Fatalf("options %#x: missing subdir in %+v", options, entries)
		}
		if !sub.hasType || sub.objType != vtypeDir || sub.hasSize || sub.entryErr != 0 {
			t.Fatalf("options %#x: subdir = %+v", options, sub)
		}
	}
}
