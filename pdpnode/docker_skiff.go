//go:build skiff

package pdpnode

import (
	"fmt"
	"os"

	"github.com/filecoin-project/curio/deps/config"
)

func skiffDockerLog(format string, args ...interface{}) {
	if skiffDockerMode() {
		_, _ = fmt.Fprintf(os.Stderr, "[skiff] "+format+"\n", args...)
	}
}

func applySkiffDockerListen(cfg *config.CurioConfig) {
	if !skiffDockerMode() {
		return
	}
	if cfg.Subsystems.GuiAddress == "" || cfg.Subsystems.GuiAddress == "127.0.0.1:4701" {
		cfg.Subsystems.GuiAddress = "0.0.0.0:4701"
	}
}
