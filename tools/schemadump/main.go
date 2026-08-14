// Command schemadump applies all embedded harmonydb migrations to a target
// Postgres (faithfully, via harmonyquery's own migrator) so the resulting schema
// can be pg_dump'd and consolidated. Throwaway tool for PDP schema extraction.
package main

import (
	"fmt"
	"os"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func main() {
	db, err := harmonydb.NewFromConfig(harmonydb.Config{
		Hosts:    []string{"127.0.0.1"},
		Username: "postgres",
		Password: "postgres",
		Database: "postgres",
		Port:     "5432",
		SSLMode:  "disable",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERR applying migrations:", err)
		os.Exit(1)
	}
	_ = db
	fmt.Println("OK: all migrations applied to schema 'curio'")
}
