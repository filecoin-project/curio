package contextlock

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

type ContextLock struct {
	mu sync.Mutex
	lockContextKey uuid.UUID
}

// NewContextLock creates a new context lock
// ContextLock allows to create a lock which can't be double-locked
// It's useful to avoid race conditions when a function is called in multiple places
// and the function needs to be called only once.
func NewContextLock() *ContextLock {
	return &ContextLock{
		lockContextKey: uuid.New(),
	}
}

func (l *ContextLock) Lock(ctx context.Context) context.Context {
	// in context see if the lock is already held
	// if so, increment refcount and return the context
	// if not, lock and return a new context with refcount 1

	lockContext := ctx.Value(l.lockContextKey)
	if lockContext != nil {
		refCount := lockContext.(int)
		return context.WithValue(ctx, l.lockContextKey, refCount+1)
	}

	l.mu.Lock()
	return context.WithValue(ctx, l.lockContextKey, 1)
}

func (l *ContextLock) Unlock(ctx context.Context) context.Context {
	// in context see if the lock is held
	// if not, return the context
	// if so, decrement refcount and only unlock when refcount reaches 0

	lockContext := ctx.Value(l.lockContextKey)
	if lockContext == nil {
		return ctx
	}

	refCount := lockContext.(int)
	if refCount <= 0 {
		return ctx
	}

	newRefCount := refCount - 1
	if newRefCount == 0 {
		l.mu.Unlock()
		return context.WithValue(ctx, l.lockContextKey, nil)
	}

	return context.WithValue(ctx, l.lockContextKey, newRefCount)
}
