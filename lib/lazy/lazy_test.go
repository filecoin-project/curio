package lazy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLazyValNilReceiver(t *testing.T) {
	var l *Lazy[int]
	v, err := l.Val()
	require.Error(t, err)
	require.Zero(t, v)
}

func TestLazyValNilGet(t *testing.T) {
	l := &Lazy[int]{}
	v, err := l.Val()
	require.Error(t, err)
	require.Zero(t, v)
}

func TestLazyValOK(t *testing.T) {
	l := MakeLazy(func() (int, error) { return 42, nil })
	v, err := l.Val()
	require.NoError(t, err)
	require.Equal(t, 42, v)
}

func TestLazyCtxValNilReceiver(t *testing.T) {
	var l *LazyCtx[int]
	v, err := l.Val(context.Background())
	require.Error(t, err)
	require.Zero(t, v)
}
