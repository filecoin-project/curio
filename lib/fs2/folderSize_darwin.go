//go:build darwin

package fs2

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	vtypeReg = 1
	vtypeDir = 2
)

// SumFileSizesRange sums logical file sizes for regular files under directory
// whose concatenated hash paths compare in the bytewise interval (low, high].
// An empty low or high bound leaves that side of the interval open.
//
// QueueDepth sizes the getattrlistbulk attribute buffer, at 512 bytes per
// entry. Zero selects 128. The buffer is at least 8192 bytes.
//
// Performance: 1e6 files took 2s and 1 MB RAM on MacBook Pro M2
// .......vs unix impl taking 20s and 442 MB RAM on the same machine.
func SumFileSizesRange(directory, low, high string, queueDepth uint32) (Result, error) {
	if err := checkSumArgs(directory, low, high, queueDepth); err != nil {
		return Result{}, err
	}
	if queueDepth == 0 {
		queueDepth = 128
	}

	var result Result
	err := sumDirDarwin(directory, "", low, high, queueDepth, &result)
	if err != nil {
		return result, fmt.Errorf("sum file sizes: %w", err)
	}
	return result, nil
}

func sumDirDarwin(path, prefix, low, high string, queueDepth uint32, result *Result) (err error) {
	bufLen := int(queueDepth) * 512
	if bufLen < 8192 {
		bufLen = 8192
	}

	dirfd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		if cerr := unix.Close(dirfd); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cerr)
		}
	}()

	attr := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_NAME | unix.ATTR_CMN_ERROR | unix.ATTR_CMN_OBJTYPE,
		Fileattr:    unix.ATTR_FILE_DATALENGTH,
	}
	buf := make([]byte, bufLen)

	for {
		var retcount int
		for {
			r1, _, errno := unix.Syscall6(
				unix.SYS_GETATTRLISTBULK,
				uintptr(dirfd),
				uintptr(unsafe.Pointer(&attr)),
				uintptr(unsafe.Pointer(&buf[0])),
				uintptr(len(buf)),
				0,
				0,
			)
			if errno == unix.EINTR {
				continue
			}
			if errno != 0 {
				return fmt.Errorf("getattrlistbulk %s: %w", path, errno)
			}
			retcount = int(r1)
			break
		}
		if retcount == 0 {
			return nil
		}

		offset := 0
		for i := 0; i < retcount; i++ {
			if offset+4 > len(buf) {
				return fmt.Errorf("getattrlistbulk %s: truncated entry length", path)
			}
			length := int(binary.LittleEndian.Uint32(buf[offset:]))
			if length < 4 || offset+length > len(buf) {
				return fmt.Errorf("getattrlistbulk %s: invalid entry length", path)
			}
			entry := buf[offset : offset+length]
			offset += length

			name, objType, size, hasSize, hasType, entryErr, perr := parseAttrEntry(entry)
			if perr != nil {
				return fmt.Errorf("getattrlistbulk %s: %w", path, perr)
			}
			if entryErr != 0 {
				if entryErr == unix.ENOENT {
					result.Vanished++
					continue
				}
				if name == "" {
					return fmt.Errorf("getattrlistbulk %s: %w", path, entryErr)
				}
				return fmt.Errorf("getattrlistbulk %s: %w", name, entryErr)
			}
			if name == "" {
				return fmt.Errorf("getattrlistbulk %s: filesystem did not return name", path)
			}
			if !hasType {
				return fmt.Errorf("getattrlistbulk %s: filesystem did not return type", name)
			}
			if name == "." || name == ".." {
				continue
			}

			hash, err := joinHash(prefix, name)
			if err != nil {
				return err
			}
			switch objType {
			case vtypeDir:
				if !subtreeCanMatch(hash, low, high) {
					continue
				}
				child, err := joinPath(path, name)
				if err != nil {
					return err
				}
				if err := sumDirDarwin(child, hash, low, high, queueDepth, result); err != nil {
					return err
				}
			case vtypeReg:
				if !hashInRange(hash, low, high) {
					continue
				}
				if !hasSize {
					return fmt.Errorf("getattrlistbulk %s: filesystem did not return size", name)
				}
				if size < 0 {
					return fmt.Errorf("getattrlistbulk %s: negative size", name)
				}
				result.Bytes += size
				result.Files++
			}
		}
	}
}

// parseAttrEntry reads one getattrlistbulk record. Variable-length values,
// such as the name, are addressed by an offset from their header; the fixed
// fields that follow stay packed in attribute-bit order.
func parseAttrEntry(entry []byte) (name string, objType uint32, size int64, hasSize, hasType bool, entryErr unix.Errno, err error) {
	if len(entry) < 4+20 {
		return "", 0, 0, false, false, 0, fmt.Errorf("truncated returned attributes")
	}
	field := entry[4:]
	returnedCommon := binary.LittleEndian.Uint32(field[0:4])
	returnedFile := binary.LittleEndian.Uint32(field[12:16])
	field = field[20:]

	if returnedCommon&unix.ATTR_CMN_NAME != 0 {
		if len(field) < 8 {
			return "", 0, 0, false, false, 0, fmt.Errorf("truncated name")
		}
		dataOff := int32(binary.LittleEndian.Uint32(field[0:4]))
		dataLen := int(binary.LittleEndian.Uint32(field[4:8]))
		if dataOff < 0 || dataLen == 0 || int(dataOff) > len(field) || int(dataOff)+dataLen > len(field) {
			return "", 0, 0, false, false, 0, fmt.Errorf("invalid name")
		}
		raw := field[dataOff : int(dataOff)+dataLen]
		n := 0
		for n < len(raw) && raw[n] != 0 {
			n++
		}
		name = string(raw[:n])
		field = field[8:]
	}
	if returnedCommon&unix.ATTR_CMN_OBJTYPE != 0 {
		if len(field) < 4 {
			return "", 0, 0, false, false, 0, fmt.Errorf("truncated type")
		}
		objType = binary.LittleEndian.Uint32(field[:4])
		hasType = true
		field = field[4:]
	}
	if returnedCommon&unix.ATTR_CMN_ERROR != 0 {
		if len(field) < 4 {
			return "", 0, 0, false, false, 0, fmt.Errorf("truncated entry error")
		}
		entryErr = unix.Errno(binary.LittleEndian.Uint32(field[:4]))
		field = field[4:]
	}
	if returnedFile&unix.ATTR_FILE_DATALENGTH != 0 {
		if len(field) < 8 {
			return "", 0, 0, false, false, 0, fmt.Errorf("truncated size")
		}
		size = int64(binary.LittleEndian.Uint64(field[:8]))
		hasSize = true
	}
	return name, objType, size, hasSize, hasType, entryErr, nil
}
