//go:build !skiff

package webrpc

import "context"

func (a *Handler) UIVariant(context.Context) (string, error) {
	return "curio", nil
}
