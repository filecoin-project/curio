package ittestgroup3

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestNoDBCallsInsideTransactions(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "failed to resolve test file path")

	repoRoot, err := findScanRoot(filepath.Dir(thisFile))
	require.NoError(t, err)

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir: repoRoot,
	}
	pkgs, err := packages.Load(cfg, "./...")
	require.NoError(t, err)

	var loadErrors []string
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			loadErrors = append(loadErrors, pkgErr.Error())
		}
	}
	require.Empty(t, loadErrors, "failed to load packages:\n%s", strings.Join(loadErrors, "\n"))

	var violations []string

	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil || pkg.Fset == nil {
			continue
		}

		for _, file := range pkg.Syntax {
			tf := pkg.Fset.File(file.Pos())
			if tf == nil {
				continue
			}
			path := tf.Name()
			if !strings.HasSuffix(path, ".go") {
				continue
			}

			importPaths := importedPackagePaths(file)
			importedNames := importedPackageNames(importPaths)
			seenScopes := map[token.Pos]struct{}{}

			checkScope := func(scopePos token.Pos, body *ast.BlockStmt, txParamNames map[string]struct{}, dbParamNames map[string]struct{}, dbReceiverExprs []ast.Expr) {
				if body == nil {
					return
				}
				if _, seen := seenScopes[scopePos]; seen {
					return
				}
				seenScopes[scopePos] = struct{}{}

				dbReceiverTexts := map[string]struct{}{}
				for _, expr := range dbReceiverExprs {
					if txt := renderExpr(pkg.Fset, expr); txt != "" {
						dbReceiverTexts[txt] = struct{}{}
					}
				}

				txAliases := inferAliasesFromAssignments(body, txParamNames)
				dbAliases := inferDBAliasesFromAssignments(body, dbReceiverTexts, importedNames, txAliases, dbParamNames, pkg.Fset)
				collectDBCallsInTxContext(body, txAliases, func(innerCall *ast.CallExpr, sel *ast.SelectorExpr) {
					if receiverStartsWithName(sel.X, txAliases) {
						return
					}

					if !isDBReceiver(sel.X, dbReceiverTexts, importedNames, dbAliases, pkg.Fset) {
						return
					}

					pos := pkg.Fset.Position(sel.Pos())
					relPath, relErr := filepath.Rel(repoRoot, path)
					if relErr != nil {
						relPath = path
					}

					violations = append(violations, filepath.ToSlash(relPath)+":"+strconv.Itoa(pos.Line)+": "+renderExpr(pkg.Fset, innerCall))
				})
			}

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				scopes := transactionCallbacks(call, pkg.TypesInfo, importPaths, file.Name.Name)
				for _, scope := range scopes {
					dbNames, _ := dbParamNamesWithFallback(scope.cb.Type.Params, pkg.TypesInfo, importPaths, file.Name.Name)
					checkScope(scope.cb.Pos(), scope.cb.Body, scope.txNames, dbNames, scope.dbReceivers)
				}
				return true
			})

			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncLit)
				if !ok {
					return true
				}

				txNames, hasTx := txParamNamesWithFallback(fn.Type.Params, pkg.TypesInfo, importPaths, file.Name.Name)
				if !hasTx {
					return true
				}
				dbNames, _ := dbParamNamesWithFallback(fn.Type.Params, pkg.TypesInfo, importPaths, file.Name.Name)

				checkScope(fn.Pos(), fn.Body, txNames, dbNames, nil)
				return true
			})

			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					return true
				}

				txNames, hasTx := txParamNamesWithFallback(fn.Type.Params, pkg.TypesInfo, importPaths, file.Name.Name)
				if !hasTx {
					return true
				}
				dbNames, _ := dbParamNamesWithFallback(fn.Type.Params, pkg.TypesInfo, importPaths, file.Name.Name)

				checkScope(fn.Pos(), fn.Body, txNames, dbNames, nil)
				return true
			})
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("db.* calls inside transaction callbacks are forbidden:\n%s", strings.Join(violations, "\n"))
	}
}

type txCallbackScope struct {
	cb          *ast.FuncLit
	txNames     map[string]struct{}
	dbReceivers []ast.Expr
}

