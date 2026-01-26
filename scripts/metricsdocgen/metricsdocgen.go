// Package main generates Prometheus metrics documentation from source code.
//
// Usage: go run ./scripts/metricsdocgen > documentation/en/configuration/metrics-reference.md
//
// This script automatically discovers all Go files that import prometheus
// and extracts metric definitions from them.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// ============================================================================
// CATEGORY CONFIGURATION - Edit this map to add new metric categories
// ============================================================================
// Key format: "NN_Category Name" where NN is sort order (01-99)
// Value: list of path prefixes that belong to this category
var categoryConfig = map[string][]string{
	"01_Database Metrics (HarmonyDB)":       {"harmony/harmonydb"},
	"02_Task Metrics (HarmonyTask)":         {"harmony/harmonytask"},
	"03_Wallet Exporter Metrics (Optional)": {"deps/stats"},
	"04_Proof Share Metrics":                {"tasks/proofshare"},
	"05_Proof Service Metrics":              {"lib/proofsvc"},
	"06_Sealing Metrics":                    {"tasks/seal", "tasks/sealsupra"},
	"07_Storage Metrics":                    {"lib/paths", "lib/slotmgr"},
	"08_Cache Metrics":                      {"lib/cachedreader"},
	"09_Network Metrics":                    {"lib/robusthttp", "lib/dealdata"},
	"10_FFI Metrics":                        {"lib/ffi"},
	"11_HTTP/Retrieval Metrics":             {"market/"},
}

// ============================================================================

type Metric struct {
	Name        string
	Type        string
	Help        string
	Labels      []string
	Buckets     string
	Source      string
	CategoryKey string
}

type Category struct {
	Name    string
	Key     string
	Metrics []Metric
}

type fileResult struct {
	file    string
	metrics []Metric
}

func main() {
	// Discover and parse metrics in a single pass
	results := discoverAndParseMetrics(".")

	// Group by category
	categories := make(map[string]*Category)
	categoryOrder := []string{}

	for _, res := range results {
		if len(res.metrics) == 0 {
			continue
		}

		catKey, catName := categoryFromPath(res.file)
		cat, exists := categories[catKey]
		if !exists {
			cat = &Category{Name: catName, Key: catKey}
			categories[catKey] = cat
			categoryOrder = append(categoryOrder, catKey)
		}
		cat.Metrics = append(cat.Metrics, res.metrics...)
	}

	// Sort categories for consistent output
	sort.Strings(categoryOrder)

	// Output
	printHeader()
	for _, key := range categoryOrder {
		if cat, ok := categories[key]; ok && len(cat.Metrics) > 0 {
			printCategory(cat)
		}
	}
	printFooter()
}

// Directories to skip entirely
var skipDirs = map[string]bool{
	"vendor":        true,
	".git":          true,
	"node_modules":  true,
	"extern":        true,
	"testdata":      true,
	"documentation": true,
	"docker":        true,
	"scripts":       true,
}

// discoverAndParseMetrics walks the tree and processes files in one pass.
// Each file is opened once: check imports, parse if needed, close.
func discoverAndParseMetrics(root string) []fileResult {
	// Collect candidate paths (just strings, no file handles yet)
	var candidates []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") ||
			strings.HasSuffix(name, "_gen.go") ||
			strings.HasSuffix(name, ".pb.go") ||
			name == "doc.go" {
			return nil
		}
		candidates = append(candidates, path)
		return nil
	})

	// Process in parallel: open, check, parse, close - all in one worker call
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}

	jobs := make(chan string, len(candidates))
	var results []fileResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if metrics := processFile(path); len(metrics) > 0 {
					mu.Lock()
					results = append(results, fileResult{file: path, metrics: metrics})
					mu.Unlock()
				}
			}
		}()
	}

	for _, c := range candidates {
		jobs <- c
	}
	close(jobs)
	wg.Wait()

	// Sort for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].file < results[j].file
	})

	return results
}

// processFile opens a file once, checks for prometheus imports, parses metrics if found.
func processFile(path string) []Metric {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Read first 4KB to check imports
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	if n == 0 {
		return nil
	}

	// Quick check for prometheus imports
	header := buf[:n]
	if !bytes.Contains(header, []byte("prometheus")) &&
		!bytes.Contains(header, []byte("opencensus.io/stats")) {
		return nil
	}

	// Has prometheus - read rest of file and parse
	rest, _ := io.ReadAll(f)
	content := append(header, rest...)

	return parseMetricsFromSource(path, content)
}

