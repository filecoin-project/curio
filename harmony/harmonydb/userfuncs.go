package harmonydb

import (
	"time"

	"github.com/curiostorage/harmonydb"
)

const InitialSerializationErrorRetryWait = 5 * time.Second

// Query offers Next/Err/Close/Scan/Values
type Query = harmonydb.Query

type Tx = harmonydb.Tx
type TransactionOptions = harmonydb.TransactionOptions

var IsErrUniqueContraint = harmonydb.IsErrUniqueContraint
var IsErrSerialization = harmonydb.IsErrSerialization

var OptionRetry = harmonydb.OptionRetry
