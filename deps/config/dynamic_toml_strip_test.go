package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripEmptyDynamicTables(t *testing.T) {
	const broken = `
# keep this comment
[Subsystems]
EnablePDP = true

[Subsystems.PDPUnclaimedUploadKeepHours]

[Ingest]
MaxQueueDownload = 16

[Ingest.MaxMarketRunningPipelines]
`

	cfg := DefaultCurioConfig()
	md, err := LoadConfigWithUpgrades(broken, cfg)
	require.NoError(t, err)

	assert.True(t, cfg.Subsystems.EnablePDP)
	assert.Equal(t, 2, cfg.Subsystems.PDPUnclaimedUploadKeepHours.Get())
	assert.Equal(t, 16, cfg.Ingest.MaxQueueDownload.Get())
	assert.Equal(t, 64, cfg.Ingest.MaxMarketRunningPipelines.Get())
	assert.False(t, md.IsDefined("Subsystems", "PDPUnclaimedUploadKeepHours"))
	assert.False(t, md.IsDefined("Ingest", "MaxMarketRunningPipelines"))
}

func TestRepairEmptyDynamicTablesTextPreservesComments(t *testing.T) {
	const broken = `
# layer header
[Subsystems]
EnablePDP = true

# should be removed below
[Subsystems.PDPUnclaimedUploadKeepHours]

[Ingest]
MaxQueueDownload = 16
`

	fixed, changed, err := RepairEmptyDynamicTablesText(broken, DefaultCurioConfig())
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Contains(t, fixed, "# layer header")
	assert.Contains(t, fixed, "EnablePDP = true")
	assert.Contains(t, fixed, "MaxQueueDownload = 16")
	assert.NotContains(t, fixed, "[Subsystems.PDPUnclaimedUploadKeepHours]")
	// Comment lines above the dropped header are preserved.
	assert.Contains(t, fixed, "# should be removed below")
	cfg := DefaultCurioConfig()
	_, err = LoadConfigWithUpgrades(fixed, cfg)
	require.NoError(t, err)
	assert.True(t, cfg.Subsystems.EnablePDP)
	assert.Equal(t, 16, cfg.Ingest.MaxQueueDownload.Get())
}

func TestRepairEmptyDynamicTablesTextNoop(t *testing.T) {
	const ok = `
[Subsystems]
PDPUnclaimedUploadKeepHours = 7
`
	fixed, changed, err := RepairEmptyDynamicTablesText(ok, DefaultCurioConfig())
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, ok, fixed)
}

func TestStripEmptyDynamicTablesLeavesScalars(t *testing.T) {
	const ok = `
[Subsystems]
PDPUnclaimedUploadKeepHours = 7
`

	text, err := StripEmptyDynamicTables(ok, DefaultCurioConfig())
	require.NoError(t, err)
	assert.Equal(t, ok, text)

	cfg := DefaultCurioConfig()
	_, err = LoadConfigWithUpgrades(ok, cfg)
	require.NoError(t, err)
	assert.Equal(t, 7, cfg.Subsystems.PDPUnclaimedUploadKeepHours.Get())
}

func TestStripEmptyDynamicTablesAddressesWrapper(t *testing.T) {
	const broken = `
[Addresses]
`

	cfg := DefaultCurioConfig()
	before := len(cfg.Addresses.Get())
	_, err := LoadConfigWithUpgrades(broken, cfg)
	require.NoError(t, err)
	assert.Equal(t, before, len(cfg.Addresses.Get()))
}

func TestRawEncodeDynamicIsEmptyTable(t *testing.T) {
	var buf strings.Builder
	err := toml.NewEncoder(&buf).Encode(DefaultCurioConfig())
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "[Subsystems.PDPUnclaimedUploadKeepHours]")

	cfg := DefaultCurioConfig()
	_, err = LoadConfigWithUpgrades(buf.String(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.Subsystems.PDPUnclaimedUploadKeepHours.Get())
}
