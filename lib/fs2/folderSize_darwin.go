//go:build darwin && cgo

package fs2

/*
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/attr.h>
#include <sys/vnode.h>
#include <unistd.h>

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

static int parse_u32(char **field, const char *end, uint32_t *out) {
	if (*field == NULL || (size_t)(end - *field) < sizeof(*out)) {
		return -1;
	}
	memcpy(out, *field, sizeof(*out));
	*field += sizeof(*out);
	return 0;
}

static int parse_obj_type(char **field, const char *end, fsobj_type_t *out) {
	if (*field == NULL || (size_t)(end - *field) < sizeof(*out)) {
		return -1;
	}
	memcpy(out, *field, sizeof(*out));
	*field += sizeof(*out);
	return 0;
}

static int parse_size(char **field, const char *end, uint64_t *out) {
	off_t size;

	if (*field == NULL || (size_t)(end - *field) < sizeof(size)) {
		return -1;
	}
	memcpy(&size, *field, sizeof(size));
	*field += sizeof(size);
	if (size < 0) {
		return -1;
	}
	*out = (uint64_t)size;
	return 0;
}

// sum_file_sizes_range scans one directory, selecting regular files whose
// names compare in the bytewise interval [low, high). An empty bound is open.
// queue_depth sizes the getattrlistbulk attribute buffer (512 bytes/entry).
static int sum_file_sizes_range(const char *path, const char *low,
				const char *high, unsigned queue_depth,
				uint64_t *total, uint64_t *files,
				uint64_t *vanished, char *err, size_t err_len) {
	int dirfd = -1;
	char *buf = NULL;
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

	size_t buf_len = (size_t)queue_depth * 512;
	if (buf_len < 8192) {
		buf_len = 8192;
	}

	dirfd = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
	if (dirfd == -1) {
		set_error(err, err_len, "open", path, errno);
		goto done;
	}

	buf = malloc(buf_len);
	if (buf == NULL) {
		set_error(err, err_len, "malloc", NULL, ENOMEM);
		goto done;
	}

	struct attrlist attr_list;
	memset(&attr_list, 0, sizeof(attr_list));
	attr_list.bitmapcount = ATTR_BIT_MAP_COUNT;
	attr_list.commonattr = ATTR_CMN_RETURNED_ATTRS | ATTR_CMN_NAME |
			       ATTR_CMN_ERROR | ATTR_CMN_OBJTYPE;
	attr_list.fileattr = ATTR_FILE_DATALENGTH;

	for (;;) {
		int retcount;

		do {
			retcount = getattrlistbulk(dirfd, &attr_list, buf,
						   buf_len, 0);
		} while (retcount == -1 && errno == EINTR);

		if (retcount == -1) {
			set_error(err, err_len, "getattrlistbulk", path, errno);
			goto done;
		}
		if (retcount == 0) {
			break;
		}

		char *entry = buf;
		const char *buf_end = buf + buf_len;

		for (int i = 0; i < retcount; i++) {
			uint32_t length;

			if (parse_u32(&entry, buf_end, &length) != 0) {
				snprintf(err, err_len, "getattrlistbulk %s: truncated entry length",
					 path);
				goto done;
			}
			if (length < sizeof(length) ||
			    entry + (length - sizeof(length)) > buf_end) {
				snprintf(err, err_len, "getattrlistbulk %s: invalid entry length",
					 path);
				goto done;
			}

			char *field = entry;
			const char *entry_end = (entry - sizeof(length)) + length;
			entry = (char *)entry_end;

			attribute_set_t returned;
			if ((size_t)(entry_end - field) < sizeof(returned)) {
				snprintf(err, err_len, "getattrlistbulk %s: truncated returned attributes",
					 path);
				goto done;
			}
			memcpy(&returned, field, sizeof(returned));
			field += sizeof(returned);

			uint32_t entry_error = 0;
			if ((returned.commonattr & ATTR_CMN_ERROR) != 0) {
				if (parse_u32(&field, entry_end, &entry_error) != 0) {
					snprintf(err, err_len, "getattrlistbulk %s: truncated entry error",
						 path);
					goto done;
				}
			}

			const char *name = NULL;
			if ((returned.commonattr & ATTR_CMN_NAME) != 0) {
				attrreference_t name_info;
				if ((size_t)(entry_end - field) < sizeof(name_info)) {
					snprintf(err, err_len, "getattrlistbulk %s: truncated name",
						 path);
					goto done;
				}
				memcpy(&name_info, field, sizeof(name_info));
				if (name_info.attr_dataoffset < 0 ||
				    name_info.attr_length == 0 ||
				    field + name_info.attr_dataoffset < field ||
				    field + name_info.attr_dataoffset +
					    name_info.attr_length > entry_end) {
					snprintf(err, err_len, "getattrlistbulk %s: invalid name",
						 path);
					goto done;
				}
				name = field + name_info.attr_dataoffset;
				field += sizeof(name_info);
			}

			if (entry_error != 0) {
				if (entry_error == ENOENT) {
					(*vanished)++;
					continue;
				}
				set_error(err, err_len, "getattrlistbulk", name,
					  (int)entry_error);
				goto done;
			}

			if (name == NULL) {
				snprintf(err, err_len, "getattrlistbulk %s: filesystem did not return name",
					 path);
				goto done;
			}

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

			if ((returned.commonattr & ATTR_CMN_OBJTYPE) == 0) {
				snprintf(err, err_len, "getattrlistbulk %s: filesystem did not return type",
					 name);
				goto done;
			}

			fsobj_type_t obj_type;
			if (parse_obj_type(&field, entry_end, &obj_type) != 0) {
				snprintf(err, err_len, "getattrlistbulk %s: truncated type",
					 name);
				goto done;
			}
			if (obj_type != VREG) {
				continue;
			}

			if ((returned.fileattr & ATTR_FILE_DATALENGTH) == 0) {
				snprintf(err, err_len, "getattrlistbulk %s: filesystem did not return size",
					 name);
				goto done;
			}

			uint64_t size;
			if (parse_size(&field, entry_end, &size) != 0) {
				snprintf(err, err_len, "getattrlistbulk %s: truncated size",
					 name);
				goto done;
			}

			if (UINT64_MAX - *total < size) {
				snprintf(err, err_len, "sum overflow at %s", name);
				goto done;
			}

			*total += size;
			(*files)++;
		}
	}

	status = 0;

done:
	saved_errno = errno;
	free(buf);
	if (dirfd != -1 && close(dirfd) == -1 && status == 0) {
		set_error(err, err_len, "close", path, errno);
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
// QueueDepth sizes the getattrlistbulk attribute buffer. Zero selects 128.
// This function makes one long cgo call; it does not allocate Go objects
// per directory entry and cannot be canceled midway.
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
