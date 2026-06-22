//go:build !maxboom

package pdpnode

import "github.com/filecoin-project/curio/deps/config"

func maxboomDockerMode() bool { return false }

func maxboomDockerLog(_ string, _ ...interface{}) {}

func applyMaxBoomDockerListen(_ *config.CurioConfig) {}
