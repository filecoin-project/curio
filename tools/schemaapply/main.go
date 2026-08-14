// Command schemaapply verifies the consolidated PDP closure schema applies via
// harmonyquery's own migrator (pgx Exec path) — i.e. exactly how Piri will apply
// it when embedding the curated SqlEmbedFS. Throwaway verification tool.
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

//go:embed sql
var curatedFS embed.FS

func main() {
	db, err := harmonydb.NewFromConfig(harmonydb.Config{
		Hosts:      []string{"127.0.0.1"},
		Username:   "postgres",
		Password:   "postgres",
		Database:   "piri_curated",
		Port:       "5432",
		SSLMode:    "disable",
		SqlEmbedFS: &curatedFS,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(1)
	}
	_ = db
	fmt.Println("OK: curated schema applied via harmonyquery migrator")
}
