//go:build skiff

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

func TestResolveSkiffDataPath(t *testing.T) {
	app := &cli.App{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "data", EnvVars: []string{"DATA_STORAGE", "SKIFF_DATA", "CURIO_DATA"}},
		},
	}

	cfg := config.DefaultCurioConfig()
	cfg.Subsystems.DataPath = "/from-config"

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("data", "/data", "")
	require.NoError(t, set.Parse([]string{}))
	c := cli.NewContext(app, set, nil)

	t.Setenv("SKIFF_DATA", "")
	t.Setenv("CURIO_DATA", "")
	t.Setenv("DATA_STORAGE", "")
	require.Equal(t, "/from-config", resolveSkiffDataPath(c, cfg))

	t.Setenv("DATA_STORAGE", "/from-env")
	require.Equal(t, "/from-env", resolveSkiffDataPath(c, cfg))

	setCLI := flag.NewFlagSet("test", flag.ContinueOnError)
	setCLI.String("data", "/data", "")
	require.NoError(t, setCLI.Parse([]string{"--data", "/from-cli"}))
	cCLI := cli.NewContext(app, setCLI, nil)
	t.Setenv("DATA_STORAGE", "/from-env")
	require.Equal(t, "/from-cli", resolveSkiffDataPath(cCLI, cfg))

	t.Setenv("SKIFF_DATA", "")
	t.Setenv("CURIO_DATA", "")
	t.Setenv("DATA_STORAGE", "")
	require.Equal(t, defaultSkiffDataPath, resolveSkiffDataPath(c, nil))
}

func TestPDPStorageConfigFromDataRoot(t *testing.T) {
	root := t.TempDir()
	writable := filepath.Join(root, "store")
	require.NoError(t, os.MkdirAll(writable, 0o755))

	cfg, err := pdpStorageConfig(root)
	require.NoError(t, err)
	require.Len(t, cfg.StoragePaths, 2)

	metaPath := filepath.Join(writable, "sectorstore.json")
	require.FileExists(t, metaPath)
}
