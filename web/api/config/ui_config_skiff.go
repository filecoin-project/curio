//go:build skiff

package config

import (
	"encoding/json"

	"github.com/filecoin-project/curio/deps"
	depsconfig "github.com/filecoin-project/curio/deps/config"
)

func structToJSONMap(v any) (map[string]any, error) {
	return tomlToJSONMap(mustEncodeTOML(v))
}

// jsonRoundTrip marshals m to JSON and unmarshals the result into dst.
// Used to transfer a map[string]any into a typed struct via the JSON codec.
func jsonRoundTrip(m map[string]any, dst any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func uiSchemaRoot() any {
	return depsconfig.UnwrapDynamics(depsconfig.SkiffConfig{})
}

func uiDefaultJSON() (map[string]any, error) {
	return structToJSONMap(depsconfig.DefaultSkiffUIConfig())
}

func uiLayerJSON(layerToml string) (map[string]any, error) {
	curioCfg := depsconfig.DefaultCurioConfig()
	if layerToml != "" {
		if _, err := deps.LoadConfigWithUpgrades(layerToml, curioCfg); err != nil {
			return nil, err
		}
	}
	return structToJSONMap(depsconfig.SkiffConfigFromCurio(curioCfg))
}

func uiPrepareLayerSave(layer string, submitted map[string]any, existingToml string) (string, error) {
	curioCfg := depsconfig.DefaultCurioConfig()
	if existingToml != "" {
		if _, err := deps.LoadConfigWithUpgrades(existingToml, curioCfg); err != nil {
			return "", err
		}
	}

	skiffCfg := depsconfig.DefaultSkiffUIConfig()
	if err := jsonRoundTrip(submitted, skiffCfg); err != nil {
		return "", err
	}
	depsconfig.ApplySkiffConfigToCurio(curioCfg, skiffCfg)

	if _, err := deps.LoadConfigWithUpgrades(mustEncodeTOML(curioCfg), curioCfg); err != nil {
		return "", err
	}

	return formatLayerTOML(layer, curioCfg)
}
