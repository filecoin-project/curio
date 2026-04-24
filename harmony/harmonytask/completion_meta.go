package harmonytask

import (
	"context"
	"sync"
)

type completionMetaKey struct{}

type completionMeta struct {
	mu   sync.Mutex
	vals map[any]any
}

// TaskCompleteFunc runs in the task goroutine after recordCompletion commits.
// ctx carries KV pairs the task stashed via SetMeta during Do.
// success is true when Do returned done=true.
type TaskCompleteFunc func(ctx context.Context, taskID TaskID, success bool)

// SetMeta records a key-value pair for OnTaskComplete callbacks after Do returns.
// key must be comparable (e.g. a package-level typed sentinel in the task package).
// No-op if ctx was not created by the task engine or value is nil.
func SetMeta(ctx context.Context, key, value any) {
	if value == nil || key == nil {
		return
	}
	m, ok := ctx.Value(completionMetaKey{}).(*completionMeta)
	if !ok || m == nil {
		return
	}
	m.mu.Lock()
	m.vals[key] = value
	m.mu.Unlock()
}

// MetaValue reads a value from the context built for task completion callbacks.
func MetaValue[T any](ctx context.Context, key any) (T, bool) {
	v, ok := ctx.Value(key).(T)
	return v, ok
}

func completionMetaFrom(ctx context.Context) *completionMeta {
	m, _ := ctx.Value(completionMetaKey{}).(*completionMeta)
	return m
}

func buildCompletionContext(meta *completionMeta) context.Context {
	if meta == nil {
		return context.Background()
	}
	meta.mu.Lock()
	pairs := make([]struct {
		k any
		v any
	}, 0, len(meta.vals))
	for k, v := range meta.vals {
		if v != nil {
			pairs = append(pairs, struct {
				k any
				v any
			}{k, v})
		}
	}
	meta.mu.Unlock()
	cbCtx := context.Background()
	for _, p := range pairs {
		cbCtx = context.WithValue(cbCtx, p.k, p.v)
	}
	return cbCtx
}

func (e *TaskEngine) invokeTaskCompleteCallbacks(taskType string, meta *completionMeta, tid TaskID) {
	if meta == nil {
		return
	}
	e.completionMu.RLock()
	fns := e.completionCallbacks[taskType]
	e.completionMu.RUnlock()
	if len(fns) == 0 {
		return
	}
	cbCtx := buildCompletionContext(meta)
	for _, fn := range fns {
		fn(cbCtx, tid, true)
	}
}

// OnTaskComplete registers a callback invoked after a successful task completion
// (Do returned done=true) and after the harmony_task row is finalized in the DB.
// Multiple callbacks per task type are allowed; they run in registration order.
// Callbacks must not block for long; they run on the task goroutine.
func (e *TaskEngine) OnTaskComplete(taskType string, fn TaskCompleteFunc) {
	e.completionMu.Lock()
	defer e.completionMu.Unlock()
	e.completionCallbacks[taskType] = append(e.completionCallbacks[taskType], fn)
}
