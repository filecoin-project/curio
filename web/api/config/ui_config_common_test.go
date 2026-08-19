package config

import (
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	depsconfig "github.com/filecoin-project/curio/deps/config"
)

func TestSchemaDynamicFieldsUseInnerType(t *testing.T) {
	ref := jsonschema.Reflector{Mapper: uiSchemaMapper}
	sch := ref.Reflect(uiSchemaRoot())

	_, isWrapper := sch.Definitions["Dynamic[int]"]
	assert.False(t, isWrapper, "Dynamic[int] must not appear as a schema object")

	sub, ok := sch.Definitions["CurioSubsystemsConfig"]
	require.True(t, ok, "named Subsystems type should remain in definitions")
	prop, ok := sub.Properties.Get("PDPUnclaimedUploadKeepHours")
	require.True(t, ok)
	assert.Equal(t, "integer", prop.Type)
	assert.Empty(t, prop.Ref)

	ingest, ok := sch.Definitions["CurioIngestConfig"]
	require.True(t, ok)
	timeout, ok := ingest.Properties.Get("MaxDealWaitTime")
	require.True(t, ok)
	assert.Equal(t, "string", timeout.Type)
}

func TestFormatLayerTOMLDoesNotEmitDynamicTables(t *testing.T) {
	out, err := formatLayerTOML(depsconfig.DefaultCurioConfig())
	require.NoError(t, err)
	assert.Contains(t, out, "PDPUnclaimedUploadKeepHours")
	assert.NotContains(t, out, "[Subsystems.PDPUnclaimedUploadKeepHours]")
}

func TestTomlToJSONMapStripsEmptyDynamicTables(t *testing.T) {
	const broken = `
[Subsystems]
EnablePDP = true

[Subsystems.PDPUnclaimedUploadKeepHours]
`

	m, err := tomlToJSONMap(broken)
	require.NoError(t, err)

	sub, ok := m["Subsystems"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, sub["EnablePDP"])
	_, present := sub["PDPUnclaimedUploadKeepHours"]
	assert.False(t, present)
}

func TestMustEncodeTOMLUnwrapsDynamics(t *testing.T) {
	out := mustEncodeTOML(depsconfig.DefaultCurioConfig())
	assert.Contains(t, out, "PDPUnclaimedUploadKeepHours = 2")
	assert.NotContains(t, out, "[Subsystems.PDPUnclaimedUploadKeepHours]")
}

func TestPrepareCurioLayerSaveRepairsEmptyDynamicTables(t *testing.T) {
	submitted := map[string]any{
		"Subsystems": map[string]any{
			"EnablePDP":                   true,
			"PDPUnclaimedUploadKeepHours": map[string]any{},
		},
	}

	out, err := prepareCurioLayerSave("pdp", submitted)
	require.NoError(t, err)
	assert.NotContains(t, out, "[Subsystems.PDPUnclaimedUploadKeepHours]")
	assert.Contains(t, out, "EnablePDP = true")
}
