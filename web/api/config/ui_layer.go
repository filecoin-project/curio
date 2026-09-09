package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	depsconfig "github.com/filecoin-project/curio/deps/config"
)

// editableCurioLayer validates with the runtime decoder but retains sparse
// layer values. Truly unknown fields stop editing instead of being dropped by
// a typed decode. This neither changes runtime loading nor repairs stored data.
func editableCurioLayer(text string) (map[string]any, error) {
	cfg := depsconfig.DefaultCurioConfig()
	md, err := depsconfig.LoadConfigWithUpgrades(text, cfg)
	if err != nil {
		return nil, err
	}
	unknown := md.Undecoded()
	if len(unknown) > 0 {
		names := make([]string, len(unknown))
		for i, k := range unknown {
			names[i] = k.String()
		}
		sort.Strings(names)
		return nil, fmt.Errorf("configuration contains unsupported fields (%s); no changes saved; use a compatible editor or explicitly review the layer", strings.Join(names, ", "))
	}
	raw, err := tomlToJSONMap(text)
	if err != nil {
		return nil, err
	}
	v, err := canonicalLayerKeys(raw, reflect.TypeFor[depsconfig.CurioConfig]())
	if err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}

// TOML matches Go fields case-insensitively; JSON Editor does not. Normalize
// supported key names only, preserving literal map keys and scalar values.
func canonicalLayerKeys(value any, typ reflect.Type) (any, error) {
	if inner, ok := depsconfig.DynamicInnerType(typ); ok {
		typ = inner
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return value, nil
		}
		out := make(map[string]any, len(object))
		for key, v := range object {
			field, ok := typ.FieldByNameFunc(func(name string) bool { return strings.EqualFold(key, name) })
			if !ok {
				return nil, fmt.Errorf("unsupported configuration field %s", key)
			}
			if _, exists := out[field.Name]; exists {
				return nil, fmt.Errorf("duplicate configuration field %s", field.Name)
			}
			normalized, err := canonicalLayerKeys(v, field.Type)
			if err != nil {
				return nil, err
			}
			out[field.Name] = normalized
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		// LoadConfigWithUpgrades supports the legacy single [addresses] table.
		if typ.Elem() == reflect.TypeFor[depsconfig.CurioAddresses]() {
			if object, ok := value.(map[string]any); ok {
				value = []any{object}
			}
		}
		rv := reflect.ValueOf(value)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return value, nil
		}
		out := make([]any, rv.Len())
		for i := range out {
			v, err := canonicalLayerKeys(rv.Index(i).Interface(), typ.Elem())
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return value, nil
		}
		out := make(map[string]any, len(object))
		for key, v := range object {
			n, err := canonicalLayerKeys(v, typ.Elem())
			if err != nil {
				return nil, err
			}
			out[key] = n
		}
		return out, nil
	default:
		return value, nil
	}
}
