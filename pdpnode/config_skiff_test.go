//go:build skiff

package pdpnode

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps/config"
)

func TestDefaultSkiffBaseConfig(t *testing.T) {
	cfg := defaultSkiffBaseConfig()
	require.True(t, cfg.Subsystems.EnablePDP)
	require.True(t, cfg.Subsystems.EnableWebGui)
	require.Equal(t, "127.0.0.1:4701", cfg.Subsystems.GuiAddress)
	require.False(t, cfg.HTTP.Enable)
	require.NotEmpty(t, cfg.Apis.StorageRPCSecret)
}

func TestApplySkiffDefaults(t *testing.T) {
	cfg := config.DefaultCurioConfig()
	applySkiffDefaults(cfg)
	require.True(t, cfg.Subsystems.EnablePDP)
	require.True(t, cfg.Subsystems.EnableWebGui)
	require.Equal(t, "127.0.0.1:4701", cfg.Subsystems.GuiAddress)
	require.NotEmpty(t, cfg.Apis.StorageRPCSecret)
}

func TestMergeConfigLayers(t *testing.T) {
	base := `
[Subsystems]
  EnableWebGui = true
`
	overlay := `
[HTTP]
  Enable = true
  DomainName = "pdp.example.com"
  ListenAddress = "0.0.0.0:443"
`
	merged, err := mergeConfigLayers(base, overlay)
	require.NoError(t, err)
	require.True(t, merged.HTTP.Enable)
	require.Equal(t, "pdp.example.com", merged.HTTP.DomainName)
	require.True(t, merged.Subsystems.EnablePDP)
}
