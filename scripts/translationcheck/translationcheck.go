package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type msgEntry struct {
	ID          string `json:"id"`
	Message     string `json:"message"`
	Translation string `json:"translation"`
}

type gotextFile struct {
	Language string     `json:"language"`
	Messages []msgEntry `json:"messages"`
}

func main() {
	const localesDir = "cmd/curio/internal/translations/locales"

	var missing int
	var scanned int

	err := filepath.WalkDir(localesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".gotext.json") {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var f gotextFile
		if err := json.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		// Skip English if you want, or keep it.
		if f.Language == "" {
			fmt.Println("skipping empty language field in", path)
		}

		scanned++

		for _, m := range f.Messages {
			// consider missing if translation is empty or only whitespace
			if strings.TrimSpace(m.Translation) == "" {
				fmt.Printf("%s: missing translation for id=%q message=%q\n", path, m.ID, m.Message)
				missing++
			}
		}
		return nil
	})
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}

	if scanned == 0 {
		fmt.Printf("no .gotext.json files found under %s (is that where gotext writes?)\n", localesDir)
		os.Exit(1)
	}

	if missing > 0 {
		fmt.Printf("\n%d missing translations\n", missing)
		os.Exit(1)
	}

	fmt.Println("all translations present ✓")
}
