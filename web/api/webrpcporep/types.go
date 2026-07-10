//go:build !skiff

package webrpcporep

import "github.com/filecoin-project/curio/web/api/webrpctypes"

type (
	NullString = webrpctypes.NullString
	NullInt64  = webrpctypes.NullInt64
	NullBool   = webrpctypes.NullBool
	NullTime   = webrpctypes.NullTime
)
