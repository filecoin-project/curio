package main

import (
	"os"
	"strconv"

	"github.com/filecoin-project/curio/lib/supraffi"
)

func main() {
	// call ./gosupra [sector size] [config]
	if len(os.Args) != 3 {
		panic("Usage: ./gosupra [sector size] [config]")
	}

	sectorSize, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil {
		panic(err)
	}

	supraffi.SupraSealInit(sectorSize, os.Args[2])
}
