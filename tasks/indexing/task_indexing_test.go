//go:build !skiff

package indexing

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexingAssignmentSQLRequiresSectorOffset(t *testing.T) {
	tests := []struct {
		name              string
		query             string
		candidateIdentity string
		updateIdentity    []string
	}{
		{
			name:              "MK12",
			query:             indexingMK12AssignSQL,
			candidateIdentity: "select uuid from market_mk12_deal_pipeline",
			updateIdentity: []string{
				"p.uuid = pending.uuid",
			},
		},
		{
			name:              "MK20",
			query:             indexingMK20AssignSQL,
			candidateIdentity: "select id, aggr_index from market_mk20_pipeline",
			updateIdentity: []string{
				"p.id = pending.id",
				"p.aggr_index = pending.aggr_index",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := normalizeIndexingSQL(test.query)
			candidate, update, found := strings.Cut(query, " update ")
			require.True(t, found, "assignment query must contain an UPDATE")

			require.Contains(t, candidate, test.candidateIdentity)
			require.Contains(t, candidate, "sector_offset is not null")
			require.Less(t,
				strings.Index(candidate, "sector_offset is not null"),
				strings.Index(candidate, "order by indexing_created_at asc limit 1"),
				"offset readiness must be applied before ordering and limiting candidates",
			)
			require.Contains(t, update, "p.sector_offset is not null")
			require.Equal(t, 2, strings.Count(query, "sector_offset is not null"),
				"both candidate selection and assignment must recheck offset readiness")

			for _, predicate := range []string{
				"sealed = true",
				"indexing_task_id is null",
				"indexed = false",
			} {
				require.Equal(t, 2, strings.Count(query, predicate),
					"candidate selection and assignment must retain %q", predicate)
			}
			require.Equal(t, 1, strings.Count(query, "indexing_created_at is not null"))
			require.Contains(t, query, "set indexing_task_id = $1")

			for _, identity := range test.updateIdentity {
				require.Contains(t, update, identity)
			}

			// A NULL check excludes unavailable offsets while allowing both zero and
			// positive offsets. Indexing remains scheduled for metadata-only rows.
			require.NotContains(t, query, "coalesce(sector_offset")
			require.NotRegexp(t, `sector_offset\s*(?:=|<>|!=|<|>)\s*[-+]?\d`, query)
			require.NotContains(t, query, "should_index = true")
		})
	}
}

func TestIndexingScheduleUsesGuardedAssignmentSQL(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	taskFile := filepath.Join(filepath.Dir(testFile), "task_indexing.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), taskFile, nil, 0)
	require.NoError(t, err)

	var schedule *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "schedule" {
			schedule = function
			break
		}
	}
	require.NotNil(t, schedule, "IndexingTask.schedule must exist")

	usedQueries := map[string]int{}
	ast.Inspect(schedule.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Exec" {
			return true
		}
		query, ok := call.Args[0].(*ast.Ident)
		if ok {
			usedQueries[query.Name]++
		}
		return true
	})

	require.Equal(t, 1, usedQueries["indexingMK12AssignSQL"])
	require.Equal(t, 1, usedQueries["indexingMK20AssignSQL"])
}

func normalizeIndexingSQL(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}
