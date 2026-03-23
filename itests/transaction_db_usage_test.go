package itests

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoDBCallsInsideTransactions(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "failed to resolve test file path")

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), ".."))

	fset := token.NewFileSet()
	var violations []string

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		importPaths := importedPackagePaths(file)
		importedNames := importedPackageNames(importPaths)
		seenScopes := map[token.Pos]struct{}{}

		checkScope := func(scopePos token.Pos, body *ast.BlockStmt, txParamNames map[string]struct{}, txReceiver ast.Expr) {
			if body == nil {
				return
			}
			if _, seen := seenScopes[scopePos]; seen {
				return
			}
			seenScopes[scopePos] = struct{}{}

			txReceiverText := renderExpr(fset, txReceiver)

			ast.Inspect(body, func(node ast.Node) bool {
				innerCall, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := innerCall.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				if receiverStartsWithTxParam(sel.X, txParamNames) {
					return true
				}

				if !isDBReceiver(sel.X, txReceiverText, importedNames, fset) {
					return true
				}

				pos := fset.Position(sel.Pos())
				relPath, relErr := filepath.Rel(repoRoot, path)
				if relErr != nil {
					relPath = path
				}

				violations = append(violations, filepath.ToSlash(relPath)+":"+strconv.Itoa(pos.Line)+": "+renderExpr(fset, innerCall))
				return true
			})
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			cb, txReceiver, ok := transactionCallback(call)
			if !ok {
				return true
			}

			txParamNames, _ := txParamNames(cb.Type.Params, importPaths, file.Name.Name)
			if len(txParamNames) == 0 {
				txParamNames = allParamNames(cb.Type.Params)
			}
			checkScope(cb.Pos(), cb.Body, txParamNames, txReceiver)
			return true
		})

		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncLit)
			if !ok {
				return true
			}

			txNames, hasTx := txParamNames(fn.Type.Params, importPaths, file.Name.Name)
			if !hasTx {
				return true
			}

			checkScope(fn.Pos(), fn.Body, txNames, nil)
			return true
		})

		return nil
	})
	require.NoError(t, err)

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("db.* calls inside transaction callbacks are forbidden:\n%s", strings.Join(violations, "\n"))
	}
}

func transactionCallback(call *ast.CallExpr) (*ast.FuncLit, ast.Expr, bool) {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if fn.Sel.Name != "BeginTransaction" {
			return nil, nil, false
		}

		for _, arg := range call.Args {
			if cb, ok := arg.(*ast.FuncLit); ok {
				return cb, fn.X, true
			}
		}

		return nil, nil, false
	case *ast.Ident:
		if fn.Name != "harmonyQuery" {
			return nil, nil, false
		}

		var dbArg ast.Expr
		if len(call.Args) >= 2 {
			dbArg = call.Args[1]
		}
		for _, arg := range call.Args {
			if cb, ok := arg.(*ast.FuncLit); ok {
				return cb, dbArg, true
			}
		}

		return nil, nil, false
	default:
		return nil, nil, false
	}
}

func txParamNames(params *ast.FieldList, importPaths map[string]string, pkgName string) (map[string]struct{}, bool) {
	out := map[string]struct{}{}
	var hasTx bool
	if params == nil {
		return out, false
	}

	for _, field := range params.List {
		if !isHarmonyTxType(field.Type, importPaths, pkgName) {
			continue
		}
		hasTx = true
		for _, name := range field.Names {
			out[name.Name] = struct{}{}
		}
	}

	return out, hasTx
}

func allParamNames(params *ast.FieldList) map[string]struct{} {
	out := map[string]struct{}{}
	if params == nil {
		return out
	}

	for _, field := range params.List {
		for _, name := range field.Names {
			out[name.Name] = struct{}{}
		}
	}

	return out
}

func isHarmonyTxType(expr ast.Expr, importPaths map[string]string, pkgName string) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return isHarmonyTxType(t.X, importPaths, pkgName)
	case *ast.SelectorExpr:
		if t.Sel.Name != "Tx" {
			return false
		}
		pkgIdent, ok := t.X.(*ast.Ident)
		if !ok {
			return false
		}
		importPath, ok := importPaths[pkgIdent.Name]
		if !ok {
			return false
		}
		return isHarmonyDBImport(importPath)
	case *ast.Ident:
		// Handle same-package references like *Tx inside harmonydb package.
		return pkgName == "harmonydb" && t.Name == "Tx"
	default:
		return false
	}
}

func isHarmonyDBImport(importPath string) bool {
	return importPath == "github.com/filecoin-project/curio/harmony/harmonydb" ||
		strings.HasSuffix(importPath, "/harmony/harmonydb")
}

func receiverStartsWithTxParam(expr ast.Expr, txParamNames map[string]struct{}) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		_, ok := txParamNames[e.Name]
		return ok
	case *ast.SelectorExpr:
		return receiverStartsWithTxParam(e.X, txParamNames)
	case *ast.ParenExpr:
		return receiverStartsWithTxParam(e.X, txParamNames)
	case *ast.StarExpr:
		return receiverStartsWithTxParam(e.X, txParamNames)
	case *ast.IndexExpr:
		return receiverStartsWithTxParam(e.X, txParamNames)
	case *ast.IndexListExpr:
		return receiverStartsWithTxParam(e.X, txParamNames)
	default:
		return false
	}
}

func isDBReceiver(expr ast.Expr, txReceiverText string, importedNames map[string]struct{}, fset *token.FileSet) bool {
	if txReceiverText != "" && renderExpr(fset, expr) == txReceiverText {
		return true
	}

	return containsDBRef(expr, importedNames)
}

func containsDBRef(expr ast.Expr, importedNames map[string]struct{}) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		if _, isImport := importedNames[e.Name]; isImport {
			return false
		}
		return looksLikeDBName(e.Name)
	case *ast.SelectorExpr:
		if looksLikeDBName(e.Sel.Name) {
			return true
		}
		return containsDBRef(e.X, importedNames)
	case *ast.ParenExpr:
		return containsDBRef(e.X, importedNames)
	case *ast.StarExpr:
		return containsDBRef(e.X, importedNames)
	case *ast.IndexExpr:
		return containsDBRef(e.X, importedNames)
	case *ast.IndexListExpr:
		return containsDBRef(e.X, importedNames)
	default:
		return false
	}
}

func looksLikeDBName(name string) bool {
	if name == "db" || name == "DB" {
		return true
	}

	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "db")
}

func importedPackagePaths(file *ast.File) map[string]string {
	paths := map[string]string{}
	for _, spec := range file.Imports {
		pathValue := strings.Trim(spec.Path.Value, `"`)
		if pathValue == "" {
			continue
		}

		if spec.Name != nil {
			paths[spec.Name.Name] = pathValue
			continue
		}

		base := filepath.Base(pathValue)
		if base != "" {
			paths[base] = pathValue
		}
	}
	return paths
}

func importedPackageNames(importPaths map[string]string) map[string]struct{} {
	names := map[string]struct{}{}
	for name := range importPaths {
		names[name] = struct{}{}
	}
	return names
}

func renderExpr(fset *token.FileSet, expr ast.Node) string {
	if expr == nil {
		return ""
	}
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, expr); err != nil {
		return ""
	}
	return b.String()
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".idea", ".vscode", "node_modules", "venv", "vendor":
		return true
	default:
		return false
	}
}
