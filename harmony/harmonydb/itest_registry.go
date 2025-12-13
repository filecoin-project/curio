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
