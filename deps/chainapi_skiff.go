//go:build maxboom

package deps

import (
	"strings"

	"github.com/filecoin-project/curio/deps/config"
)

func useEmbeddedLanternBackend(backend string) bool {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case config.ChainBackendExternal, "lotus":
		return false
	default:
		return true
	}
}

func embeddedLanternAPIInfo() (string, bool) {
	if embedInst != nil && embedInst.apiInfo != "" {
		return embedInst.apiInfo, true
	}
	return "", false
}