func transactionCallbacks(call *ast.CallExpr, info *types.Info, importPaths map[string]string, pkgName string) []txCallbackScope {
	var out []txCallbackScope
	dbReceivers := dbReceiverExprsForCall(call, info)

	for _, arg := range call.Args {
		cb, ok := arg.(*ast.FuncLit)
		if !ok {
			continue
		}

		txNames, hasTx := txParamNamesWithFallback(cb.Type.Params, info, importPaths, pkgName)
		if !hasTx {
			continue
		}
		if len(txNames) == 0 {
			txNames = allParamNames(cb.Type.Params)
		}
		out = append(out, txCallbackScope{
			cb:          cb,
			txNames:     txNames,
			dbReceivers: dbReceivers,
		})
	}
	return out
}

func dbReceiverExprsForCall(call *ast.CallExpr, info *types.Info) []ast.Expr {
	var out []ast.Expr
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isHarmonyDBGoType(typeOfExpr(info, sel.X)) {
		out = append(out, sel.X)
	}

	for _, arg := range call.Args {
		if _, ok := arg.(*ast.FuncLit); ok {
			continue
		}
		if isHarmonyDBGoType(typeOfExpr(info, arg)) {
			out = append(out, arg)
		}
	}

	return out
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

func dbParamNames(params *ast.FieldList, importPaths map[string]string, pkgName string) (map[string]struct{}, bool) {
	out := map[string]struct{}{}
	var hasDB bool
	if params == nil {
		return out, false
	}

	for _, field := range params.List {
		if !isHarmonyDBType(field.Type, importPaths, pkgName) {
			continue
		}
		hasDB = true
		for _, name := range field.Names {
			out[name.Name] = struct{}{}
		}
	}

	return out, hasDB
}

func txParamNamesWithFallback(params *ast.FieldList, info *types.Info, importPaths map[string]string, pkgName string) (map[string]struct{}, bool) {
	if typed, hasTyped := paramNamesByType(params, info, isHarmonyTxGoType); hasTyped {
		return typed, true
	}
	return txParamNames(params, importPaths, pkgName)
}

func dbParamNamesWithFallback(params *ast.FieldList, info *types.Info, importPaths map[string]string, pkgName string) (map[string]struct{}, bool) {
	if typed, hasTyped := paramNamesByType(params, info, isHarmonyDBGoType); hasTyped {
		return typed, true
	}
	return dbParamNames(params, importPaths, pkgName)
}

func paramNamesByType(params *ast.FieldList, info *types.Info, isTargetType func(types.Type) bool) (map[string]struct{}, bool) {
	out := map[string]struct{}{}
	if params == nil || info == nil {
		return out, false
	}

	var hasMatch bool
	for _, field := range params.List {
		if !isTargetType(typeOfExpr(info, field.Type)) {
			continue
		}
		hasMatch = true
		for _, name := range field.Names {
			out[name.Name] = struct{}{}
		}
	}

	return out, hasMatch
}

func typeOfExpr(info *types.Info, expr ast.Expr) types.Type {
	if info == nil || expr == nil {
		return nil
	}
	return info.TypeOf(expr)
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

func isHarmonyDBType(expr ast.Expr, importPaths map[string]string, pkgName string) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return isHarmonyDBType(t.X, importPaths, pkgName)
	case *ast.SelectorExpr:
		if t.Sel.Name != "DB" {
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
		// Handle same-package references like *DB inside harmonydb package.
		return pkgName == "harmonydb" && t.Name == "DB"
	default:
		return false
	}
}

func isHarmonyTxGoType(t types.Type) bool {
	return isHarmonyNamedGoType(t, "Tx")
}

func isHarmonyDBGoType(t types.Type) bool {
	return isHarmonyNamedGoType(t, "DB")
}

func isHarmonyNamedGoType(t types.Type, wantName string) bool {
	if t == nil {
		return false
	}

	base := types.Unalias(t)
	for {
		ptr, ok := base.(*types.Pointer)
		if !ok {
			break
		}
		base = types.Unalias(ptr.Elem())
	}

	named, ok := base.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Name() != wantName {
		return false
	}

	return isHarmonyDBImport(obj.Pkg().Path())
}

func isHarmonyDBImport(importPath string) bool {
	return importPath == "github.com/filecoin-project/curio/harmony/harmonydb" ||
		importPath == "github.com/curiostorage/harmonyquery" ||
		strings.HasSuffix(importPath, "/harmony/harmonydb")
}

func receiverStartsWithName(expr ast.Expr, names map[string]struct{}) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		_, ok := names[e.Name]
		return ok
	case *ast.SelectorExpr:
		return receiverStartsWithName(e.X, names)
	case *ast.ParenExpr:
		return receiverStartsWithName(e.X, names)
	case *ast.StarExpr:
		return receiverStartsWithName(e.X, names)
	case *ast.IndexExpr:
		return receiverStartsWithName(e.X, names)
	case *ast.IndexListExpr:
		return receiverStartsWithName(e.X, names)
	default:
		return false
	}
}

