//go:build linux && cgo

package fs2

/*
#cgo CFLAGS: -D_GNU_SOURCE -std=gnu11
#cgo pkg-config: liburing

#define _GNU_SOURCE

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#include <liburing.h>

#ifndef NAME_MAX
#define NAME_MAX 255
#endif
#ifndef AT_NO_AUTOMOUNT
#define AT_NO_AUTOMOUNT 0x800
#endif
#ifndef AT_STATX_DONT_SYNC
#define AT_STATX_DONT_SYNC 0x4000
#endif
#ifndef STATX_TYPE
#include <linux/stat.h>
#endif

struct sum_slot {
	char name[NAME_MAX + 1];
	struct statx stx;
};

static void set_error(char *dst, size_t dst_len, const char *operation,
			  const char *name, int error_number) {
	if (dst == NULL || dst_len == 0) {
		return;
	}

	if (name != NULL) {
		snprintf(dst, dst_len, "%s %s: %s", operation, name,
			 strerror(error_number));
	} else {
		snprintf(dst, dst_len, "%s: %s", operation,
			 strerror(error_number));
	}
}

// submit_all submits every SQE currently prepared for this batch. It reports
// how many requests reached the kernel so the caller can drain them before
// releasing their pathname and output buffers after an error.
static int submit_all(struct io_uring *ring, unsigned wanted,
			  unsigned *submitted, char *err, size_t err_len) {
	*submitted = 0;

	while (*submitted < wanted) {
		int rc = io_uring_submit(ring);
		if (rc == -EINTR) {
			continue;
		}
		if (rc < 0) {
			set_error(err, err_len, "io_uring_submit", NULL, -rc);
			return -1;
		}
		if (rc == 0) {
			set_error(err, err_len, "io_uring_submit", NULL, EIO);
			return -1;
		}

		*submitted += (unsigned)rc;
	}

	return 0;
}

// drain_batch consumes exactly pending completions. It remembers the first
// per-file failure but continues draining so every slot can safely be reused.
static int drain_batch(struct io_uring *ring, unsigned pending,
			   uint64_t *total, uint64_t *files,
			   uint64_t *vanished, char *err, size_t err_len) {
	int failed = 0;

	for (unsigned i = 0; i < pending; i++) {
		struct io_uring_cqe *cqe;
		int rc;

		do {
			rc = io_uring_wait_cqe(ring, &cqe);
		} while (rc == -EINTR);

		if (rc < 0) {
			if (!failed) {
				set_error(err, err_len, "io_uring_wait_cqe", NULL, -rc);
			}
			return -1;
		}

		struct sum_slot *slot = (struct sum_slot *)io_uring_cqe_get_data(cqe);
		int result = cqe->res;
		io_uring_cqe_seen(ring, cqe);

		if (result == -ENOENT) {
			(*vanished)++;
			continue;
		}

		if (result < 0) {
			if (!failed) {
				set_error(err, err_len, "statx", slot->name, -result);
				failed = 1;
			}
			continue;
		}

		if ((slot->stx.stx_mask & STATX_SIZE) == 0) {
			if (!failed) {
				snprintf(err, err_len, "statx %s: filesystem did not return size",
					 slot->name);
				failed = 1;
			}
			continue;
		}

		if ((slot->stx.stx_mode & S_IFMT) != S_IFREG) {
			continue;
		}

		if (UINT64_MAX - *total < slot->stx.stx_size) {
			if (!failed) {
				snprintf(err, err_len, "sum overflow at %s", slot->name);
				failed = 1;
			}
			continue;
		}

		*total += slot->stx.stx_size;
		(*files)++;
	}

	return failed ? -1 : 0;
}

// sum_file_sizes_range scans one directory, selecting regular files whose
// names compare in the bytewise interval [low, high). An empty bound is open.
// queue_depth is both the ring size and the maximum number of outstanding
// statx requests.
int sum_file_sizes_range(const char *path, const char *low,
				const char *high, unsigned queue_depth,
				uint64_t *total, uint64_t *files,
				uint64_t *vanished, char *err, size_t err_len) {
	DIR *directory = NULL;
	struct io_uring ring;
	struct sum_slot *slots = NULL;
	int ring_ready = 0;
	int status = -1;
	int saved_errno;

	*total = 0;
	*files = 0;
	*vanished = 0;
	if (err != NULL && err_len != 0) {
		err[0] = '\0';
	}

	if (queue_depth == 0) {
		queue_depth = 128;
	}

	directory = opendir(path);
	if (directory == NULL) {
		set_error(err, err_len, "opendir", path, errno);
		goto done;
	}

	int rc = io_uring_queue_init(queue_depth, &ring, 0);
	if (rc < 0) {
		set_error(err, err_len, "io_uring_queue_init", NULL, -rc);
		goto done;
	}
	ring_ready = 1;

	slots = calloc(queue_depth, sizeof(*slots));
	if (slots == NULL) {
		set_error(err, err_len, "calloc", NULL, ENOMEM);
		goto done;
	}

	int directory_fd = dirfd(directory);
	if (directory_fd == -1) {
		set_error(err, err_len, "dirfd", path, errno);
		goto done;
	}

	int at_flags = AT_SYMLINK_NOFOLLOW | AT_NO_AUTOMOUNT |
		       AT_STATX_DONT_SYNC;
	int reached_end = 0;

	while (!reached_end) {
		unsigned pending = 0;

		while (pending < queue_depth) {
			errno = 0;
			struct dirent *entry = readdir(directory);
			if (entry == NULL) {
				if (errno != 0) {
					set_error(err, err_len, "readdir", path, errno);
					goto done;
				}
				reached_end = 1;
				break;
			}

			const char *name = entry->d_name;
			if (name[0] == '.' &&
			    (name[1] == '\0' ||
			     (name[1] == '.' && name[2] == '\0'))) {
				continue;
			}

			if (low[0] != '\0' && strcmp(name, low) < 0) {
				continue;
			}
			if (high[0] != '\0' && strcmp(name, high) >= 0) {
				continue;
			}

			// A filesystem may report DT_UNKNOWN, so those entries still
			// need statx. Known non-regular entries can be skipped.
			if (entry->d_type != DT_REG && entry->d_type != DT_UNKNOWN) {
				continue;
			}

			size_t name_len = strlen(name);
			if (name_len >= sizeof(slots[pending].name)) {
				snprintf(err, err_len, "filename exceeds NAME_MAX: %s", name);
				goto done;
			}

			memcpy(slots[pending].name, name, name_len + 1);
			memset(&slots[pending].stx, 0, sizeof(slots[pending].stx));

			struct io_uring_sqe *sqe = io_uring_get_sqe(&ring);
			if (sqe == NULL) {
				set_error(err, err_len, "io_uring_get_sqe", NULL, EBUSY);
				goto done;
			}

			io_uring_prep_statx(sqe, directory_fd,
					    slots[pending].name, at_flags,
					    STATX_TYPE | STATX_SIZE,
					    &slots[pending].stx);
			io_uring_sqe_set_data(sqe, &slots[pending]);
			pending++;
		}

		if (pending == 0) {
			continue;
		}

		unsigned submitted = 0;
		if (submit_all(&ring, pending, &submitted, err, err_len) != 0) {
			// Requests accepted before a partial-submit failure still
			// reference slot memory. Drain those before cleanup.
			if (submitted != 0) {
				char ignored[1];
				drain_batch(&ring, submitted, total, files, vanished,
					    ignored, sizeof(ignored));
			}
			goto done;
		}

		if (drain_batch(&ring, pending, total, files, vanished,
					err, err_len) != 0) {
			goto done;
		}
	}

	status = 0;

done:
	// Preserve errno only for closedir diagnostics; liburing returns
	// negative errno values directly and does not reliably set errno.
	saved_errno = errno;
	if (ring_ready) {
		io_uring_queue_exit(&ring);
	}
	free(slots);
	if (directory != NULL && closedir(directory) == -1 && status == 0) {
		set_error(err, err_len, "closedir", path, errno);
		status = -1;
	}
	errno = saved_errno;
	return status;
}
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

// SumFileSizesRange sums logical file sizes for regular files immediately
// within directory whose names compare in the bytewise interval [low, high).
// An empty low or high bound leaves that side of the interval open.
//
// QueueDepth controls the maximum number of outstanding io_uring statx
// requests. Zero selects 128. This function makes one long cgo call; it does
// not allocate Go objects per directory entry and cannot be canceled midway.
func SumFileSizesRange(directory, low, high string, queueDepth uint32) (Result, error) {
	if strings.IndexByte(directory, 0) >= 0 ||
		strings.IndexByte(low, 0) >= 0 ||
		strings.IndexByte(high, 0) >= 0 {
		return Result{}, fmt.Errorf("sum file sizes: path and bounds cannot contain NUL bytes")
	}
	if queueDepth > 4096 {
		return Result{}, fmt.Errorf("sum file sizes: queue depth %d exceeds 4096", queueDepth)
	}

	cDirectory := C.CString(directory)
	cLow := C.CString(low)
	cHigh := C.CString(high)
	defer C.free(unsafe.Pointer(cDirectory))
	defer C.free(unsafe.Pointer(cLow))
	defer C.free(unsafe.Pointer(cHigh))

	var result Result
	var total C.uint64_t
	var files C.uint64_t
	var vanished C.uint64_t
	var errorBuffer [512]C.char

	status := C.sum_file_sizes_range(
		cDirectory,
		cLow,
		cHigh,
		C.uint(queueDepth),
		&total,
		&files,
		&vanished,
		&errorBuffer[0],
		C.size_t(len(errorBuffer)),
	)

	result.Bytes = uint64(total)
	result.Files = uint64(files)
	result.Vanished = uint64(vanished)

	if status != 0 {
		message := C.GoString(&errorBuffer[0])
		if message == "" {
			message = "directory scan failed"
		}
		return result, fmt.Errorf("sum file sizes: %s", message)
	}

	return result, nil
}
