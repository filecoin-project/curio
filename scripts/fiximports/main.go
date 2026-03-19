package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/imports"
	"golang.org/x/xerrors"
)

var (
	// groupByPrefixes is the list of import prefixes that should _each_ be grouped separately.
	// See: imports.LocalPrefix.
	groupByPrefixes = []string{
		"github.com/filecoin-project",
		"github.com/filecoin-project/lotus",
		"github.com/filecoin-project/curio",
	}
	newline                  = []byte("\n")
	importBlockRegex         = regexp.MustCompile(`(?s)import\s*\((.*?)\)`)
	consecutiveNewlinesRegex = regexp.MustCompile(`\n\s*\n`)
)

func main() {
	numWorkers := runtime.NumCPU()

	// Get files changed since merge-base with main
	// Files already on main have had fiximports run, so we only need to process changes
	changedFiles := getChangedFilesSinceMergeBase()

	if len(changedFiles) == 0 {
		fmt.Println("No Go files changed since merge-base with main")
		return
	}

	fmt.Printf("Processing %d changed Go file(s)\n", len(changedFiles))

	imports.LocalPrefix = strings.Join(groupByPrefixes, ",")
	if err := processFiles(changedFiles, numWorkers); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error processing files: %v\n", err)
		os.Exit(1)
	}
}

// getChangedFilesSinceMergeBase returns Go files that have changed since the merge-base with main.
// This includes committed changes on the branch, uncommitted changes, and untracked files.
func getChangedFilesSinceMergeBase() []string {
	// Find the merge-base between main and HEAD
	mergeBase, err := exec.Command("git", "merge-base", "main", "HEAD").Output()
	if err != nil {
		// If we can't find merge-base (e.g., not on a branch), fall back to all files
		fmt.Println("Could not determine merge-base, processing all files")
		return getAllGoFiles()
	}
	base := strings.TrimSpace(string(mergeBase))

	// Get files changed between merge-base and HEAD (committed changes)
	committed, _ := exec.Command("git", "diff", "--name-only", base, "HEAD").Output()

	// Get uncommitted changes (staged + unstaged)
	staged, _ := exec.Command("git", "diff", "--name-only", "--cached").Output()
	unstaged, _ := exec.Command("git", "diff", "--name-only").Output()

	// Get untracked files (new files not yet added to git)
	untracked, _ := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output()

	// Combine all changes
	allChanges := string(committed) + string(staged) + string(unstaged) + string(untracked)

	// Filter to Go files, excluding extern/ and generated files
	seen := make(map[string]bool)
	var goFiles []string
	for _, line := range strings.Split(allChanges, "\n") {
		path := strings.TrimSpace(line)
		if path == "" || seen[path] {
			continue
		}
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		if strings.HasPrefix(path, "extern/") {
			continue
		}
		if strings.HasSuffix(path, "_cbor_gen.go") {
			continue
		}
		// Verify file exists (might have been deleted)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = true
		goFiles = append(goFiles, path)
	}
	return goFiles
}

// getAllGoFiles returns all Go files in the repo (fallback when merge-base fails)
func getAllGoFiles() []string {
	var files []string
	err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(path, "extern/") {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_cbor_gen.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to walk directory: %v", err)
	}
	return files
}

// processFiles runs a goroutine per file: read → collapse → imports.Process → write (if changed).
func processFiles(files []string, numWorkers int) error {
	var g errgroup.Group
	g.SetLimit(numWorkers)

	for _, path := range files {
		path := path
		g.Go(func() error {
			content, err := os.ReadFile(path)
			if err != nil {
				return xerrors.Errorf("reading %s: %w", path, err)
			}
			collapsed := importBlockRegex.ReplaceAllFunc(content, func(importBlock []byte) []byte {
				// Replace consecutive newlines with a single newline within the import block
				return consecutiveNewlinesRegex.ReplaceAll(importBlock, newline)
			})
			formatted, err := imports.Process(path, collapsed, nil)
			if err != nil {
				return xerrors.Errorf("processing %s: %w", path, err)
			}
			if bytes.Equal(content, formatted) {
				return nil
			}
			if err := os.WriteFile(path, formatted, 0666); err != nil {
				return xerrors.Errorf("writing %s: %w", path, err)
			}
			return nil
		})
	}

	return g.Wait()
}
