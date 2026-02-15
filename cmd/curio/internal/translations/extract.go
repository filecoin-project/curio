//go:build ignore

// Fast translation string extractor that captures argument names for proper placeholders.
// This avoids the slow CGO compilation from running gotext on packages with ffi dependencies.

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type ExtractedString struct {
	Format   string   `json:"format"`
	Args     []string `json:"args"`     // Inferred names for fallback
	RawExprs []string `json:"rawExprs"` // Original expressions for gotext compatibility
	File     string   `json:"file"`
	Line     int      `json:"line"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run extract.go <dir1> [dir2] ...")
		os.Exit(1)
	}

	var results []ExtractedString
	seen := make(map[string]bool)

	for _, dir := range os.Args[1:] {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			// Skip translations directory itself
			if strings.Contains(path, "/translations/") {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				extractFromFile(path, &results, seen)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error walking %s: %v\n", dir, err)
		}
	}

	// Sort by format string for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].Format < results[j].Format
	})

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(results)
}

func extractFromFile(path string, results *[]ExtractedString, seen map[string]bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
		return
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Look for T(...), d.T(...), or say(...), d.say(...) calls
		funcName := getFuncName(call)
		if funcName != "T" && funcName != "say" {
			return true
		}

		if len(call.Args) == 0 {
			return true
		}

		// For say(), first arg is style, second is the format string
		// For T(), first arg is the format string
		argOffset := 0
		if funcName == "say" {
			argOffset = 1
			if len(call.Args) < 2 {
				return true
			}
		}

		// Get the format string
		formatStr := getStringLiteral(call.Args[argOffset])
		if formatStr == "" {
			return true
		}

		// Skip if already seen
		if seen[formatStr] {
			return true
		}
		seen[formatStr] = true

		// Extract argument info from remaining args
		var argNames []string
		var rawExprs []string
		for i := argOffset + 1; i < len(call.Args); i++ {
			argNames = append(argNames, inferArgName(call.Args[i], i-argOffset))
			rawExprs = append(rawExprs, getRawExpr(call.Args[i]))
		}

		pos := fset.Position(call.Pos())
		*results = append(*results, ExtractedString{
			Format:   formatStr,
			Args:     argNames,
			RawExprs: rawExprs,
			File:     pos.Filename,
			Line:     pos.Line,
		})

		return true
	})
}

func getFuncName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func getStringLiteral(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			// Remove quotes
			s := e.Value
			if len(s) >= 2 {
				if s[0] == '"' {
					s = s[1 : len(s)-1]
				} else if s[0] == '`' {
					s = s[1 : len(s)-1]
				}
			}
			return s
		}
	}
	return ""
}

// inferArgName extracts a meaningful name from an expression for placeholder generation
func inferArgName(expr ast.Expr, index int) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		// String literal: "Slack" → Slack, "#fil-curio-help" → Fil_curio_help
		if e.Kind == token.STRING {
			s := e.Value
			if len(s) >= 2 {
				s = s[1 : len(s)-1] // Remove quotes
			}
			// Clean up the string to make a valid identifier
			return cleanStringForPlaceholder(s)
		}

	case *ast.Ident:
		// Simple variable: miner → Miner
		return capitalize(e.Name)

	case *ast.SelectorExpr:
		// Field access: harmonyCfg.Database → Database
		return capitalize(e.Sel.Name)

	case *ast.CallExpr:
		// Method call: miner.String() → look at receiver
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			// Check if it's a .String() call, use the receiver name
			if sel.Sel.Name == "String" {
				return inferArgName(sel.X, index)
			}
			// For err.Error() calls, use "Error" to match gotext behavior
			if sel.Sel.Name == "Error" {
				return "Error"
			}
			// For other methods, try to use a sensible name
			if ident, ok := sel.X.(*ast.Ident); ok {
				return capitalize(ident.Name)
			}
		}
		// strings.Join(hosts, ",") - try to get first arg name
		if len(e.Args) > 0 {
			return inferArgName(e.Args[0], index)
		}

	case *ast.IndexExpr:
		// Array index: items[0] → Items
		return inferArgName(e.X, index)

	case *ast.SliceExpr:
		return inferArgName(e.X, index)
	}

	// Fallback: use positional name
	return fmt.Sprintf("Arg_%d", index)
}

// cleanStringForPlaceholder converts a string literal to a placeholder name
// e.g., "#fil-curio-help" → "Fil_curio_help", "Slack" → "Slack"
func cleanStringForPlaceholder(s string) string {
	var result strings.Builder
	isFirstLetter := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			if isFirstLetter {
				// Capitalize the very first letter
				result.WriteRune(unicode.ToUpper(r))
				isFirstLetter = false
			} else {
				result.WriteRune(unicode.ToLower(r))
			}
		} else if r >= '0' && r <= '9' {
			result.WriteRune(r)
		} else if r == '-' || r == '_' {
			result.WriteRune('_')
		}
		// Skip other characters entirely
	}
	if result.Len() == 0 {
		return "Arg"
	}
	return result.String()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// getRawExpr returns the raw expression string for an argument
// For string literals, returns the literal value (without quotes)
// For other expressions, returns a descriptive string
func getRawExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			// Return the string literal value (without quotes)
			s := e.Value
			if len(s) >= 2 {
				s = s[1 : len(s)-1] // Remove quotes
			}
			return s
		}
		return e.Value

	case *ast.Ident:
		return e.Name

	case *ast.SelectorExpr:
		// For field access like harmonyCfg.Database, return "Database"
		return e.Sel.Name

	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			// For method calls like err.Error(), miner.String()
			if ident, ok := sel.X.(*ast.Ident); ok {
				return ident.Name + "." + sel.Sel.Name + "()"
			}
		}
		return "expr"

	default:
		return "expr"
	}
}
