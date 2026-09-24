//go:build linux

package fs2

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	ioringOpStatx          = 21
	ioringEnterGetevents   = 1
	ioringFeatSingleMmap   = 1
	ioringOffSQRing        = 0
	ioringOffCQRing        = 0x8000000
	ioringOffSQEs          = 0x10000000
	statxAtFlags           = unix.AT_SYMLINK_NOFOLLOW | unix.AT_NO_AUTOMOUNT | unix.AT_STATX_DONT_SYNC
	statxMask              = unix.STATX_TYPE | unix.STATX_SIZE
	linuxDirentHeaderBytes = 19
)

// ioUringSQE is the kernel submission queue entry. For IORING_OP_STATX the
// pathname pointer is Addr, the statx buffer pointer is Off, the request
// mask is Len, and the AT_* flags are OpFlags.
type ioUringSQE struct {
	Opcode      uint8
	Flags       uint8
	IoPrio      uint16
	Fd          int32
	Off         uint64
	Addr        uint64
	Len         uint32
	OpFlags     uint32
	UserData    uint64
	BufIndex    uint16
	Personality uint16
	SpliceFdIn  int32
	Addr3       uint64
	Pad2        uint64
}

type ioUringCQE struct {
	UserData uint64
	Res      int32
	Flags    uint32
}

type ioSQRingOffsets struct {
	Head        uint32
	Tail        uint32
	RingMask    uint32
	RingEntries uint32
	Flags       uint32
	Dropped     uint32
	Array       uint32
	Resv        uint32
	UserAddr    uint64
}

type ioCQRingOffsets struct {
	Head        uint32
	Tail        uint32
	RingMask    uint32
	RingEntries uint32
	Overflow    uint32
	Cqes        uint32
	Flags       uint32
	Resv        uint32
	UserAddr    uint64
}

type ioUringParams struct {
	SqEntries    uint32
	CqEntries    uint32
	Flags        uint32
	SqThreadCPU  uint32
	SqThreadIdle uint32
	Features     uint32
	WqFd         uint32
	Resv         [3]uint32
	SqOff        ioSQRingOffsets
	CqOff        ioCQRingOffsets
}

type statSlot struct {
	name [unix.NAME_MAX + 1]byte
	nlen int
	stx  unix.Statx_t
}

type uring struct {
	fd      int
	sqRing  []byte
	cqRing  []byte
	sqes    []byte
	sqHead  *uint32
	sqTail  *uint32
	sqMask  uint32
	sqArray []uint32
	cqHead  *uint32
	cqTail  *uint32
	cqMask  uint32
	cqes    []ioUringCQE
	slots   []statSlot
	pending int
	tail    uint32
}

const (
	_ = uint64(unsafe.Sizeof(ioUringSQE{}) - 64)
	_ = uint64(64 - unsafe.Sizeof(ioUringSQE{}))
	_ = uint64(unsafe.Sizeof(ioUringCQE{}) - 16)
	_ = uint64(16 - unsafe.Sizeof(ioUringCQE{}))
	_ = uint64(unsafe.Sizeof(ioUringParams{}) - 120)
	_ = uint64(120 - unsafe.Sizeof(ioUringParams{}))
)

// SumFileSizesRange sums logical file sizes for regular files under directory
// whose concatenated hash paths compare in the bytewise interval (low, high].
// An empty low or high bound leaves that side of the interval open.
//
// QueueDepth is the io_uring queue size and the maximum number of outstanding
// statx requests for one directory. Zero selects 128.
//
// Requires: Linux 5.15+ (all Ubuntu LTSs support it). liburing not required (reimplemented here).
func SumFileSizesRange(directory, low, high string, queueDepth uint32) (Result, error) {
	if err := checkSumArgs(directory, low, high, queueDepth); err != nil {
		return Result{}, err
	}
	if queueDepth == 0 {
		queueDepth = 128
	}

	ring, err := newUring(queueDepth)
	if err != nil {
		return Result{}, fmt.Errorf("sum file sizes: %w", err)
	}
	defer ring.close()

	var result Result
	err = sumDirLinux(ring, directory, "", low, high, &result)
	if err != nil {
		return result, fmt.Errorf("sum file sizes: %w", err)
	}
	return result, nil
}

