package config

import (
	"bytes"

	"github.com/BurntSushi/toml"

	"github.com/filecoin-project/curio/deps"
	depsconfig "github.com/filecoin-project/curio/deps/config"
)

func configToJSONMap(v any) (map[string]any, error) {
	cb, err := depsconfig.ConfigUpdate(v, depsconfig.DefaultCurioConfig(), depsconfig.Commented(false), depsconfig.DefaultKeepUncommented(), depsconfig.NoEnv())
	if err != nil {
		return nil, err
	}
	return tomlToJSONMap(string(cb))
}

func tomlToJSONMap(layerToml string) (map[string]any, error) {
	configStruct := map[string]any{}
	if layerToml != "" {
		if _, err := toml.Decode(layerToml, &configStruct); err != nil {
			return nil, err
		}
	}
	return configStruct, nil
}

func prepareCurioLayerSave(layer string, configStruct map[string]any) (string, error) {
	var tomlData bytes.Buffer
	if err := toml.NewEncoder(&tomlData).Encode(configStruct); err != nil {
		return "", err
	}

	curioCfg := depsconfig.DefaultCurioConfig()
	if _, err := deps.LoadConfigWithUpgrades(tomlData.String(), curioCfg); err != nil {
		return "", err
	}

	return formatLayerTOML(layer, curioCfg)
}

func formatLayerTOML(layer string, curioCfg *depsconfig.CurioConfig) (string, error) {
	cb, err := depsconfig.ConfigUpdate(curioCfg, depsconfig.DefaultCurioConfig(), depsconfig.Commented(true), depsconfig.DefaultKeepUncommented(), depsconfig.NoEnv())
	if err != nil {
		return "", err
	}

	configStr := mustEncodeTOML(curioCfg)
	if layer == "base" {
		configStr = string(cb)
	}
	return configStr, nil
}

// mustEncodeTOML serialises v to TOML or panics.
func mustEncodeTOML(v any) string {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(v); err != nil {
		panic(err)
	}
	return buf.String()
}
