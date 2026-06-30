//go:build skiff

package deps

import (
	"github.com/filecoin-project/curio/lib/apiconn"
)

func parseAPIInfo(s string) apiconn.Info {
	return apiconn.Parse(s)
}
