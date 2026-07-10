//go:build !skiff

package pdpnode

import "github.com/filecoin-project/curio/deps/config"

func skiffDockerMode() bool { return false }

func skiffDockerLog(_ string, _ ...interface{}) {}

func applySkiffDockerListen(_ *config.CurioConfig) {}