// parseMetricsFromSource parses metrics from file content bytes.
func parseMetricsFromSource(path string, content []byte) []Metric {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil
	}

	var metrics []Metric
	seen := make(map[string]bool)

	// Find prefix variable value
	prefix := "curio_"
	ast.Inspect(node, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok {
			for i, name := range vs.Names {
				if name.Name == "pre" && i < len(vs.Values) {
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						prefix = strings.Trim(lit.Value, `"`)
					}
				}
			}
		}
		return true
	})

	// Collect metrics
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if m := parseMetricCall(x, prefix); m != nil && !seen[m.Name] {
				m.Source = path
				seen[m.Name] = true
				metrics = append(metrics, *m)
			}
			if m := parseStatsCall(x, prefix); m != nil && !seen[m.Name] {
				m.Source = path
				seen[m.Name] = true
				metrics = append(metrics, *m)
			}
		case *ast.KeyValueExpr:
			if call, ok := x.Value.(*ast.CallExpr); ok {
				if m := parseStatsCall(call, prefix); m != nil && !seen[m.Name] {
					m.Source = path
					seen[m.Name] = true
					metrics = append(metrics, *m)
				}
				if m := parseMetricCall(call, prefix); m != nil && !seen[m.Name] {
					m.Source = path
					seen[m.Name] = true
					metrics = append(metrics, *m)
				}
			}
		}
		return true
	})

	return metrics
}

// categoryFromPath derives a category name and key from the file path.
// Falls back to "99_Uncategorized" with a warning if no match found.
func categoryFromPath(path string) (key, name string) {
	cleanPath := filepath.Clean(path)

	for catKey, prefixes := range categoryConfig {
		for _, prefix := range prefixes {
			if strings.Contains(cleanPath, prefix) {
				// Extract display name by removing the "NN_" prefix
				name = catKey
				if len(catKey) > 3 && catKey[2] == '_' {
					name = catKey[3:]
				}
				return catKey, name
			}
		}
	}

	// Fallback for uncategorized files - warn but don't fail
	fmt.Fprintf(os.Stderr, "WARN: no category for %s (add entry to categoryConfig)\n", path)
	return "99_Uncategorized (" + filepath.Dir(path) + ")", "Uncategorized (" + filepath.Dir(path) + ")"
}

func printHeader() {
	fmt.Println(`<!-- AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY -->
<!-- Generated by: go run ./scripts/metricsdocgen -->
<!-- To regenerate: make docsgen-metrics -->

# Metrics Reference

This document lists all Prometheus metrics exported by Curio. All metrics use the ` + "`curio_`" + ` namespace prefix.

> **Note**: This file is auto-generated from source code. Run ` + "`make docsgen-metrics`" + ` to update.
`)
}

func printCategory(cat *Category) {
	fmt.Printf("## %s\n\n", cat.Name)

	// Check if any metrics have labels
	hasLabels := false
	for _, m := range cat.Metrics {
		if len(m.Labels) > 0 {
			hasLabels = true
			break
		}
	}

	if hasLabels {
		fmt.Println("| Metric | Type | Labels | Description |")
		fmt.Println("|--------|------|--------|-------------|")
	} else {
		fmt.Println("| Metric | Type | Description |")
		fmt.Println("|--------|------|-------------|")
	}

	// Sort metrics by name
	sort.Slice(cat.Metrics, func(i, j int) bool {
		return cat.Metrics[i].Name < cat.Metrics[j].Name
	})

	for _, m := range cat.Metrics {
		name := fmt.Sprintf("`%s`", m.Name)
		metricType := strings.ToLower(m.Type)
		help := m.Help
		if help == "" {
			help = "—"
		}

		if hasLabels {
			labels := "—"
			if len(m.Labels) > 0 {
				labels = "`" + strings.Join(m.Labels, "`, `") + "`"
			}
			fmt.Printf("| %s | %s | %s | %s |\n", name, metricType, labels, help)
		} else {
			fmt.Printf("| %s | %s | %s |\n", name, metricType, help)
		}
	}
	fmt.Println()
}

