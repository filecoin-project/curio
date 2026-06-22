//go:build maxboom

package pdpnode

import (
	"syscall"

	logging "github.com/ipfs/go-log/v2"
)

var ulimitLog = logging.Logger("maxboom/ulimit")

func manageFdLimit() {
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		ulimitLog.Errorf("getting fd limit: %s", err)
		return
	}
	const minFds = 2048
	if rLimit.Cur >= minFds {
		return
	}
	rLimit.Cur = rLimit.Max
	if rLimit.Cur > 1<<20 {
		rLimit.Cur = 1 << 20
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		ulimitLog.Warnf("raising fd limit: %s", err)
		return
	}
	ulimitLog.Infof("raised fd limit to %d", rLimit.Cur)
}