func isDBReceiver(expr ast.Expr, dbReceiverTexts map[string]struct{}, importedNames map[string]struct{}, dbAliases map[string]struct{}, fset *token.FileSet) bool {
	if len(dbReceiverTexts) > 0 {
		if _, ok := dbReceiverTexts[renderExpr(fset, expr)]; ok {
			return true
		}
	}
	if receiverStartsWithName(expr, dbAliases) {
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

func inferAliasesFromAssignments(body *ast.BlockStmt, seed map[string]struct{}) map[string]struct{} {
	aliases := copyNameSet(seed)
	for {
		changed := false
		inspectAssignmentPairs(body, func(lhs *ast.Ident, rhs ast.Expr) {
			if receiverStartsWithName(rhs, aliases) {
				if _, exists := aliases[lhs.Name]; !exists {
					aliases[lhs.Name] = struct{}{}
					changed = true
				}
			}
		})
		if !changed {
			return aliases
		}
	}
}

func inferDBAliasesFromAssignments(
	body *ast.BlockStmt,
	dbReceiverTexts map[string]struct{},
	importedNames map[string]struct{},
	txAliases map[string]struct{},
	seed map[string]struct{},
	fset *token.FileSet,
) map[string]struct{} {
	aliases := copyNameSet(seed)
	for {
		changed := false
		inspectAssignmentPairs(body, func(lhs *ast.Ident, rhs ast.Expr) {
			if receiverStartsWithName(rhs, txAliases) {
				return
			}
			if !isDBReceiver(rhs, dbReceiverTexts, importedNames, aliases, fset) {
				return
			}
			if _, exists := aliases[lhs.Name]; !exists {
				aliases[lhs.Name] = struct{}{}
				changed = true
			}
		})
		if !changed {
			return aliases
		}
	}
}

func inspectAssignmentPairs(body *ast.BlockStmt, onPair func(lhs *ast.Ident, rhs ast.Expr)) {
	if body == nil {
		return
	}

	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}

		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) != len(s.Rhs) {
				return true
			}
			for i, lhsExpr := range s.Lhs {
				lhs, ok := lhsExpr.(*ast.Ident)
				if !ok || lhs.Name == "_" {
					continue
				}
				rhs := s.Rhs[i]
				onPair(lhs, rhs)
			}
		case *ast.ValueSpec:
			if len(s.Names) != len(s.Values) {
				return true
			}
			for i, lhs := range s.Names {
				if lhs == nil || lhs.Name == "_" {
					continue
				}
				rhs := s.Values[i]
				onPair(lhs, rhs)
			}
		}
		return true
	})
}

