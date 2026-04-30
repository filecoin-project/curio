package harmonytask_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/harmonytask/treehelper"

	// Side-effect imports: harmonytask.Reg in each package fills Registry for these tests.
	_ "github.com/filecoin-project/curio/alertmanager"
	_ "github.com/filecoin-project/curio/tasks/balancemgr"
	_ "github.com/filecoin-project/curio/tasks/expmgr"
	_ "github.com/filecoin-project/curio/tasks/f3"
	_ "github.com/filecoin-project/curio/tasks/gc"
	_ "github.com/filecoin-project/curio/tasks/indexing"
	_ "github.com/filecoin-project/curio/tasks/message"
	_ "github.com/filecoin-project/curio/tasks/metadata"
	_ "github.com/filecoin-project/curio/tasks/pay"
	_ "github.com/filecoin-project/curio/tasks/pdp"
	_ "github.com/filecoin-project/curio/tasks/pdpv0"
	_ "github.com/filecoin-project/curio/tasks/piece"
	_ "github.com/filecoin-project/curio/tasks/proofshare"
	_ "github.com/filecoin-project/curio/tasks/scrub"
	_ "github.com/filecoin-project/curio/tasks/seal"
	_ "github.com/filecoin-project/curio/tasks/sealsupra"
	_ "github.com/filecoin-project/curio/tasks/snap"
	_ "github.com/filecoin-project/curio/tasks/storage-market"
	_ "github.com/filecoin-project/curio/tasks/unseal"
	_ "github.com/filecoin-project/curio/tasks/window"
	_ "github.com/filecoin-project/curio/tasks/winning"
)

const curioModulePrefix = "github.com/filecoin-project/curio/"

// curioRepoRoot finds the repository root (directory containing go.mod).
func curioRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(file)
	for {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "go.mod not found above %s", file)
		dir = parent
	}
}

// Harmony task Do methods use this signature; scanning for it finds concrete task types
// without relying on a separate var _ harmonytask.TaskInterface line. (?s) allows the
// receiver and parameter list to span lines (gofmt / manual wrapping).
var reTaskDo = regexp.MustCompile(`(?s)func\s*\(\s*[a-zA-Z_][a-zA-Z0-9_]*\s*\*([A-Za-z0-9_]+)\s*\)\s*Do\s*\(\s*ctx\s+context\.Context\s*,\s*taskID\s+harmonytask\.TaskID`)

// taskDoImpl is a registry-style key (module-relative dir + "/" + concrete type name)
// and a source file path for failure messages.
type taskDoImpl struct {
	key    string
	source string
}

func collectTaskDoImplsFromRepo(t *testing.T, repoRoot string) []taskDoImpl {
	t.Helper()
	var out []taskDoImpl
	for _, root := range []string{"tasks", "alertmanager"} {
		dir := filepath.Join(repoRoot, root)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			relSlash := filepath.ToSlash(rel)
			dirKey := filepath.ToSlash(filepath.Dir(relSlash))
			for _, sm := range reTaskDo.FindAllStringSubmatch(string(b), -1) {
				if len(sm) < 2 {
					continue
				}
				out = append(out, taskDoImpl{
					key:    dirKey + "/" + sm[1],
					source: relSlash,
				})
			}
			return nil
		})
		require.NoError(t, err)
	}
	return out
}

func registryPkgRelTypeKeys() map[string]struct{} {
	out := make(map[string]struct{})
	for _, task := range harmonytask.Registry {
		if task == nil {
			continue
		}
		rt := reflect.TypeOf(task)
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		pp := rt.PkgPath()
		if !strings.HasPrefix(pp, curioModulePrefix) {
			continue
		}
		rel := strings.TrimPrefix(pp, curioModulePrefix)
		out[rel+"/"+rt.Name()] = struct{}{}
	}
	return out
}

func TestCurioRegistryMayFollowAcyclic(t *testing.T) {
	mayFollows := make(map[string][]string)
	for regKey, task := range harmonytask.Registry {
		if task == nil {
			continue
		}
		td := task.TypeDetails()
		require.NotEmpty(t, td.Name, "registry key %q: empty TypeDetails().Name", regKey)
		if prev, exists := mayFollows[td.Name]; exists {
			require.Equal(t, prev, td.MayFollow, "duplicate task name %q with different MayFollow", td.Name)
		}
		mayFollows[td.Name] = append([]string(nil), td.MayFollow...)
	}

	require.NotEmpty(t, mayFollows)

	require.NotPanics(t, func() {
		treehelper.AssertMayFollowAcyclic(mayFollows)
	})
	pref := treehelper.FindPreferredTaskRunOrder(mayFollows)
	require.Equal(t, len(mayFollows), len(pref))
}

func TestCurioTaskDoImplementationsRegistered(t *testing.T) {
	repoRoot := curioRepoRoot(t)

	impls := collectTaskDoImplsFromRepo(t, repoRoot)
	require.NotEmpty(t, impls, "expected sources under tasks/ and alertmanager/ to define harmonytask Do(ctx,...) methods")

	regKeys := registryPkgRelTypeKeys()
	var missing []string
	seen := make(map[string]struct{})

	for _, impl := range impls {
		if _, dup := seen[impl.key]; dup {
			continue
		}
		seen[impl.key] = struct{}{}
		if _, ok := regKeys[impl.key]; !ok {
			missing = append(missing, impl.key+"\t"+impl.source)
		}
	}

	require.Empty(t, missing,
		"every harmonytask Do() implementation under tasks/ and alertmanager/ must be registered (harmonytask.Reg); missing rel/type:\n%s",
		strings.Join(missing, "\n"))
}
