package config

import (
	"bytes"

	"github.com/BurntSushi/toml"

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
		sanitized, err := depsconfig.StripEmptyDynamicTables(layerToml, depsconfig.DefaultCurioConfig())
		if err != nil {
			return nil, err
		}
		if _, err := toml.Decode(sanitized, &configStruct); err != nil {
			return nil, err
		}
	}
	return configStruct, nil
}

func prepareCurioLayerSave(_ string, configStruct map[string]any) (string, error) {
	var tomlData bytes.Buffer
	if err := toml.NewEncoder(&tomlData).Encode(configStruct); err != nil {
		return "", err
	}

	layer, err := editableCurioLayer(tomlData.String())
	if err != nil {
		return "", err
	}
	// A layer is a sparse set of explicit overrides, not a full effective config.
	// Comparing against defaults comments out intentional false/zero overrides
	// and can synthesize empty address entries. Keep exactly the submitted keys.
	tomlData.Reset()
	if err := toml.NewEncoder(&tomlData).Encode(layer); err != nil {
		return "", err
	}
	return tomlData.String(), nil
}

func formatLayerTOML(curioCfg *depsconfig.CurioConfig) (string, error) {
	cb, err := depsconfig.ConfigUpdate(curioCfg, depsconfig.DefaultCurioConfig(), depsconfig.Commented(true), depsconfig.DefaultKeepUncommented(), depsconfig.NoEnv())
	if err != nil {
		return "", err
	}
	return string(cb), nil
}

// mustEncodeTOML serialises v to TOML or panics.
func mustEncodeTOML(v any) string {
	data, err := depsconfig.TransparentMarshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}