func newUring(entries uint32) (*uring, error) {
	var params ioUringParams
	r1, _, errno := unix.Syscall(unix.SYS_IO_URING_SETUP, uintptr(entries), uintptr(unsafe.Pointer(&params)), 0)
	if errno != 0 {
		return nil, fmt.Errorf("io_uring_setup: %w", errno)
	}
	ringfd := int(r1)

	ring := &uring{fd: ringfd, slots: make([]statSlot, entries)}
	sqSize := int(params.SqOff.Array) + int(params.SqEntries)*4
	cqSize := int(params.CqOff.Cqes) + int(params.CqEntries)*int(unsafe.Sizeof(ioUringCQE{}))
	single := params.Features&ioringFeatSingleMmap != 0
	if single && cqSize > sqSize {
		sqSize = cqSize
	}
	sqRing, err := unix.Mmap(ringfd, ioringOffSQRing, sqSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(ringfd)
		return nil, fmt.Errorf("mmap sq ring: %w", err)
	}
	ring.sqRing = sqRing
	if single {
		ring.cqRing = sqRing
	} else {
		cqRing, err := unix.Mmap(ringfd, ioringOffCQRing, cqSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			unix.Munmap(sqRing)
			unix.Close(ringfd)
			return nil, fmt.Errorf("mmap cq ring: %w", err)
		}
		ring.cqRing = cqRing
	}
	sqeSize := int(params.SqEntries) * int(unsafe.Sizeof(ioUringSQE{}))
	sqes, err := unix.Mmap(ringfd, ioringOffSQEs, sqeSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		if !single {
			unix.Munmap(ring.cqRing)
		}
		unix.Munmap(sqRing)
		unix.Close(ringfd)
		return nil, fmt.Errorf("mmap sqes: %w", err)
	}
	ring.sqes = sqes

	sqBase := unsafe.Pointer(&sqRing[0])
	cqBase := unsafe.Pointer(&ring.cqRing[0])
	ring.sqHead = (*uint32)(unsafe.Add(sqBase, params.SqOff.Head))
	ring.sqTail = (*uint32)(unsafe.Add(sqBase, params.SqOff.Tail))
	ring.sqMask = *(*uint32)(unsafe.Add(sqBase, params.SqOff.RingMask))
	arrayOff := int(params.SqOff.Array)
	ring.sqArray = unsafe.Slice((*uint32)(unsafe.Add(sqBase, arrayOff)), params.SqEntries)
	for i := range ring.sqArray {
		ring.sqArray[i] = uint32(i)
	}
	ring.cqHead = (*uint32)(unsafe.Add(cqBase, params.CqOff.Head))
	ring.cqTail = (*uint32)(unsafe.Add(cqBase, params.CqOff.Tail))
	ring.cqMask = *(*uint32)(unsafe.Add(cqBase, params.CqOff.RingMask))
	ring.cqes = unsafe.Slice((*ioUringCQE)(unsafe.Add(cqBase, params.CqOff.Cqes)), params.CqEntries)
	ring.tail = atomic.LoadUint32(ring.sqTail)
	return ring, nil
}

func (ring *uring) close() {
	if ring.sqes != nil {
		unix.Munmap(ring.sqes)
	}
	if ring.cqRing != nil && (len(ring.sqRing) == 0 || &ring.cqRing[0] != &ring.sqRing[0]) {
		unix.Munmap(ring.cqRing)
	}
	if ring.sqRing != nil {
		unix.Munmap(ring.sqRing)
	}
	if ring.fd >= 0 {
		unix.Close(ring.fd)
	}
}

