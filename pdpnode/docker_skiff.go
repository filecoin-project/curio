//go:build maxboom

package pdpnode

import (
	"fmt"
	"os"

	"github.com/filecoin-project/curio/deps/config"
)

func maxboomDockerLog(format string, args ...interface{}) {
	if maxboomDockerMode() {
		_, _ = fmt.Fprintf(os.Stderr, "[maxboom] "+format+"\n", args...)
	}
}

func applyMaxBoomDockerListen(cfg *config.CurioConfig) {
	if !maxboomDockerMode() {
		return
	}
	if cfg.Subsystems.GuiAddress == "" || cfg.Subsystems.GuiAddress == "127.0.0.1:4701" {
		cfg.Subsystems.GuiAddress = "0.0.0.0:4701"
	}
}
