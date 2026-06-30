//go:build maxboom

package config

import (
	"github.com/filecoin-project/curio/deps"
	depsconfig "github.com/filecoin-project/curio/deps/config"
)

func uiSchemaRoot() any {
	return depsconfig.UnwrapDynamics(depsconfig.MaxBoomConfig{})
}

func uiDefaultJSON() (map[string]any, error) {
	return structToJSONMap(depsconfig.DefaultMaxBoomUIConfig())
}

func uiLayerJSON(layerToml string) (map[string]any, error) {
	curioCfg := depsconfig.DefaultCurioConfig()
	if layerToml != "" {
		if _, err := deps.LoadConfigWithUpgrades(layerToml, curioCfg); err != nil {
			return nil, err
		}
	}
	return structToJSONMap(depsconfig.MaxBoomConfigFromCurio(curioCfg))
}

func uiPrepareLayerSave(layer string, submitted map[string]any, existingToml string) (string, error) {
	curioCfg := depsconfig.DefaultCurioConfig()
	if existingToml != "" {
		if _, err := deps.LoadConfigWithUpgrades(existingToml, curioCfg); err != nil {
			return "", err
		}
	}

	maxboomCfg := depsconfig.DefaultMaxBoomUIConfig()
	if err := jsonRoundTrip(submitted, maxboomCfg); err != nil {
		return "", err
	}
	depsconfig.ApplyMaxBoomConfigToCurio(curioCfg, maxboomCfg)

	if _, err := deps.LoadConfigWithUpgrades(mustEncodeTOML(curioCfg), curioCfg); err != nil {
		return "", err
	}

	return formatLayerTOML(layer, curioCfg)
}
