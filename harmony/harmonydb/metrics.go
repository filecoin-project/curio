package harmonydb

import (
	"go.opencensus.io/tag"
)

var (
	dbTag, _         = tag.NewKey("db_name")
	pre              = "curio_db_"
	waitsBuckets     = []float64{0, 10, 20, 30, 50, 80, 130, 210, 340, 550, 890}
	whichHostBuckets = []float64{0, 1, 2, 3, 4, 5}
)
