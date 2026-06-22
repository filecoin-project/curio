//go:build !maxboom

package pdpnode

import "github.com/filecoin-project/lotus/lib/ulimit"

func manageFdLimit() {
	if _, _, err := ulimit.ManageFdLimit(); err != nil {
		log.Errorf("setting file descriptor limit: %s", err)
	}
}
