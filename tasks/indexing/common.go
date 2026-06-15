package indexing

import (
	"database/sql"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("indexing")
var ilog = logging.Logger("ipni")

const ipniHeadCASRetries = 16

func nullableText(v string) any {
	if v == "" {
		return nil
	}
	return sql.NullString{String: v, Valid: true}
}
