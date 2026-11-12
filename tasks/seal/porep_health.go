package seal

import (
	"errors"
	"sync"
	"time"

	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
)

// A local C2 failure holds admission for 2..30 minutes, then permits one probe.
type porepHealth struct {
	mu                          sync.Mutex
	now                         func() time.Time
	until                       time.Time
	delay                       time.Duration
	probe, sequence, generation uint64
}

func (h *porepHealth) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h *porepHealth) blocked() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.probe != 0 || h.clock().Before(h.until)
}

func (h *porepHealth) reserve() (uint64, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.probe != 0 || h.clock().Before(h.until) {
		return 0, nil, false
	}
	if h.delay == 0 {
		return h.generation, func() {}, true
	}
	h.sequence++
	token := h.sequence
	h.probe = token
	return h.generation, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.probe == token {
			h.probe = 0
		}
	}, true
}

func (h *porepHealth) result(epoch uint64, err error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var unavailable *ffi.LocalPoRepBackendUnavailable
	if errors.As(err, &unavailable) {
		h.generation++
		h.delay = min(max(2*time.Minute, h.delay*2), 30*time.Minute)
		h.until = h.clock().Add(h.delay)
		return &taskhelp.WorkerUnavailable{Cause: err}
	}
	if epoch == h.generation {
		if err == nil {
			h.delay = 0
			h.until = time.Time{}
		} else if h.delay > 0 {
			h.until = h.clock().Add(h.delay)
		}
	}
	return err
}
