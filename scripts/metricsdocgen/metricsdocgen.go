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
	"log"
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
	"00_Node Metrics":                       {"lib/metrics"},
	"01_Database Metrics (HarmonyDB)":       {"harmony/harmonydb"},
	"02_Task Metrics (HarmonyTask)":         {"harmony/harmonytask"},
	"03_Wallet Exporter Metrics (Optional)": {"deps/stats"},
	"04_Proof Share Metrics":                {"tasks/proofshare"},
	"05_Proof Service Metrics":              {"lib/proofsvc"},
	"06_Sealing Metrics":                    {"tasks/seal"},
	"07_Snap Pipeline Metrics":              {"tasks/snap"},
	"08_Mining Metrics":                     {"tasks/winning"},
	"09_Storage Metrics":                    {"lib/paths"},
	"10_Cache Metrics":                      {"lib/cachedreader"},
	"11_Network Metrics":                    {"lib/robusthttp", "lib/dealdata"},
	"12_FFI Metrics":                        {"lib/ffi"},
	"13_HTTP/Retrieval Metrics":             {"market/"},
	"14_GC Metrics":                         {"tasks/gc"},
	"15_Batching Metrics":                   {"tasks/sealsupra", "lib/slotmgr"},
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

type viewDef struct {
	Name        string
	MeasureExpr string
	Type        string
	Labels      []string
}

var registeredLotusMetricViews = map[string]Metric{
	"DagStorePRAtCacheFillCountView": externalOpenCensusMetric("dagstore/pr_at_cache_fill_count", "Counter", "PieceReader ReadAt full cache fill count", []string{"network"}),
	"DagStorePRAtHitBytesView":       externalOpenCensusMetric("dagstore/pr_at_hit_bytes", "Counter", "PieceReader ReadAt bytes from cache", []string{"network"}),
	"DagStorePRAtHitCountView":       externalOpenCensusMetric("dagstore/pr_at_hit_count", "Counter", "PieceReader ReadAt from cache hits", []string{"network"}),
	"DagStorePRAtReadBytesView":      externalOpenCensusMetric("dagstore/pr_at_read_bytes", "Counter", "PieceReader ReadAt bytes read from source", []string{"pr_size", "network"}),
	"DagStorePRAtReadCountView":      externalOpenCensusMetric("dagstore/pr_at_read_count", "Counter", "PieceReader ReadAt reads from source", []string{"pr_size", "network"}),
	"DagStorePRBytesDiscardedView":   externalOpenCensusMetric("dagstore/pr_discarded_bytes", "Counter", "PieceReader discarded bytes", []string{"network"}),
	"DagStorePRBytesRequestedView":   externalOpenCensusMetric("dagstore/pr_requested_bytes", "Counter", "PieceReader requested bytes", []string{"pr_type", "network"}),
	"DagStorePRDiscardCountView":     externalOpenCensusMetric("dagstore/pr_discard_count", "Counter", "PieceReader discard count", []string{"network"}),
	"DagStorePRInitCountView":        externalOpenCensusMetric("dagstore/pr_init_count", "Counter", "PieceReader init count", []string{"network"}),
	"DagStorePRSeekBackBytesView":    externalOpenCensusMetric("dagstore/pr_seek_back_bytes", "Counter", "PieceReader seek back bytes", []string{"network"}),
	"DagStorePRSeekBackCountView":    externalOpenCensusMetric("dagstore/pr_seek_back_count", "Counter", "PieceReader seek back count", []string{"network"}),
	"DagStorePRSeekForwardBytesView": externalOpenCensusMetric("dagstore/pr_seek_forward_bytes", "Counter", "PieceReader seek forward bytes", []string{"network"}),
	"DagStorePRSeekForwardCountView": externalOpenCensusMetric("dagstore/pr_seek_forward_count", "Counter", "PieceReader seek forward count", []string{"network"}),
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
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
	if err != nil {
		log.Fatalf("Failed to walk directory: %v", err)
	}

	// Process in parallel: open, check, parse, close - all in one worker call
	numWorkers := min(runtime.NumCPU(), 8)

	jobs := make(chan string, len(candidates))
	var results []fileResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Go(func() {
			for path := range jobs {
				if metrics := processFile(path); len(metrics) > 0 {
					mu.Lock()
					results = append(results, fileResult{file: path, metrics: metrics})
					mu.Unlock()
				}
			}
		})
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
	defer func() { _ = f.Close() }()

	// Read the file once before checking imports. Relying only on a small
	// header can miss files with long package comments or generated headers.
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	if n == 0 {
		return nil
	}
	rest, _ := io.ReadAll(f)
	content := append(buf[:n], rest...)

	// Quick check for metrics imports before parsing.
	if !bytes.Contains(content, []byte("prometheus")) &&
		!bytes.Contains(content, []byte("opencensus.io/stats")) {
		return nil
	}

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
	measures := make(map[string]Metric)
	tagKeys := make(map[string]string)
	aggregationVars := make(map[string]string)
	viewVars := make(map[string]viewDef)
	lotusMetricsAliases := importAliases(node, "github.com/filecoin-project/lotus/metrics")

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

	// Collect local symbols first so view registrations can resolve measure
	// names, aggregation types, and tag labels.
	ast.Inspect(node, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}

		for i, value := range vs.Values {
			var assignName string
			if i < len(vs.Names) {
				assignName = vs.Names[i].Name
			}

			if label, ok := parseTagKeyCall(value); ok && assignName != "" {
				tagKeys[assignName] = label
			}

			if aggType := aggregationType(value, aggregationVars); aggType != "" && assignName != "" {
				aggregationVars[assignName] = aggType
			}

			if view := parseView(value, aggregationVars, tagKeys); view != nil && assignName != "" {
				viewVars[assignName] = *view
			}

			if metric := parseStatsMeasure(value, prefix); metric != nil && assignName != "" {
				measures[assignName] = *metric
			}

			if composite, ok := indirectCompositeLit(value); ok && assignName != "" {
				for _, elt := range composite.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					fieldName := exprName(kv.Key)
					if fieldName == "" {
						continue
					}
					if metric := parseStatsMeasure(kv.Value, prefix); metric != nil {
						measures[assignName+"."+fieldName] = *metric
					}
					if view := parseView(kv.Value, aggregationVars, tagKeys); view != nil {
						viewVars[assignName+"."+fieldName] = *view
					}
				}
			}
		}
		return true
	})

	// Direct Prometheus collectors are exported by the collector itself.
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if m := parseMetricCall(call, prefix); m != nil && !seen[m.Name] {
			m.Source = path
			seen[m.Name] = true
			metrics = append(metrics, *m)
		}
		return true
	})

	// OpenCensus metrics are exported through registered views, so the view
	// aggregation decides the Prometheus type and TagKeys decide labels.
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isSelector(call.Fun, "view", "Register") {
			return true
		}
		for _, arg := range call.Args {
			view := parseView(arg, aggregationVars, tagKeys)
			if view == nil {
				if name := exprName(arg); name != "" {
					if registeredView, ok := viewVars[name]; ok {
						view = &registeredView
					} else if metric, ok := registeredLotusMetricView(arg, lotusMetricsAliases); ok {
						metric.Source = path
						if !seen[metric.Name] {
							seen[metric.Name] = true
							metrics = append(metrics, metric)
						}
						continue
					} else {
						fmt.Fprintf(os.Stderr, "WARN: unresolved registered view %s in %s\n", name, path)
					}
				}
			}
			if view == nil {
				continue
			}

			measure, ok := measures[view.MeasureExpr]
			if !ok {
				fmt.Fprintf(os.Stderr, "WARN: unresolved measure %s for registered view in %s\n", view.MeasureExpr, path)
				continue
			}
			viewName := measure.Name
			if view.Name != "" {
				viewName = view.Name
			}
			measure.Name = exportedOpenCensusName(viewName)
			measure.Type = view.Type
			measure.Labels = view.Labels
			measure.Source = path
			if !seen[measure.Name] {
				seen[measure.Name] = true
				metrics = append(metrics, measure)
			}
		}
		return true
	})

	return metrics
}

