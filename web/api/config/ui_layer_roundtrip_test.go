//go:build !skiff

package config

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps"
	depsconfig "github.com/filecoin-project/curio/deps/config"
)

func editorGenericJSON(t *testing.T, layer string) map[string]any {
	t.Helper()
	m, err := uiLayerJSON(layer)
	require.NoError(t, err)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	var submitted map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	require.NoError(t, d.Decode(&submitted))
	return submitted
}

func TestUIGenericLayerConfigRoundTrip(t *testing.T) {
	const layer = `[Subsystems]
EnableSealSDR = true
SealSDRMaxTasks = 4
[Ingest]
MaxQueueSDR = 0
MaxQueueTrees = 9
MaxQueuePoRep = 11
MaxQueueDealSector = 12
MaxQueueDownload = 13
MaxQueueCommP = 14
MaxMarketRunningPipelines = 15
MaxQueueSnapEncode = 16
MaxQueueSnapProve = 18
MaxDealWaitTime = "2h3m"
`
	m := editorGenericJSON(t, layer)
	for _, edit := range []bool{false, true} {
		if edit {
			m["Subsystems"].(map[string]any)["SealSDRMaxTasks"] = json.Number("8")
		}
		out, err := uiPrepareLayerSave("synthetic", m, layer)
		require.NoError(t, err)
		cfg := depsconfig.DefaultCurioConfig()
		_, err = depsconfig.LoadConfigWithUpgrades(out, cfg)
		require.NoError(t, err)
		require.True(t, cfg.Subsystems.EnableSealSDR)
		require.Equal(t, 0, cfg.Ingest.MaxQueueSDR.Get())
		require.Equal(t, 2*time.Hour+3*time.Minute, cfg.Ingest.MaxDealWaitTime.Get())
		require.Equal(t, m, editorGenericJSON(t, out), "every explicit value survives without synthesizing defaults")
	}
}

func TestUIGenericLayerPreservesExplicitDefaults(t *testing.T) {
	const layer = `[Subsystems]
EnableSealSDR = false
SealSDRMaxTasks = 0
[Ingest]
MaxQueueSDR = 8
`
	m := editorGenericJSON(t, layer)
	out, err := uiPrepareLayerSave("override", m, layer)
	require.NoError(t, err)
	require.Equal(t, m, editorGenericJSON(t, out), "a layer default is still an override of earlier layers")
}

func TestUIGenericLayerUnknownKeysFailClosed(t *testing.T) {
	const layer = "[Subsystems]\nEnableSealSDR=true\nFutureUnsupportedOption=23\n"
	_, err := uiLayerJSON(layer)
	require.Error(t, err)
	m := map[string]any{"Subsystems": map[string]any{"EnableSealSDR": true, "FutureUnsupportedOption": json.Number("23")}}
	_, err = uiPrepareLayerSave("unknown", m, layer)
	require.Error(t, err)
	delete(m["Subsystems"].(map[string]any), "FutureUnsupportedOption")
	_, err = uiPrepareLayerSave("unknown", m, layer)
	require.Error(t, err, "editor omission must not silently destroy an existing unknown key")
}

func TestUIGenericLayerSchemaSemantics(t *testing.T) {
	root := schemaMap(t, buildUISchema())
	node, err := schemaNode(root, root)
	require.NoError(t, err)
	for path, want := range map[string]string{
		"Subsystems.EnableSealSDR":   "boolean",
		"Subsystems.SealSDRMaxTasks": "integer",
		"Ingest.MaxQueueSDR":         "integer",
		"Ingest.MaxDealWaitTime":     "string",
		"Fees.MaxWindowPoStGasFee":   "string",
	} {
		current := node
		for _, key := range strings.Split(path, ".") {
			props := current["properties"].(map[string]any)
			p, ok := props[key].(map[string]any)
			require.True(t, ok, path)
			current, err = schemaNode(root, p)
			require.NoError(t, err, path)
		}
		require.Equal(t, want, current["type"], path)
	}
}

