package webrpc

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// SQLQuery runs arbitrary SQL against the HarmonyDB for the admin console.
func (a *WebRPC) SQLQuery(ctx context.Context, query string) (*harmonydb.AdminQueryResult, error) {
	if a.Deps.DB == nil {
		return nil, xerrors.Errorf("database not configured")
	}
	return harmonydb.AdminQuery(ctx, a.Deps.DB, query)
}