func externalOpenCensusMetric(name, typ, help string, labels []string) Metric {
	return Metric{
		Name:   exportedOpenCensusName(name),
		Type:   typ,
		Help:   help,
		Labels: labels,
	}
}

func importAliases(node *ast.File, importPath string) map[string]bool {
	aliases := make(map[string]bool)
	for _, spec := range node.Imports {
		if strings.Trim(spec.Path.Value, `"`) != importPath {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name != "." && spec.Name.Name != "_" {
				aliases[spec.Name.Name] = true
			}
			continue
		}
		aliases[lastImportPathPart(importPath)] = true
	}
	return aliases
}

func lastImportPathPart(importPath string) string {
	idx := strings.LastIndex(importPath, "/")
	if idx < 0 {
		return importPath
	}
	return importPath[idx+1:]
}

func registeredLotusMetricView(expr ast.Expr, lotusMetricsAliases map[string]bool) (Metric, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return Metric{}, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || !lotusMetricsAliases[pkg.Name] {
		return Metric{}, false
	}
	metric, ok := registeredLotusMetricViews[sel.Sel.Name]
	return metric, ok
}

// categoryFromPath derives a category name and key from the file path.
// Falls back to "99_Uncategorized" with a warning if no match found.
func categoryFromPath(path string) (key, name string) {
	cleanPath := filepath.Clean(path)

	var bestKey string
	bestPrefixLen := -1

	keys := make([]string, 0, len(categoryConfig))
	for catKey := range categoryConfig {
		keys = append(keys, catKey)
	}
	sort.Strings(keys)

	for _, catKey := range keys {
		prefixes := categoryConfig[catKey]
		for _, prefix := range prefixes {
			if pathMatchesPrefix(cleanPath, prefix) && len(prefix) > bestPrefixLen {
				bestKey = catKey
				bestPrefixLen = len(prefix)
			}
		}
	}

	if bestKey != "" {
		name = bestKey
		if len(bestKey) > 3 && bestKey[2] == '_' {
			name = bestKey[3:]
		}
		return bestKey, name
	}

	// Fallback for uncategorized files - warn but don't fail
	fmt.Fprintf(os.Stderr, "WARN: no category for %s (add entry to categoryConfig)\n", path)
	return "99_Uncategorized (" + filepath.Dir(path) + ")", "Uncategorized (" + filepath.Dir(path) + ")"
}

