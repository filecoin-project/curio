package config

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gorilla/mux"
	"github.com/invopop/jsonschema"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/web/api/apihelper"

	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("config-ui")

type cfg struct {
	*deps.Deps
}

func Routes(r *mux.Router, deps *deps.Deps) {
	c := &cfg{deps}
	// At menu.html:
	r.Methods("GET").Path("/layers").HandlerFunc(c.getLayers)
	r.Methods("GET").Path("/topo").HandlerFunc(c.topo)

	// At edit.html:
	r.Methods("GET").Path("/schema").HandlerFunc(getSch)
	r.Methods("GET").Path("/layers/{layer}").HandlerFunc(c.getLayer)
	r.Methods("POST").Path("/addlayer").HandlerFunc(c.addLayer)
	r.Methods("POST").Path("/layers/{layer}").HandlerFunc(c.setLayer)
	r.Methods("GET").Path("/default").HandlerFunc(c.def)
}

func (c *cfg) addLayer(w http.ResponseWriter, r *http.Request) {
	var layer struct {
		Name string
	}
	apihelper.OrHTTPFail(w, json.NewDecoder(r.Body).Decode(&layer))
	ct, err := c.DB.Exec(context.Background(), `INSERT INTO harmony_config (title, config) VALUES ($1, $2)`, layer.Name, "")
	apihelper.OrHTTPFail(w, err)
	if ct != 1 {
		w.WriteHeader(http.StatusConflict)
		_, err = w.Write([]byte("Layer already exists"))
		if err != nil {
			log.Errorf("Failed to write response: %s", err)
		}
		return
	}
	w.WriteHeader(200)
}

func getSch(w http.ResponseWriter, r *http.Request) {
	ref := jsonschema.Reflector{
		Mapper: func(i reflect.Type) *jsonschema.Schema {
			if i == reflect.TypeOf(types.MustParseFIL("1 Fil")) { // Override the Pattern for types.FIL
				return &jsonschema.Schema{
					Type:    "string",
					Pattern: "1 fil/0.03 fil/0.31/1 attofil",
				}
			}
			if i == reflect.TypeOf(time.Second) { // Override the Pattern for duration
				return &jsonschema.Schema{
					Type:    "string",
					Pattern: "0h0m0s",
				}
			}
			return nil
		},
	}
	sch := ref.Reflect(config.UnwrapDynamics(config.CurioConfig{}))
	// add comments
	for k, doc := range config.Doc {
		item, ok := sch.Definitions[k]
		if !ok {
			continue
		}
		for _, line := range doc {
			item, ok := item.Properties.Get(line.Name)
			if !ok {
				continue
			}
			if line.Comment != "" {
				extra := make(map[string]any)
				type options struct {
					InfoText string `json:"infoText"`
				}
				opt := options{
					InfoText: line.Comment,
				}
				extra["options"] = opt
				item.Extras = extra
			}
		}
	}

	var allOpt func(s *jsonschema.Schema)
	allOpt = func(s *jsonschema.Schema) {
		if s == nil {
			return
		}
		s.Required = []string{}

		// Recurse into Properties (handles inline schemas like Ingest)
		if s.Properties != nil {
			// Iterate using Oldest/Next pattern (OrderedMap doesn't have Keys method)
			for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
				allOpt(pair.Value) // Recursively process property schemas
			}
		}

		// Recurse into Definitions
		for _, v := range s.Definitions {
			allOpt(v)
		}

		// Recurse into other nested schema structures
		for _, v := range []*jsonschema.Schema{s.Items, s.AdditionalProperties, s.Not, s.If, s.Then, s.Else} {
			allOpt(v)
		}
		for _, v := range []interface{}{s.AllOf, s.AnyOf, s.OneOf} {
			for _, sub := range v.([]*jsonschema.Schema) {
				allOpt(sub)
			}
		}
	}
	allOpt(sch)

	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(sch))
}

func (c *cfg) getLayers(w http.ResponseWriter, r *http.Request) {
	var layers []string
	apihelper.OrHTTPFail(w, c.DB.Select(context.Background(), &layers, `SELECT title FROM harmony_config ORDER BY title`))
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(layers))
}

func (c *cfg) getLayer(w http.ResponseWriter, r *http.Request) {
	var layer string
	apihelper.OrHTTPFail(w, c.DB.QueryRow(context.Background(), `SELECT config FROM harmony_config WHERE title = $1`, mux.Vars(r)["layer"]).Scan(&layer))

	// Read the TOML into a struct
	configStruct := map[string]any{} // NOT CurioConfig b/c we want to preserve unsets
	_, err := toml.Decode(layer, &configStruct)
	apihelper.OrHTTPFail(w, err)

	// Encode the struct as JSON
	jsonData, err := json.Marshal(configStruct)
	apihelper.OrHTTPFail(w, err)

	// Write the JSON response
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonData)
	apihelper.OrHTTPFail(w, err)
}

func (c *cfg) setLayer(w http.ResponseWriter, r *http.Request) {
	layer := mux.Vars(r)["layer"]
	var configStruct map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // JSON lib by default treats number is float64()
	apihelper.OrHTTPFail(w, dec.Decode(&configStruct))

	//Encode the struct as TOML
	var tomlData bytes.Buffer
	err := toml.NewEncoder(&tomlData).Encode(configStruct)
	apihelper.OrHTTPFail(w, err)

	configStr := tomlData.String()

	curioCfg := config.DefaultCurioConfig()
	_, err = deps.LoadConfigWithUpgrades(tomlData.String(), curioCfg)
	apihelper.OrHTTPFail(w, err)

	cb, err := config.ConfigUpdate(curioCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	apihelper.OrHTTPFail(w, err)

	// Generate a full commented string if this is base layer
	if layer == "base" {
		configStr = string(cb)
	}

	//Write the TOML to the database
	_, err = c.DB.Exec(context.Background(), `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, layer, configStr)
	apihelper.OrHTTPFail(w, err)
}

func (c *cfg) topo(w http.ResponseWriter, r *http.Request) {
	var topology []struct {
		Server    string `db:"server"`
		CPU       int    `db:"cpu"`
		GPU       int    `db:"gpu"`
		RAM       int    `db:"ram"`
		LayersCSV string `db:"layers"`
		TasksCSV  string `db:"tasks"`
	}
	apihelper.OrHTTPFail(w, c.DB.Select(context.Background(), &topology, `
	SELECT 
		m.host_and_port as server,
		cpu, gpu, ram, layers, tasks
	FROM harmony_machines m JOIN harmony_machine_details d ON m.id=d.machine_id
	ORDER BY server`))
	w.Header().Set("Content-Type", "application/json")
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(topology))
}

func (c *cfg) def(w http.ResponseWriter, r *http.Request) {
	cb, err := config.ConfigUpdate(config.DefaultCurioConfig(), nil, config.Commented(false), config.DefaultKeepUncommented(), config.NoEnv())
	apihelper.OrHTTPFail(w, err)

	// Read the TOML into a struct
	configStruct := map[string]any{} // NOT CurioConfig b/c we want to preserve unsets
	_, err = toml.Decode(string(cb), &configStruct)
	apihelper.OrHTTPFail(w, err)

	// Encode the struct as JSON
	jsonData, err := json.Marshal(configStruct)
	apihelper.OrHTTPFail(w, err)

	// Write the JSON response
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonData)
	apihelper.OrHTTPFail(w, err)
}
