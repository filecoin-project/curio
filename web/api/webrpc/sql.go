package webrpc

import (
	"context"
	"net/url"
	"path"
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const sqlConsolePathPrefix = "/pages/sql"

// SQLQuery runs arbitrary SQL against the HarmonyDB for the admin console.
// Only requests whose Referer is the SQL Console page are allowed.
func (a *WebRPC) SQLQuery(ctx context.Context, query string) (*harmonydb.AdminQueryResult, error) {
	if err := requireSQLConsoleReferer(ctx); err != nil {
		return nil, err
	}
	if a.Deps.DB == nil {
		return nil, xerrors.Errorf("database not configured")
	}
	return harmonydb.AdminQuery(ctx, a.Deps.DB, query)
}

func requireSQLConsoleReferer(ctx context.Context) error {
	req := httpRequestFromContext(ctx)
	if req == nil {
		return xerrors.Errorf("SQLQuery is only available from the SQL Console page")
	}
	ref := req.Header.Get("Referer")
	if ref == "" {
		return xerrors.Errorf("SQLQuery requires a Referer from the SQL Console page")
	}
	if !isSQLConsoleReferer(ref, req.Host) {
		return xerrors.Errorf("SQLQuery is only available from the SQL Console page")
	}
	return nil
}

// isSQLConsoleReferer reports whether referer is a same-host URL under /pages/sql.
func isSQLConsoleReferer(referer, requestHost string) bool {
	u, err := url.Parse(referer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if requestHost != "" && !strings.EqualFold(u.Host, requestHost) {
		return false
	}
	p := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	return p == sqlConsolePathPrefix || strings.HasPrefix(p, sqlConsolePathPrefix+"/")
}
