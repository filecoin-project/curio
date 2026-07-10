// Package servicedeps holds shared HTTP service dependency handles without
// pulling in the full cuhttp server / deal-market dependency graph.
package servicedeps

import (
	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/tasks/message"
)

// Deps is passed to PDP and curio HTTP route mounting code.
type Deps struct {
	EthSender *message.SenderETH
	AlertTask *alertmanager.AlertTask
}
