//go:build darwin && cgo

package fs2

/*
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/attr.h>
#include <sys/vnode.h>
#include <unistd.h>

#ifndef PATH_MAX
#define PATH_MAX 4096
#endif

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

static int join_hash(char *out, size_t out_len, const char *prefix,
			 const char *name) {
	size_t prefix_len = strlen(prefix);
	size_t name_len = strlen(name);
	if (prefix_len + name_len + 1 > out_len) {
		return -1;
	}
	memcpy(out, prefix, prefix_len);
	memcpy(out + prefix_len, name, name_len + 1);
	return 0;
}

static int join_path(char *out, size_t out_len, const char *dir,
			 const char *name) {
	size_t dir_len = strlen(dir);
	size_t name_len = strlen(name);
	int need_slash = (dir_len > 0 && dir[dir_len - 1] != '/');
	if (dir_len + (size_t)need_slash + name_len + 1 > out_len) {
		return -1;
	}
	memcpy(out, dir, dir_len);
	size_t offset = dir_len;
	if (need_slash) {
		out[offset++] = '/';
	}
	memcpy(out + offset, name, name_len + 1);
	return 0;
}

static int hash_in_range(const char *hash, const char *low, const char *high) {
	if (low[0] != '\0' && strcmp(hash, low) <= 0) {
		return 0;
	}
	if (high[0] != '\0' && strcmp(hash, high) > 0) {
		return 0;
	}
	return 1;
}

static int subtree_can_match(const char *prefix, const char *low,
				 const char *high) {
	if (prefix[0] == '\0') {
		return 1;
	}
	if (high[0] != '\0' && strcmp(prefix, high) > 0) {
		return 0;
	}
	if (low[0] == '\0' || strcmp(prefix, low) >= 0) {
		return 1;
	}
	return strncmp(low, prefix, strlen(prefix)) == 0;
}

static int sum_dir(const char *path, const char *prefix, const char *low,
		       const char *high, unsigned queue_depth, uint64_t *total,
		       uint64_t *files, uint64_t *vanished, char *err,
		       size_t err_len);

// sum_file_sizes_range walks the directory tree, selecting regular files whose
// concatenated hash paths compare in the bytewise interval (low, high]. An
// empty bound is open. queue_depth sizes the getattrlistbulk attribute buffer
// (512 bytes/entry).
static int sum_file_sizes_range(const char *path, const char *low,
				const char *high, unsigned queue_depth,
				uint64_t *total, uint64_t *files,
				uint64_t *vanished, char *err, size_t err_len) {
	*total = 0;
	*files = 0;
	*vanished = 0;
	if (err != NULL && err_len != 0) {
		err[0] = '\0';
	}
	if (queue_depth == 0) {
		queue_depth = 128;
	}
	return sum_dir(path, "", low, high, queue_depth, total, files, vanished,
		       err, err_len);
}

static int sum_dir(const char *path, const char *prefix, const char *low,
		       const char *high, unsigned queue_depth, uint64_t *total,
		       uint64_t *files, uint64_t *vanished, char *err,
		       size_t err_len) {
	int dirfd = -1;
	char *buf = NULL;
	int status = -1;
	int saved_errno;

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

			char hash[PATH_MAX];
			if (join_hash(hash, sizeof(hash), prefix, name) != 0) {
				snprintf(err, err_len, "hash path exceeds PATH_MAX: %s%s",
					 prefix, name);
				goto done;
			}

			if (obj_type == VDIR) {
				char child_path[PATH_MAX];
				if (join_path(child_path, sizeof(child_path), path, name) != 0) {
					snprintf(err, err_len, "path exceeds PATH_MAX under %s: %s",
						 path, name);
					goto done;
				}
				if (!subtree_can_match(hash, low, high)) {
					continue;
				}
				if (sum_dir(child_path, hash, low, high, queue_depth,
					    total, files, vanished, err, err_len) != 0) {
					goto done;
				}
				continue;
			}
			if (obj_type != VREG) {
				continue;
			}
			if (!hash_in_range(hash, low, high)) {
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
	"unsafe"
)

// SumFileSizesRange sums logical file sizes for regular files under directory
// whose concatenated hash paths compare in the bytewise interval (low, high].
// An empty low or high bound leaves that side of the interval open.
//
// QueueDepth sizes the getattrlistbulk attribute buffer. Zero selects 128.
// This function makes one long cgo call; it does not allocate Go objects
// per directory entry and cannot be canceled midway.
func SumFileSizesRange(directory, low, high string, queueDepth uint32) (Result, error) {
	if err := checkSumArgs(directory, low, high, queueDepth); err != nil {
		return Result{}, err
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
