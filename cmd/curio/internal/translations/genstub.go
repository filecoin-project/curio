//go:build ignore

// Generates a toy main.go with translation strings and properly named variables.
// This allows gotext to extract strings with correct placeholder names without
// loading the heavy dependency tree (CGO, ffi, etc.)
//
// IMPORTANT: If existing translations exist, we preserve their placeholder names
// to avoid breaking translated strings.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type ExtractedString struct {
	Format   string   `json:"format"`
	Args     []string `json:"args"`
	RawExprs []string `json:"rawExprs"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
}

type ExistingMessage struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type ExistingCatalog struct {
	Messages []ExistingMessage `json:"messages"`
}

var placeholderRe = regexp.MustCompile(`\{([^}]+)\}`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run genstub.go <existing-out.gotext.json> < extracted.json")
		os.Exit(1)
	}

	// Load existing translations to preserve placeholder names
	existingNames := loadExistingPlaceholders(os.Args[1])

	var extracted []ExtractedString
	if err := json.NewDecoder(os.Stdin).Decode(&extracted); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding JSON: %v\n", err)
		os.Exit(1)
	}

	generateToyMain(extracted, existingNames)
}

// loadExistingPlaceholders reads the existing out.gotext.json and extracts
// placeholder names for each format string, so we can preserve them
func loadExistingPlaceholders(path string) map[string][]string {
	result := make(map[string][]string)

	f, err := os.Open(path)
	if err != nil {
		// No existing file, use inferred names
		return result
	}
	defer f.Close()

	var catalog ExistingCatalog
	if err := json.NewDecoder(f).Decode(&catalog); err != nil {
		return result
	}

	for _, msg := range catalog.Messages {
		// Extract placeholder names from the message ID
		matches := placeholderRe.FindAllStringSubmatch(msg.ID, -1)
		if len(matches) > 0 {
			var names []string
			for _, m := range matches {
				names = append(names, m[1])
			}
			// Create a normalized key (replace placeholders with %s for matching)
			normalizedKey := placeholderRe.ReplaceAllString(msg.ID, "%s")
			result[normalizedKey] = names
		}
	}

	return result
}

func generateToyMain(extracted []ExtractedString, existingNames map[string][]string) {
	fmt.Println(`// AUTO-GENERATED toy main for gotext extraction. DO NOT EDIT.
// This file provides translation strings with properly named arguments
// so gotext can generate correct placeholder names.
//
// IMPORTANT: We use string literals (not variables) for arguments because
// gotext derives placeholder names from string literal content, preserving
// underscores and proper formatting.

package main

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func main() {
	p := message.NewPrinter(language.English)
	_ = p
`)

	// Generate Sprintf calls with string literal arguments
	// We use the raw expressions from the original source so gotext
	// will derive the same placeholder names as it would from the original code.
	// For strings that already exist in the catalog, we use the existing
	// placeholder names from out.gotext.json to preserve compatibility.
	fmt.Println("\t// Translation strings")
	for _, s := range extracted {
		// Escape the format string for Go
		escaped := escapeString(s.Format)

		if len(s.RawExprs) == 0 {
			fmt.Printf("\tp.Sprintf(%q)\n", escaped)
		} else {
			// Check if we have existing placeholder names for this format string
			// If so, use them to maintain compatibility with existing translations
			if existingArgs, ok := existingNames[s.Format]; ok && len(existingArgs) == len(s.RawExprs) {
				quotedArgs := make([]string, len(existingArgs))
				for i, name := range existingArgs {
					quotedArgs[i] = fmt.Sprintf("%q", name)
				}
				fmt.Printf("\tp.Sprintf(%q, %s)\n", escaped, strings.Join(quotedArgs, ", "))
			} else {
				// Use raw expressions as string literals
				quotedArgs := make([]string, len(s.RawExprs))
				for i, raw := range s.RawExprs {
					quotedArgs[i] = fmt.Sprintf("%q", raw)
				}
				fmt.Printf("\tp.Sprintf(%q, %s)\n", escaped, strings.Join(quotedArgs, ", "))
			}
		}
	}

	fmt.Println("}")
}

func escapeString(s string) string {
	// Handle escape sequences that might be in the original strings
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\t", "\t")
	return s
}

// sanitizeVarName converts a placeholder name to a valid Go identifier
// while preserving underscores to maintain compatibility with existing translations
func sanitizeVarName(name string) string {
	// Replace invalid characters (but keep underscores)
	var result strings.Builder
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			result.WriteRune(r)
		} else if r >= '0' && r <= '9' {
			if i == 0 {
				// Can't start with a number
				result.WriteRune('_')
			}
			result.WriteRune(r)
		}
		// Skip other characters entirely to match gotext behavior
	}

	s := result.String()
	if s == "" {
		return "Arg"
	}
	return s
}
