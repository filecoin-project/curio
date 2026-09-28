package hashspace

import "sync"

// sizeTracker is the in-memory used counter for one space folder.
// Writers add on successful Close. DeleteCID subtracts that one file.
// The counter stays non-negative.
type sizeTracker struct {
	mu   sync.Mutex
	used int64
}

func (t *sizeTracker) Add(n int64) {
	if n <= 0 {
		return
	}
	t.mu.Lock()
	t.used += n
	t.mu.Unlock()
}

func (t *sizeTracker) Sub(n int64) {
	if n <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if n >= t.used {
		t.used = 0
		return
	}
	t.used -= n
}

func (t *sizeTracker) Set(n int64) {
	t.mu.Lock()
	t.used = n
	t.mu.Unlock()
}

func (t *sizeTracker) Used() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.used
}