func (ring *uring) prepStatx(slot int, dirfd int) error {
	head := atomic.LoadUint32(ring.sqHead)
	next := ring.tail + 1
	if next-head > uint32(len(ring.sqArray)) {
		return fmt.Errorf("io_uring_get_sqe: %w", unix.EBUSY)
	}
	index := ring.tail & ring.sqMask
	sqeIndex := ring.sqArray[index]
	sqe := (*ioUringSQE)(unsafe.Add(unsafe.Pointer(&ring.sqes[0]), uintptr(sqeIndex)*unsafe.Sizeof(ioUringSQE{})))
	*sqe = ioUringSQE{
		Opcode:   ioringOpStatx,
		Fd:       int32(dirfd),
		Off:      uint64(uintptr(unsafe.Pointer(&ring.slots[slot].stx))),
		Addr:     uint64(uintptr(unsafe.Pointer(&ring.slots[slot].name[0]))),
		Len:      statxMask,
		OpFlags:  statxAtFlags,
		UserData: uint64(slot),
	}
	ring.tail = next
	return nil
}

func (ring *uring) submitAndDrain(want int, result *Result) error {
	if want == 0 {
		return nil
	}
	var pinner runtime.Pinner
	for i := 0; i < want; i++ {
		pinner.Pin(&ring.slots[i].stx)
		pinner.Pin(&ring.slots[i].name[0])
	}
	defer pinner.Unpin()

	atomic.StoreUint32(ring.sqTail, ring.tail)
	submitted := 0
	for submitted < want {
		n, err := ring.enter(uint32(want-submitted), 0, 0)
		if err != nil {
			if submitted > 0 {
				ring.drain(submitted, result)
			}
			return fmt.Errorf("io_uring_enter: %w", err)
		}
		if n == 0 {
			if submitted > 0 {
				ring.drain(submitted, result)
			}
			return fmt.Errorf("io_uring_enter: %w", unix.EIO)
		}
		submitted += n
	}

	_, err := ring.drain(want, result)
	return err
}

func (ring *uring) enter(toSubmit, minComplete uint32, flags uint32) (int, error) {
	for {
		n, _, errno := unix.Syscall6(
			unix.SYS_IO_URING_ENTER,
			uintptr(ring.fd),
			uintptr(toSubmit),
			uintptr(minComplete),
			uintptr(flags),
			0,
			0,
		)
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			return 0, errno
		}
		return int(n), nil
	}
}

// drain consumes want completions. The first per-file failure is returned
// after the ring is empty so the slots can be reused.
func (ring *uring) drain(want int, result *Result) (int, error) {
	var failed error
	got := 0
	for got < want {
		head := atomic.LoadUint32(ring.cqHead)
		tail := atomic.LoadUint32(ring.cqTail)
		if head == tail {
			if _, err := ring.enter(0, 1, ioringEnterGetevents); err != nil {
				if failed == nil {
					failed = fmt.Errorf("io_uring_enter: %w", err)
				}
				return got, failed
			}
			continue
		}
		cqe := ring.cqes[head&ring.cqMask]
		atomic.StoreUint32(ring.cqHead, head+1)
		got++

		slot := int(cqe.UserData)
		if slot < 0 || slot >= len(ring.slots) {
			if failed == nil {
				failed = fmt.Errorf("statx: invalid completion")
			}
			continue
		}
		name := cString(ring.slots[slot].name[:ring.slots[slot].nlen])
		if cqe.Res == -int32(unix.ENOENT) {
			result.Vanished++
			continue
		}
		if cqe.Res < 0 {
			if failed == nil {
				failed = fmt.Errorf("statx %s: %w", name, unix.Errno(-cqe.Res))
			}
			continue
		}
		stx := &ring.slots[slot].stx
		if stx.Mask&unix.STATX_SIZE == 0 {
			if failed == nil {
				failed = fmt.Errorf("statx %s: filesystem did not return size", name)
			}
			continue
		}
		if stx.Mode&unix.S_IFMT != unix.S_IFREG {
			continue
		}
		if stx.Size > uint64(^uint64(0)>>1) {
			if failed == nil {
				failed = fmt.Errorf("statx %s: size overflows int64", name)
			}
			continue
		}
		result.Bytes += int64(stx.Size)
		result.Files++
	}
	ring.pending = 0
	return got, failed
}

