package harmonydb

import "sync"

var itestDatabaseRegistry sync.Map

// RegisterITestDatabase allows test helpers to declare that a given ITestID
// should connect to the provided physical database instead of creating a fresh
// schema inside the default database. This is used by the integration-test
// template cloning logic.
func RegisterITestDatabase(id ITestID, database string) {
	if id == "" || database == "" {
		return
	}
	itestDatabaseRegistry.Store(string(id), database)
}

func unregisterITestDatabase(id ITestID) {
	if id == "" {
		return
	}
	itestDatabaseRegistry.Delete(string(id))
}

func lookupITestDatabase(id ITestID) (string, bool) {
	if id == "" {
		return "", false
	}
	if v, ok := itestDatabaseRegistry.Load(string(id)); ok {
		if s, ok2 := v.(string); ok2 {
			return s, true
		}
	}
	return "", false
}
