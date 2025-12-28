package harmonydb

import (
	"embed"

	"github.com/curiostorage/harmonydb"
)

type ITestID string

// ITestNewID see ITestWithID doc
var ITestNewID = harmonydb.ITestNewID

type DB = harmonydb.DB

type Config = harmonydb.Config

var NewFromConfig = harmonydb.NewFromConfig
var NewFromConfigWithITestID = harmonydb.NewFromConfigWithITestID
var New = harmonydb.New

func init() {
	harmonydb.Init(upgadeFS, downgradeFS)
	harmonydb.DefaultHostEnv = "CURIO_HARMONYDB_HOSTS"
}

//go:embed sql
var upgadeFS embed.FS

//go:embed downgrade
var downgradeFS embed.FS

var ITestUpgradeFunc = harmonydb.ITestUpgradeFunc
