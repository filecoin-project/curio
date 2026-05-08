package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyLayersOrder verifies that config layers are applied in the order
// they are provided, so that later layers override earlier ones. This is a
// regression test for a bug where layers were sorted alphabetically before
// being applied, breaking the override invariant.
func TestApplyLayersOrder(t *testing.T) {
	type Subsystems struct {
		EnableUpdateEncode bool
		EnableWindowPost   bool
		MaxTasks           int
	}
	type TestCfg struct {
		Subsystems Subsystems
	}

	noopFixup := func(_ string, _ *TestCfg) error { return nil }

	t.Run("last layer wins for bool fields", func(t *testing.T) {
		cfg := &TestCfg{}

		// Layer "alpha" enables the field, layer "zeta" disables it.
		// Despite "alpha" sorting before "zeta", the user-specified order
		// must determine the outcome.
		layers := []ConfigText{
			{Title: "alpha", Config: `
[Subsystems]
EnableUpdateEncode = true
`},
			{Title: "zeta", Config: `
[Subsystems]
EnableUpdateEncode = false
`},
		}

		err := ApplyLayers(context.Background(), cfg, layers, noopFixup)
		require.NoError(t, err)
		assert.False(t, cfg.Subsystems.EnableUpdateEncode, "last layer (zeta) should win")
	})

	t.Run("last layer wins reverse order", func(t *testing.T) {
		cfg := &TestCfg{}

		// Now "zeta" comes first and "alpha" last — "alpha" should win.
		layers := []ConfigText{
			{Title: "zeta", Config: `
[Subsystems]
EnableUpdateEncode = false
`},
			{Title: "alpha", Config: `
[Subsystems]
EnableUpdateEncode = true
`},
		}

		err := ApplyLayers(context.Background(), cfg, layers, noopFixup)
		require.NoError(t, err)
		assert.True(t, cfg.Subsystems.EnableUpdateEncode, "last layer (alpha) should win")
	})

	t.Run("intermediate layer does not clobber later layer", func(t *testing.T) {
		cfg := &TestCfg{}

		// Three layers: base sets defaults, middle enables, last disables.
		layers := []ConfigText{
			{Title: "base", Config: `
[Subsystems]
MaxTasks = 10
`},
			{Title: "enable-all", Config: `
[Subsystems]
EnableUpdateEncode = true
EnableWindowPost = true
`},
			{Title: "disable-encode", Config: `
[Subsystems]
EnableUpdateEncode = false
`},
		}

		err := ApplyLayers(context.Background(), cfg, layers, noopFixup)
		require.NoError(t, err)
		assert.False(t, cfg.Subsystems.EnableUpdateEncode, "last layer should disable encode")
		assert.True(t, cfg.Subsystems.EnableWindowPost, "window post from middle layer should survive")
		assert.Equal(t, 10, cfg.Subsystems.MaxTasks, "base layer int value should survive")
	})

	t.Run("layer omitting field preserves earlier value", func(t *testing.T) {
		cfg := &TestCfg{}

		layers := []ConfigText{
			{Title: "base", Config: `
[Subsystems]
EnableUpdateEncode = true
MaxTasks = 5
`},
			{Title: "overlay", Config: `
[Subsystems]
MaxTasks = 20
`},
		}

		err := ApplyLayers(context.Background(), cfg, layers, noopFixup)
		require.NoError(t, err)
		assert.True(t, cfg.Subsystems.EnableUpdateEncode, "field not in overlay should keep base value")
		assert.Equal(t, 20, cfg.Subsystems.MaxTasks, "overlay should override MaxTasks")
	})

	t.Run("many layers applied in sequence", func(t *testing.T) {
		cfg := &TestCfg{}

		// Names are deliberately NOT in alphabetical order to catch sorting bugs.
		layers := []ConfigText{
			{Title: "base", Config: `
[Subsystems]
MaxTasks = 1
`},
			{Title: "gpu", Config: `
[Subsystems]
MaxTasks = 4
`},
			{Title: "seal-config", Config: ``},
			{Title: "seal-params", Config: ``},
			{Title: "storage", Config: `
[Subsystems]
EnableUpdateEncode = false
`},
			{Title: "post", Config: `
[Subsystems]
EnableUpdateEncode = true
`},
		}

		err := ApplyLayers(context.Background(), cfg, layers, noopFixup)
		require.NoError(t, err)
		assert.True(t, cfg.Subsystems.EnableUpdateEncode, "post (last) should win over storage")
		assert.Equal(t, 4, cfg.Subsystems.MaxTasks, "gpu layer value should persist")
	})
}
