package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/imports"
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
	// Get files changed since merge-base with main
	// Files already on main have had fiximports run, so we only need to process changes
	changedFiles := getChangedFilesSinceMergeBase()

	if len(changedFiles) == 0 {
		fmt.Println("No Go files changed since merge-base with main")
		return
	}

	fmt.Printf("Processing %d changed Go file(s)\n", len(changedFiles))

	for _, path := range changedFiles {
		if err := fixGoImports(path); err != nil {
			fmt.Printf("Error fixing imports in %s: %v\n", path, err)
			os.Exit(1)
		}
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
	filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
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
	return files
}

func fixGoImports(path string) error {
	sourceFile, err := os.OpenFile(path, os.O_RDWR, 0666)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	source, err := io.ReadAll(sourceFile)
	if err != nil {
		return err
	}
	formatted := collapseImportNewlines(source)
	for _, prefix := range groupByPrefixes {
		imports.LocalPrefix = prefix
		formatted, err = imports.Process(path, formatted, nil)
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(source, formatted) {
		if err := replaceFileContent(sourceFile, formatted); err != nil {
			return err
		}
	}
	return nil
}

func replaceFileContent(target *os.File, replacement []byte) error {
	if _, err := target.Seek(0, io.SeekStart); err != nil {
		return err
	}
	written, err := target.Write(replacement)
	if err != nil {
		return err
	}
	return target.Truncate(int64(written))
}

func collapseImportNewlines(content []byte) []byte {
	return importBlockRegex.ReplaceAllFunc(content, func(importBlock []byte) []byte {
		// Replace consecutive newlines with a single newline within the import block
		return consecutiveNewlinesRegex.ReplaceAll(importBlock, newline)
	})
}
