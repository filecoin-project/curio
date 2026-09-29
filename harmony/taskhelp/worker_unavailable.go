package taskhelp

// WorkerUnavailable records a failed attempt without charging a sector's
// failure budget. Callers must first gate further admission on this worker;
// this is not for generic transient errors or invalid sector input.
type WorkerUnavailable struct{ Cause error }

func (e *WorkerUnavailable) Error() string { return "worker unavailable: " + e.Cause.Error() }
func (e *WorkerUnavailable) Unwrap() error { return e.Cause }
