package harmonydb

import (
	"time"

	"github.com/curiostorage/harmonydb"
)

const InitialSerializationErrorRetryWait = 5 * time.Second

// rawStringOnly is _intentionally_private_ to force only basic strings in SQL queries.
// In any package, raw strings will satisfy compilation.  Ex:
//
//	harmonydb.Exec("INSERT INTO version (number) VALUES (1)")
//
// This prevents SQL injection attacks where the input contains query fragments.
type Qry = harmonydb.Qry

// Query offers Next/Err/Close/Scan/Values
type Query = harmonydb.Query

type Row = harmonydb.Row

type Tx = harmonydb.Tx
type TransactionOptions = harmonydb.TransactionOptions

var IsErrUniqueContraint = harmonydb.IsErrUniqueContraint
var IsErrSerialization = harmonydb.IsErrSerialization

var IsErrDDLConflict = harmonydb.IsErrDDLConflict

var OptionRetry = harmonydb.OptionRetry
