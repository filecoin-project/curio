package pdp

import (
	"net/http"
	"time"

	logging "github.com/ipfs/go-log/v2"
)

// lifecycleLog emits standardized "[Begin <event>]" / "[End <event>]" lines for
// every state-changing PDP HTTP handler. Filterable independently from the
// general-purpose `pdpv0` subsystem so operators can subscribe to inbound
// request lifecycle events alone (e.g. in Betterstack).
var lifecycleLog = logging.Logger("pdp-lifecycle")

// statusRecorder is a minimal http.ResponseWriter wrapper that captures the
// final status code so the End log line can record it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w}
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// instrument wraps a state-changing PDP HTTP handler with standardized
// "[Begin <event>]" / "[End <event>]" logging at Debug level. The End line
// records the response status code, duration, and outcome (success|failure).
//
// kvFn, if non-nil, is invoked at request time to extract additional context
// (typically chi URL params) included on both the Begin and End lines.
//
// Only state-changing verbs (POST/PUT/DELETE) should be instrumented; GET
// traffic is intentionally excluded to keep the lifecycle stream signal-rich
// for centralized log viewers.
func instrument(event string, h http.HandlerFunc, kvFn func(*http.Request) []any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := newStatusRecorder(w)
		var extra []any
		if kvFn != nil {
			extra = kvFn(r)
		}
		base := make([]any, 0, 6+len(extra))
		base = append(base, "event", event, "method", r.Method, "path", r.URL.Path)
		base = append(base, extra...)
		lifecycleLog.Debugw("[Begin "+event+"]", base...)
		start := time.Now()
		defer func() {
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			outcome := "success"
			if status >= 400 {
				outcome = "failure"
			}
			end := append(base, "status", status, "dur_ms", time.Since(start).Milliseconds(), "outcome", outcome)
			lifecycleLog.Debugw("[End "+event+"]", end...)
		}()
		h(rec, r)
	}
}