func printFooter() {
	fmt.Println(`---

*Generated from source files. See [Prometheus Metrics](prometheus-metrics.md) for setup instructions.*`)
}

func parseMetricCall(call *ast.CallExpr, prefix string) *Metric {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	funcName := sel.Sel.Name
	var metricType string

	switch funcName {
	case "NewHistogram":
		metricType = "Histogram"
	case "NewHistogramVec":
		metricType = "HistogramVec"
	case "NewCounter":
		metricType = "Counter"
	case "NewCounterVec":
		metricType = "CounterVec"
	case "NewGauge":
		metricType = "Gauge"
	case "NewGaugeVec":
		metricType = "GaugeVec"
	case "NewSummary":
		metricType = "Summary"
	case "NewSummaryVec":
		metricType = "SummaryVec"
	default:
		return nil
	}

	if len(call.Args) == 0 {
		return nil
	}

	// Parse the options struct
	opts, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		return nil
	}

	metric := &Metric{Type: metricType}

	for _, elt := range opts.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "Name":
			if lit, ok := kv.Value.(*ast.BasicLit); ok {
				metric.Name = strings.Trim(lit.Value, `"`)
			} else if bin, ok := kv.Value.(*ast.BinaryExpr); ok {
				// Handle concatenation like pre + "name"
				metric.Name = extractConcatString(bin, prefix)
			}
		case "Help":
			if lit, ok := kv.Value.(*ast.BasicLit); ok {
				metric.Help = strings.Trim(lit.Value, `"`)
			}
		case "Buckets":
			metric.Buckets = "custom"
		}
	}

	// Parse labels for Vec types (second argument)
	if strings.HasSuffix(metricType, "Vec") && len(call.Args) > 1 {
		if labels, ok := call.Args[1].(*ast.CompositeLit); ok {
			for _, elt := range labels.Elts {
				if lit, ok := elt.(*ast.BasicLit); ok {
					metric.Labels = append(metric.Labels, strings.Trim(lit.Value, `"`))
				}
			}
		}
	}

	// Ensure we have a valid metric name
	if metric.Name == "" {
		return nil
	}

	// Add curio_ prefix if not present
	if !strings.HasPrefix(metric.Name, "curio_") {
		metric.Name = "curio_" + metric.Name
	}

	return metric
}

func parseStatsCall(call *ast.CallExpr, prefix string) *Metric {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	// Check if this is a stats.Int64 or stats.Float64 call
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "stats" {
		return nil
	}

	var metricType string
	switch sel.Sel.Name {
	case "Int64":
		metricType = "Gauge/Counter"
	case "Float64":
		metricType = "Gauge"
	default:
		return nil
	}

	if len(call.Args) < 2 {
		return nil
	}

	metric := &Metric{Type: metricType}

	// First arg is name
	switch v := call.Args[0].(type) {
	case *ast.BasicLit:
		metric.Name = strings.Trim(v.Value, `"`)
	case *ast.BinaryExpr:
		metric.Name = extractConcatString(v, prefix)
	}

	// Second arg is description
	if lit, ok := call.Args[1].(*ast.BasicLit); ok {
		metric.Help = strings.Trim(lit.Value, `"`)
	}

	if metric.Name == "" {
		return nil
	}

	// Add curio_ prefix if not present
	if !strings.HasPrefix(metric.Name, "curio_") {
		metric.Name = "curio_" + metric.Name
	}

	return metric
}

func extractConcatString(bin *ast.BinaryExpr, prefix string) string {
	var parts []string

	var extract func(expr ast.Expr)
	extract = func(expr ast.Expr) {
		switch v := expr.(type) {
		case *ast.BasicLit:
			parts = append(parts, strings.Trim(v.Value, `"`))
		case *ast.Ident:
			// Resolve the "pre" variable to the known prefix
			if v.Name == "pre" {
				parts = append(parts, prefix)
			}
		case *ast.BinaryExpr:
			extract(v.X)
			extract(v.Y)
		}
	}

	extract(bin)
	return strings.Join(parts, "")
}

// findWorkspaceRoot finds the workspace root by looking for go.mod
func findWorkspaceRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func init() {
	// Change to workspace root if needed
	root := findWorkspaceRoot()
	if root != "" {
		os.Chdir(root)
	}
}
