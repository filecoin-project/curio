//go:build maxboom

package webrpc

import "context"

func (a *Handler) UIVariant(context.Context) (string, error) {
	return "maxboom", nil
}