func sumDirLinux(ring *uring, path, prefix, low, high string, result *Result) (err error) {
	dirfd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		if cerr := unix.Close(dirfd); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cerr)
		}
	}()

	buf := make([]byte, 64*1024)
	for {
		n, rerr := unix.ReadDirent(dirfd, buf)
		if rerr == unix.EINTR {
			continue
		}
		if rerr != nil {
			return fmt.Errorf("getdents %s: %w", path, rerr)
		}
		if n == 0 {
			break
		}
		rest := buf[:n]
		for len(rest) > 0 {
			if len(rest) < linuxDirentHeaderBytes {
				return fmt.Errorf("getdents %s: truncated entry", path)
			}
			reclen := int(binary.NativeEndian.Uint16(rest[16:18]))
			if reclen < linuxDirentHeaderBytes || reclen > len(rest) {
				return fmt.Errorf("getdents %s: invalid entry length", path)
			}
			typ := rest[18]
			nameBytes := rest[19:reclen]
			nul := 0
			for nul < len(nameBytes) && nameBytes[nul] != 0 {
				nul++
			}
			name := string(nameBytes[:nul])
			rest = rest[reclen:]
			if name == "." || name == ".." || name == "" {
				continue
			}

			switch typ {
			case unix.DT_DIR:
				if err := ring.flush(result); err != nil {
					return err
				}
				if err := walkChild(ring, path, prefix, name, low, high, result); err != nil {
					return err
				}
			case unix.DT_UNKNOWN:
				if err := ring.flush(result); err != nil {
					return err
				}
				if err := statUnknown(ring, dirfd, path, prefix, name, low, high, result); err != nil {
					return err
				}
			case unix.DT_REG:
				hash, err := joinHash(prefix, name)
				if err != nil {
					return err
				}
				if !hashInRange(hash, low, high) {
					continue
				}
				if err := ring.queue(dirfd, name, result); err != nil {
					return err
				}
			}
		}
	}
	return ring.flush(result)
}

func (ring *uring) queue(dirfd int, name string, result *Result) error {
	if ring.pending >= len(ring.slots) {
		if err := ring.flush(result); err != nil {
			return err
		}
	}
	slot := &ring.slots[ring.pending]
	if len(name) >= len(slot.name) {
		return fmt.Errorf("filename exceeds NAME_MAX: %s", name)
	}
	slot.nlen = copy(slot.name[:], name)
	slot.name[slot.nlen] = 0
	slot.nlen++
	slot.stx = unix.Statx_t{}
	if err := ring.prepStatx(ring.pending, dirfd); err != nil {
		return err
	}
	ring.pending++
	return nil
}

func (ring *uring) flush(result *Result) error {
	pending := ring.pending
	ring.pending = 0
	return ring.submitAndDrain(pending, result)
}

func walkChild(ring *uring, path, prefix, name, low, high string, result *Result) error {
	hash, err := joinHash(prefix, name)
	if err != nil {
		return err
	}
	if !subtreeCanMatch(hash, low, high) {
		return nil
	}
	child, err := joinPath(path, name)
	if err != nil {
		return err
	}
	return sumDirLinux(ring, child, hash, low, high, result)
}

func statUnknown(ring *uring, dirfd int, path, prefix, name, low, high string, result *Result) error {
	var stx unix.Statx_t
	err := unix.Statx(dirfd, name, statxAtFlags, statxMask, &stx)
	if err == unix.ENOENT {
		result.Vanished++
		return nil
	}
	if err != nil {
		return fmt.Errorf("statx %s: %w", name, err)
	}
	hash, err := joinHash(prefix, name)
	if err != nil {
		return err
	}
	switch stx.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		if !subtreeCanMatch(hash, low, high) {
			return nil
		}
		child, err := joinPath(path, name)
		if err != nil {
			return err
		}
		return sumDirLinux(ring, child, hash, low, high, result)
	case unix.S_IFREG:
		if !hashInRange(hash, low, high) {
			return nil
		}
		if stx.Mask&unix.STATX_SIZE == 0 {
			return fmt.Errorf("statx %s: filesystem did not return size", name)
		}
		if stx.Size > uint64(^uint64(0)>>1) {
			return fmt.Errorf("statx %s: size overflows int64", name)
		}
		result.Bytes += int64(stx.Size)
		result.Files++
	}
	return nil
}

func cString(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
