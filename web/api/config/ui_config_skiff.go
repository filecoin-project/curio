//go:build skiff

package config

import (
	"bytes"

	"github.com/BurntSushi/toml"

	"github.com/filecoin-project/curio/deps"
	depsconfig "github.com/filecoin-project/curio/deps/config"
)

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
	if err := decodeJSONMap(submitted, skiffCfg); err != nil {
		return "", err
	}
	depsconfig.ApplySkiffConfigToCurio(curioCfg, skiffCfg)

	if _, err := deps.LoadConfigWithUpgrades(mustEncodeTOML(curioCfg), curioCfg); err != nil {
		return "", err
	}

	return formatLayerTOML(layer, curioCfg)
}

func decodeJSONMap(m map[string]any, dst any) error {
	b, err := jsonMap(m)
	if err != nil {
		return err
	}
	return jsonUnmarshal(b, dst)
}

func mustEncodeTOML(cfg *depsconfig.CurioConfig) string {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		panic(err)
	}
	return buf.String()
}
