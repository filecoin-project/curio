package harmonytask

// Captured from the successful claim/recovery, never reconstructed from the
// current database row after Do returns. Generation zero is a valid acquisition.
type completionIdentity struct {
	owner      int
	generation int64
	token      string
	valid      bool
}

func completionIdentityFor(store taskAttemptStore, id TaskID, token string) completionIdentity {
	s, ok := store.(harmonyTaskAttemptStore)
	if !ok {
		return completionIdentity{}
	}
	generation, found := s.generations[id]
	return completionIdentity{owner: s.owner, generation: generation, token: token,
		valid: found && s.owner > 0 && generation >= 0 && token != ""}
}

type taskCompletion struct {
	applied bool
	retry   *task
}

// Stale results must not notify either in-process followers or peer scheduling.
// They also have no successful history row for SQL-driven followers to observe.
func (h *taskTypeHandler) publishCompletion(result taskCompletion, id TaskID, meta *completionMeta, done bool, doErr error, ee eventEmitter) {
	if !result.applied {
		return
	}
	success := done && doErr == nil
	if success {
		h.TaskEngine.invokeTaskCompleteCallbacks(h.Name, meta, id)
	}
	ee.EmitTaskCompleted(h.Name, success)
	h.emitRetryTask(ee, result.retry)
}
