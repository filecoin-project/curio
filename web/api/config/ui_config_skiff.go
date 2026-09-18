//go:build skiff

package config

import (
	"bytes"

	"github.com/BurntSushi/toml"

	"github.com/filecoin-project/curio/deps"
	depsconfig "github.com/filecoin-project/curio/deps/config"
)

func structToJSONMap(v any) (map[string]any, error) {
	return tomlToJSONMap(mustEncodeTOML(v))
}

func uiSchemaRoot() any {
	return depsconfig.SkiffConfig{}
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

	var submittedToml bytes.Buffer
	if err := toml.NewEncoder(&submittedToml).Encode(submitted); err != nil {
		return "", err
	}
	skiffCfg := depsconfig.DefaultSkiffUIConfig()
	if _, err := depsconfig.TransparentDecode(submittedToml.String(), skiffCfg); err != nil {
		return "", err
	}
	depsconfig.ApplySkiffConfigToCurio(curioCfg, skiffCfg)

	if _, err := deps.LoadConfigWithUpgrades(mustEncodeTOML(curioCfg), curioCfg); err != nil {
		return "", err
	}

	return formatLayerTOML(curioCfg)
}