func TestUIGenericLayerCanonicalKeysAndNestedValues(t *testing.T) {
	const layer = `[subsystems]
enablesealsdr = true
[market.storagemarketconfig.mk12]
publishmsgperiod = "1m"
[[addresses]]
mineraddresses = ["t01000"]
[addresses.balancemanager.mk12collateral]
collaterallowthreshold = "3 FIL"
collateralhighthreshold = "7 FIL"
[[market.storagemarketconfig.piecelocator]]
URL = "https://example.invalid"
[market.storagemarketconfig.piecelocator.Headers]
X-Custom = ["", "value"]
`
	m := editorGenericJSON(t, layer)
	require.Equal(t, true, m["Subsystems"].(map[string]any)["EnableSealSDR"])
	out, err := uiPrepareLayerSave("synthetic", m, layer)
	require.NoError(t, err)
	require.Equal(t, m, editorGenericJSON(t, out))
	cfg := depsconfig.DefaultCurioConfig()
	_, err = depsconfig.LoadConfigWithUpgrades(out, cfg)
	require.NoError(t, err)
	require.Equal(t, "3 FIL", cfg.Addresses.Get()[0].BalanceManager.MK12Collateral.CollateralLowThreshold.String())
}

func TestUIGenericLayerDefaultOverridesEarlierLayer(t *testing.T) {
	const earlier = `[Subsystems]
EnableSealSDR=true
[Ingest]
MaxDealWaitTime="1h"
MaxQueueSDR=9
`
	const override = `[Subsystems]
EnableSealSDR=false
[Ingest]
MaxDealWaitTime="0s"
MaxQueueSDR=0
`
	out, err := uiPrepareLayerSave("override", editorGenericJSON(t, override), override)
	require.NoError(t, err)
	cfg := depsconfig.DefaultCurioConfig()
	_, err = depsconfig.LoadConfigWithUpgrades(earlier, cfg)
	require.NoError(t, err)
	_, err = depsconfig.LoadConfigWithUpgrades(out, cfg)
	require.NoError(t, err)
	require.False(t, cfg.Subsystems.EnableSealSDR)
	require.Zero(t, cfg.Ingest.MaxDealWaitTime.Get())
	require.Zero(t, cfg.Ingest.MaxQueueSDR.Get())
}

func TestUIGenericLayerLegacyAddressesAndInvalidValues(t *testing.T) {
	const legacy = "[addresses]\nMinerAddresses=[\"t01000\"]\n"
	m := editorGenericJSON(t, legacy)
	_, ok := m["Addresses"].([]any)
	require.True(t, ok, "legacy single address table must become a schema-compatible array")
	out, err := uiPrepareLayerSave("legacy", m, legacy)
	require.NoError(t, err)
	require.Equal(t, m, editorGenericJSON(t, out))
	for _, invalid := range []string{
		"[Ingest]\nMaxDealWaitTime=\"forever\"\n",
		"[Fees]\nMaxWindowPoStGasFee=\"not money\"\n",
		"[Subsystems]\nEnableSealSDR=\"true\"\n",
	} {
		_, err := uiLayerJSON(invalid)
		require.Error(t, err, "runtime decoder still validates typed values")
	}
}

func TestUIGenericDefaultConfigurationDocumentation(t *testing.T) {
	// Match the generator's source of truth, not a hand-maintained field list.
	defaults, err := deps.GetDefaultConfig(true)
	require.NoError(t, err)
	doc, err := os.ReadFile("../../../documentation/en/configuration/default-curio-configuration.md")
	require.NoError(t, err)
	want := "---\ndescription: The default curio configuration\n---\n\n# Default Curio Configuration\n\n```toml\n" + defaults + "```\n"
	require.Equal(t, want, string(doc), "regenerate with the config-default command used by docsgen-cli")
}