func pathMatchesPrefix(path, prefix string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	prefix = filepath.ToSlash(filepath.Clean(prefix))

	return path == prefix ||
		strings.HasPrefix(path, prefix+"/") ||
		strings.Contains(path, "/"+prefix+"/")
}

func printHeader() {
	fmt.Println(`<!-- AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY -->
<!-- Generated by: go run ./scripts/metricsdocgen -->
<!-- To regenerate: make docsgen-metrics -->

# Metrics Reference

This document lists Prometheus metrics exported by Curio using the exact metric names exposed on the scrape endpoint.

> **Note**: This file is auto-generated from source code. Run ` + "`make docsgen-metrics`" + ` to update.`)
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
	isVec := strings.HasSuffix(funcName, "Vec")

	switch funcName {
	case "NewHistogram", "NewHistogramVec":
		metricType = "Histogram"
	case "NewCounter", "NewCounterVec":
		metricType = "Counter"
	case "NewGauge", "NewGaugeVec":
		metricType = "Gauge"
	case "NewSummary", "NewSummaryVec":
		metricType = "Summary"
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
	if isVec && len(call.Args) > 1 {
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

	return metric
}

func parseStatsMeasure(expr ast.Expr, prefix string) *Metric {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}

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
		metricType = "Counter"
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

	return metric
}

func parseTagKeyCall(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, "tag", "NewKey") || len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return "", false
	}
	return strings.Trim(lit.Value, `"`), true
}

func parseView(expr ast.Expr, aggregationVars map[string]string, tagKeys map[string]string) *viewDef {
	composite, ok := indirectCompositeLit(expr)
	if !ok || !isViewComposite(composite) {
		return nil
	}

	view := &viewDef{}
	for _, elt := range composite.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := exprName(kv.Key)
		switch key {
		case "Name":
			view.Name = stringExpr(kv.Value, "curio_")
		case "Measure":
			view.MeasureExpr = exprName(kv.Value)
		case "Aggregation":
			view.Type = aggregationType(kv.Value, aggregationVars)
		case "TagKeys":
			view.Labels = parseTagKeys(kv.Value, tagKeys)
		}
	}
	if view.MeasureExpr == "" || view.Type == "" {
		return nil
	}
	return view
}

func aggregationType(expr ast.Expr, aggregationVars map[string]string) string {
	switch x := expr.(type) {
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "view" {
			return ""
		}
		switch sel.Sel.Name {
		case "Distribution":
			return "Histogram"
		case "LastValue":
			return "Gauge"
		case "Sum", "Count":
			return "Counter"
		default:
			return ""
		}
	case *ast.Ident:
		return aggregationVars[x.Name]
	default:
		return ""
	}
}

func parseTagKeys(expr ast.Expr, tagKeys map[string]string) []string {
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}

	var labels []string
	for _, elt := range composite.Elts {
		key := exprName(elt)
		if key == "" {
			continue
		}
		label, ok := tagKeys[key]
		if !ok {
			label = lastNamePart(key)
		}
		labels = append(labels, sanitizePrometheusName(label))
	}
	return labels
}

func indirectCompositeLit(expr ast.Expr) (*ast.CompositeLit, bool) {
	switch x := expr.(type) {
	case *ast.CompositeLit:
		return x, true
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return indirectCompositeLit(x.X)
		}
	}
	return nil, false
}

func isViewComposite(composite *ast.CompositeLit) bool {
	switch typ := composite.Type.(type) {
	case *ast.SelectorExpr:
		return isSelector(typ, "view", "View")
	case *ast.StarExpr:
		if sel, ok := typ.X.(*ast.SelectorExpr); ok {
			return isSelector(sel, "view", "View")
		}
	}
	return false
}

func isSelector(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg
}

func exprName(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		parent := exprName(x.X)
		if parent == "" {
			return x.Sel.Name
		}
		return parent + "." + x.Sel.Name
	case *ast.UnaryExpr:
		return exprName(x.X)
	case *ast.ParenExpr:
		return exprName(x.X)
	default:
		return ""
	}
}

func lastNamePart(name string) string {
	parts := strings.Split(name, ".")
	return parts[len(parts)-1]
}

func exportedOpenCensusName(name string) string {
	return "curio_" + sanitizePrometheusName(name)
}

func sanitizePrometheusName(name string) string {
	if name == "" {
		return name
	}
	if len(name) > 100 {
		name = name[:100]
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	out := b.String()
	if strings.HasPrefix(out, "_") {
		return "key" + out
	}
	return out
}

func stringExpr(expr ast.Expr, prefix string) string {
	switch x := expr.(type) {
	case *ast.BasicLit:
		return strings.Trim(x.Value, `"`)
	case *ast.BinaryExpr:
		return extractConcatString(x, prefix)
	default:
		return ""
	}
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
		if err := os.Chdir(root); err != nil {
			panic(err)
		}
	}
}
