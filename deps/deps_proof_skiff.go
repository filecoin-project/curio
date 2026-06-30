//go:build skiff

package deps

import (
	"context"
)

func initProofTypes(deps *Deps) error {
	_ = deps
	return nil
}

func startWalletExporterIfEnabled(ctx context.Context, deps *Deps) {
	_, _ = ctx, deps
}
