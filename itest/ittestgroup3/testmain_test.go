package ittestgroup3

import (
	"os"
	"testing"

	"github.com/filecoin-project/curio/lib/testutil/dbtest"
)

func TestMain(m *testing.M) {
	os.Exit(dbtest.WithDatabases(m))
}
