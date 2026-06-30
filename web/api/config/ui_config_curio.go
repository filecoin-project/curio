//go:build !skiff

package config

import (
	depsconfig "github.com/filecoin-project/curio/deps/config"
)

func uiSchemaRoot() any {
	return depsconfig.UnwrapDynamics(depsconfig.CurioConfig{})
}

func uiDefaultJSON() (map[string]any, error) {
	return configToJSONMap(depsconfig.DefaultCurioConfig())
}

func uiLayerJSON(layerToml string) (map[string]any, error) {
	return tomlToJSONMap(layerToml)
}

func uiPrepareLayerSave(layer string, submitted map[string]any, existingToml string) (string, error) {
	return prepareCurioLayerSave(layer, submitted)
}
