//go:build maxboom

package pdpnode

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/deps/config"
)

func TestResolveMaxBoomDataPath(t *testing.T) {
	app := &cli.App{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "data", EnvVars: []string{"DATA_STORAGE", "MAXBOOM_DATA", "CURIO_DATA"}},
		},
	}

	cfg := config.DefaultCurioConfig()
	cfg.Subsystems.DataPath = "/from-config"

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("data", "/data", "")
	require.NoError(t, set.Parse([]string{}))
	c := cli.NewContext(app, set, nil)

	t.Setenv("MAXBOOM_DATA", "")
	t.Setenv("CURIO_DATA", "")
	t.Setenv("DATA_STORAGE", "")
	require.Equal(t, "/from-config", resolveMaxBoomDataPath(c, cfg))

	t.Setenv("DATA_STORAGE", "/from-env")
	require.Equal(t, "/from-env", resolveMaxBoomDataPath(c, cfg))

	setCLI := flag.NewFlagSet("test", flag.ContinueOnError)
	setCLI.String("data", "/data", "")
	require.NoError(t, setCLI.Parse([]string{"--data", "/from-cli"}))
	cCLI := cli.NewContext(app, setCLI, nil)
	t.Setenv("DATA_STORAGE", "/from-env")
	require.Equal(t, "/from-cli", resolveMaxBoomDataPath(cCLI, cfg))

	t.Setenv("MAXBOOM_DATA", "")
	t.Setenv("CURIO_DATA", "")
	t.Setenv("DATA_STORAGE", "")
	require.Equal(t, defaultMaxBoomDataPath, resolveMaxBoomDataPath(c, nil))
}

func TestPDPStorageConfigFromDataRoot(t *testing.T) {
	root := t.TempDir()
	hot := filepath.Join(root, maxboomHotDataDirName)
	require.NoError(t, os.MkdirAll(hot, 0o755))

	cfg, err := pdpStorageConfig(root)
	require.NoError(t, err)
	require.Len(t, cfg.StoragePaths, 2)

	metaPath := filepath.Join(hot, "sectorstore.json")
	require.FileExists(t, metaPath)
}
