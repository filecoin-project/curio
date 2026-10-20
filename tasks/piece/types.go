package piece

import (
	"time"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("cu-piece")
var PieceParkPollInterval = time.Second * 5
