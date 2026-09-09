package config

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/require"

	depsconfig "github.com/filecoin-project/curio/deps/config"

	"github.com/filecoin-project/lotus/chain/types"
)

// Resolve JSON pointers against the document root, not a nested $defs object.
// This is deliberately independent of the production mapper.
func schemaNode(root, node map[string]any) (map[string]any, error) {
	seen := map[string]bool{}
	for {
		ref, _ := node["$ref"].(string)
		if ref == "" {
			return node, nil
		}
		if !strings.HasPrefix(ref, "#/") || seen[ref] {
			return nil, fmt.Errorf("invalid or cyclic reference %q", ref)
		}
		seen[ref] = true
		var v any = root
		for _, p := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unresolved reference %q", ref)
			}
			v = m[strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")]
		}
		var ok bool
		node, ok = v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unresolved reference %q", ref)
		}
	}
}

func schemaMap(t *testing.T, s *jsonschema.Schema) map[string]any {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(b, &root))
	return root
}

func checkConfigSchema(root, node map[string]any, typ reflect.Type, path string) error {
	if inner, ok := depsconfig.DynamicInnerType(typ); ok {
		typ = inner
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	node, err := schemaNode(root, node)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	want := ""
	switch {
	case typ == reflect.TypeFor[time.Duration](), typ == reflect.TypeFor[types.FIL]():
		want = "string"
	case typ.Kind() == reflect.Struct:
		want = "object"
	case typ.Kind() == reflect.Map:
		want = "object"
	case typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array:
		want = "array"
	case typ.Kind() == reflect.Bool:
		want = "boolean"
	case typ.Kind() == reflect.String:
		want = "string"
	case typ.Kind() >= reflect.Int && typ.Kind() <= reflect.Uint64:
		want = "integer"
	case typ.Kind() == reflect.Float32 || typ.Kind() == reflect.Float64:
		want = "number"
	default:
		return fmt.Errorf("%s: unsupported model type %s requires explicit review", path, typ)
	}
	if node["type"] != want {
		return fmt.Errorf("%s: schema type %v, want %s for %s", path, node["type"], want, typ)
	}
	if want == "object" && typ.Kind() == reflect.Struct {
		props, _ := node["properties"].(map[string]any)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			name := field.Name
			if tag := strings.Split(field.Tag.Get("json"), ",")[0]; tag != "" {
				name = tag
			}
			if name == "-" || field.Tag.Get("toml") == "-" {
				return fmt.Errorf("%s.%s needs a documented exclusion", path, field.Name)
			}
			if field.Anonymous && field.Tag.Get("json") == "" {
				if err := checkConfigSchema(root, node, field.Type, path); err != nil {
					return err
				}
				continue
			}
			child, ok := props[name].(map[string]any)
			if !ok {
				return fmt.Errorf("%s.%s: missing schema property", path, name)
			}
			if err := checkConfigSchema(root, child, field.Type, path+"."+name); err != nil {
				return err
			}
		}
	}
	if want == "array" {
		child, _ := node["items"].(map[string]any)
		return checkConfigSchema(root, child, typ.Elem(), path+"[]")
	}
	if typ.Kind() == reflect.Map {
		child, _ := node["additionalProperties"].(map[string]any)
		return checkConfigSchema(root, child, typ.Elem(), path+".*")
	}
	return nil
}

func TestUIConfigModelCompleteness(t *testing.T) {
	// Exercise the actual GET handler, including serialization of $defs and $ref.
	rr := httptest.NewRecorder()
	getSch(rr, httptest.NewRequest("GET", "/api/config/schema", nil))
	require.Equal(t, 200, rr.Code)
	var root map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &root))
	require.NoError(t, checkConfigSchema(root, root, reflect.TypeOf(uiSchemaRoot()), "Configuration"))
}

type schemaNestedFixture struct {
	Delay   time.Duration
	Amount  types.FIL
	Enabled bool
}
type schemaDynamicFixture struct{ OnlyHere schemaNestedFixture }
type schemaContainerFixture struct {
	Named  *schemaNestedFixture
	Inline struct {
		Limit  int
		Values map[string][]*schemaNestedFixture
	}
	Dynamic  *depsconfig.Dynamic[[]schemaDynamicFixture]
	Duration *depsconfig.Dynamic[time.Duration]
	Enabled  *depsconfig.Dynamic[bool]
	Limit    *depsconfig.Dynamic[int]
}

func TestUIConfigSchemaTypeShapes(t *testing.T) {
	s := (&jsonschema.Reflector{Mapper: uiSchemaMapper}).Reflect(schemaContainerFixture{})
	root := schemaMap(t, s)
	require.NoError(t, checkConfigSchema(root, root, reflect.TypeFor[schemaContainerFixture](), "fixture"))
}

func TestUISchemaDocumentation(t *testing.T) {
	root := schemaMap(t, buildUISchema())
	var walk func(map[string]any, reflect.Type, string)
	walk = func(node map[string]any, typ reflect.Type, path string) {
		if inner, ok := depsconfig.DynamicInnerType(typ); ok {
			typ = inner
		}
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		resolved, err := schemaNode(root, node)
		require.NoError(t, err, path)
		if typ == reflect.TypeFor[types.FIL]() || typ == reflect.TypeFor[time.Duration]() {
			return
		}
		switch typ.Kind() {
		case reflect.Struct:
			props, _ := resolved["properties"].(map[string]any)
			for _, doc := range depsconfig.Doc[typ.Name()] {
				if doc.Comment == "" {
					continue
				}
				p, ok := props[doc.Name].(map[string]any)
				require.True(t, ok, path+"."+doc.Name)
				require.Equal(t, doc.Comment, p["description"], path+"."+doc.Name)
			}
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				if !f.IsExported() {
					continue
				}
				p, _ := props[f.Name].(map[string]any)
				walk(p, f.Type, path+"."+f.Name)
			}
		case reflect.Slice, reflect.Array:
			p, _ := resolved["items"].(map[string]any)
			walk(p, typ.Elem(), path+"[]")
		case reflect.Map:
			p, _ := resolved["additionalProperties"].(map[string]any)
			walk(p, typ.Elem(), path+".*")
		}
	}
	walk(root, reflect.TypeOf(uiSchemaRoot()), "Configuration")
}
