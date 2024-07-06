package hugepageutil

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"golang.org/x/xerrors"
)

func CheckHugePages(minPages int) error {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return xerrors.Errorf("error opening /proc/meminfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	hugepagesTotal := 0
	using1GHugepages := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "HugePages_Total:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				hugepagesTotal, err = strconv.Atoi(fields[1])
				if err != nil {
					return xerrors.Errorf("error parsing HugePages_Total: %w", err)
				}
			}
		} else if strings.Contains(line, "Hugepagesize:") && strings.Contains(line, "1048576 kB") {
			using1GHugepages = true
		}
	}

	if err := scanner.Err(); err != nil {
		return xerrors.Errorf("error reading /proc/meminfo: %w", err)
	}

	if hugepagesTotal < minPages {
		return xerrors.Errorf("insufficient hugepages: got %d, want at least %d", hugepagesTotal, minPages)
	}

	if !using1GHugepages {
		return xerrors.New("1G hugepages are not being used")
	}

	return nil
}
