//go:build skiff && linux

package pdpnode

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var skipFSTypes = map[string]struct{}{
	"autofs":     {},
	"binfmt_misc": {},
	"bpf":        {},
	"cgroup":     {},
	"cgroup2":    {},
	"configfs":   {},
	"debugfs":    {},
	"devpts":     {},
	"devtmpfs":   {},
	"efivarfs":   {},
	"fusectl":    {},
	"hugetlbfs":  {},
	"mqueue":     {},
	"proc":       {},
	"pstore":     {},
	"securityfs": {},
	"sysfs":      {},
	"tmpfs":      {},
	"tracefs":    {},
}

func listMountPoints() ([]string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	seen := map[string]struct{}{}
	var mounts []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		parts := bytes.SplitN(line, []byte(" - "), 2)
		if len(parts) != 2 {
			continue
		}

		left := bytes.Fields(parts[0])
		if len(left) < 5 {
			continue
		}

		right := bytes.Fields(parts[1])
		if len(right) < 1 {
			continue
		}

		fstype := string(right[0])
		if _, skip := skipFSTypes[fstype]; skip {
			continue
		}

		mountPoint := unescapeMountinfoPath(string(left[4]))
		if mountPoint == "" {
			continue
		}

		if _, ok := seen[mountPoint]; ok {
			continue
		}
		seen[mountPoint] = struct{}{}
		mounts = append(mounts, mountPoint)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Strings(mounts)
	return mounts, nil
}

func unescapeMountinfoPath(in string) string {
	if !strings.Contains(in, `\`) {
		return filepath.Clean(in)
	}

	var b strings.Builder
	for i := 0; i < len(in); i++ {
		if in[i] == '\\' && i+3 < len(in) {
			if oct, ok := parseOctal(in[i+1 : i+4]); ok {
				b.WriteByte(oct)
				i += 3
				continue
			}
		}
		b.WriteByte(in[i])
	}
	return filepath.Clean(b.String())
}

func parseOctal(s string) (byte, bool) {
	if len(s) != 3 {
		return 0, false
	}
	var n byte
	for i := 0; i < 3; i++ {
		if s[i] < '0' || s[i] > '7' {
			return 0, false
		}
		n = n*8 + (s[i] - '0')
	}
	return n, true
}