func collectDBCallsInTxContext(body *ast.BlockStmt, txParamNames map[string]struct{}, onCall func(*ast.CallExpr, *ast.SelectorExpr)) {
	if body == nil {
		return
	}

	var scanBlock func(*ast.BlockStmt, map[string]struct{})
	var scanStmt func(ast.Stmt, map[string]struct{})
	var scanElse func(ast.Stmt, map[string]struct{})

	scanCallsInNode := func(node ast.Node, activeTx map[string]struct{}) {
		if node == nil || len(activeTx) == 0 {
			return
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}

			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			onCall(call, sel)
			return true
		})
	}

	scanBlock = func(block *ast.BlockStmt, activeTx map[string]struct{}) {
		if block == nil {
			return
		}
		for _, stmt := range block.List {
			scanStmt(stmt, activeTx)
		}
	}

	scanElse = func(stmt ast.Stmt, activeTx map[string]struct{}) {
		switch s := stmt.(type) {
		case *ast.BlockStmt:
			scanBlock(s, activeTx)
		case *ast.IfStmt:
			scanStmt(s, activeTx)
		default:
			scanCallsInNode(s, activeTx)
		}
	}

	scanStmt = func(stmt ast.Stmt, activeTx map[string]struct{}) {
		if stmt == nil {
			return
		}
		switch s := stmt.(type) {
		case *ast.BlockStmt:
			scanBlock(s, activeTx)
		case *ast.IfStmt:
			scanCallsInNode(s.Init, activeTx)
			scanCallsInNode(s.Cond, activeTx)

			thenActive := copyNameSet(activeTx)
			elseActive := copyNameSet(activeTx)

			if txName, op, ok := txNilCompare(s.Cond, txParamNames); ok {
				switch op {
				case token.EQL:
					delete(thenActive, txName)
				case token.NEQ:
					delete(elseActive, txName)
				}
			}

			scanBlock(s.Body, thenActive)
			if s.Else != nil {
				scanElse(s.Else, elseActive)
			}
		case *ast.ForStmt:
			scanCallsInNode(s.Init, activeTx)
			scanCallsInNode(s.Cond, activeTx)
			scanCallsInNode(s.Post, activeTx)
			scanBlock(s.Body, activeTx)
		case *ast.RangeStmt:
			scanCallsInNode(s.Key, activeTx)
			scanCallsInNode(s.Value, activeTx)
			scanCallsInNode(s.X, activeTx)
			scanBlock(s.Body, activeTx)
		case *ast.SwitchStmt:
			scanCallsInNode(s.Init, activeTx)
			scanCallsInNode(s.Tag, activeTx)
			for _, clause := range s.Body.List {
				cc, ok := clause.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range cc.List {
					scanCallsInNode(expr, activeTx)
				}
				for _, bodyStmt := range cc.Body {
					scanStmt(bodyStmt, activeTx)
				}
			}
		case *ast.TypeSwitchStmt:
			scanCallsInNode(s.Init, activeTx)
			scanCallsInNode(s.Assign, activeTx)
			for _, clause := range s.Body.List {
				cc, ok := clause.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, bodyStmt := range cc.Body {
					scanStmt(bodyStmt, activeTx)
				}
			}
		case *ast.SelectStmt:
			for _, clause := range s.Body.List {
				cc, ok := clause.(*ast.CommClause)
				if !ok {
					continue
				}
				scanCallsInNode(cc.Comm, activeTx)
				for _, bodyStmt := range cc.Body {
					scanStmt(bodyStmt, activeTx)
				}
			}
		case *ast.LabeledStmt:
			scanStmt(s.Stmt, activeTx)
		default:
			scanCallsInNode(s, activeTx)
		}
	}

	scanBlock(body, copyNameSet(txParamNames))
}

func copyNameSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for name := range in {
		out[name] = struct{}{}
	}
	return out
}

func txNilCompare(expr ast.Expr, txParamNames map[string]struct{}) (txName string, op token.Token, ok bool) {
	bin, ok := stripParens(expr).(*ast.BinaryExpr)
	if !ok {
		return "", 0, false
	}
	if bin.Op != token.EQL && bin.Op != token.NEQ {
		return "", 0, false
	}

	left := stripParens(bin.X)
	right := stripParens(bin.Y)

	leftName, leftIsTx := txParamName(left, txParamNames)
	rightName, rightIsTx := txParamName(right, txParamNames)

	if leftIsTx && isNilIdent(right) {
		return leftName, bin.Op, true
	}
	if rightIsTx && isNilIdent(left) {
		return rightName, bin.Op, true
	}
	return "", 0, false
}

func stripParens(expr ast.Expr) ast.Expr {
	for {
		par, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = par.X
	}
}

func txParamName(expr ast.Expr, txParamNames map[string]struct{}) (string, bool) {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	_, ok = txParamNames[id.Name]
	return id.Name, ok
}

func isNilIdent(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "nil"
}

func findScanRoot(startDir string) (string, error) {
	dir := filepath.Clean(startDir)
	var nearestGit string
	var nearestGoWork string
	var nearestGoMod string

	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			nearestGit = dir
		}
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			nearestGoWork = dir
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			nearestGoMod = dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	switch {
	case nearestGit != "":
		return nearestGit, nil
	case nearestGoWork != "":
		return nearestGoWork, nil
	case nearestGoMod != "":
		return nearestGoMod, nil
	default:
		return "", os.ErrNotExist
	}
}
