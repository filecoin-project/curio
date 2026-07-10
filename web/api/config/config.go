package config

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/invopop/jsonschema"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/web/api/apihelper"

	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("config-ui")

// durationPattern validates Go time.ParseDuration strings (e.g. "1h30m", "1m1s", "30s").
// Each clause is optional, but at least one number+unit pair is required.
const durationPattern = `^(\d+(\.\d+)?(h|m|s|ms|us|µs|ns))+$`

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
	r.Methods("GET").Path("/history/{layer}").HandlerFunc(c.getLayerHistory)
	r.Methods("GET").Path("/history/{layer}/{id}").HandlerFunc(c.getHistoryEntry)
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
			if i == reflect.TypeFor[time.Duration]() { // Override the Pattern for duration
				return &jsonschema.Schema{
					Type:        "string",
					Pattern:     durationPattern,
					Description: `Go duration string (e.g. "1h30m", "1m1s", "30s"); units may be omitted when zero`,
				}
			}
			return nil
		},
	}
	sch := ref.Reflect(uiSchemaRoot())

	// Helper to add comments to a schema's properties
	addComments := func(targetSchema *jsonschema.Schema, docEntries []config.DocField) {
		if targetSchema == nil || targetSchema.Properties == nil {
			return
		}
		for _, line := range docEntries {
			item, ok := targetSchema.Properties.Get(line.Name)
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

	// Add comments to definitions
	for k, doc := range config.Doc {
		if item, ok := sch.Definitions[k]; ok {
			addComments(item, doc)
		}
	}

	// Add comments to inline schemas in root Properties (like Ingest -> CurioIngestConfig)
	inlineSchemaMap := uiInlineSchemaDocMap()
	for propName, docKey := range inlineSchemaMap {
		if prop, ok := sch.Properties.Get(propName); ok {
			if doc, ok := config.Doc[docKey]; ok {
				addComments(prop, doc)
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
		for _, v := range []any{s.AllOf, s.AnyOf, s.OneOf} {
			for _, sub := range v.([]*jsonschema.Schema) {
				allOpt(sub)
			}
		}
	}
	allOpt(sch)

	w.Header().Set("Content-Type", "application/json")
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(sch))
}

func (c *cfg) getLayers(w http.ResponseWriter, r *http.Request) {
	var layers []string
	apihelper.OrHTTPFail(w, c.DB.Select(context.Background(), &layers, `SELECT title FROM harmony_config ORDER BY title`))
	w.Header().Set("Content-Type", "application/json")
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(layers))
}

func (c *cfg) getLayer(w http.ResponseWriter, r *http.Request) {
	var layerToml string
	apihelper.OrHTTPFail(w, c.DB.QueryRow(context.Background(), `SELECT config FROM harmony_config WHERE title = $1`, mux.Vars(r)["layer"]).Scan(&layerToml))

	configStruct, err := uiLayerJSON(layerToml)
	apihelper.OrHTTPFail(w, err)

	jsonData, err := json.Marshal(configStruct)
	apihelper.OrHTTPFail(w, err)

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

	var existingToml string
	_ = c.DB.QueryRow(context.Background(), `SELECT config FROM harmony_config WHERE title = $1`, layer).Scan(&existingToml)

	configStr, err := uiPrepareLayerSave(layer, configStruct, existingToml)
	apihelper.OrHTTPFail(w, err)

	// Save config history: snapshot the old config before overwriting
	var oldConfig string
	err = c.DB.QueryRow(context.Background(), `SELECT config FROM harmony_config WHERE title = $1`, layer).Scan(&oldConfig)
	if err == nil && oldConfig != configStr {
		_, err = c.DB.Exec(context.Background(),
			`INSERT INTO harmony_config_history (title, config) VALUES ($1, $2)`,
			layer, oldConfig)
		apihelper.OrHTTPFail(w, err)
	}

	//Write the TOML to the database
	_, err = c.DB.Exec(context.Background(), `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, layer, configStr)
	apihelper.OrHTTPFail(w, err)
}

func (c *cfg) getLayerHistory(w http.ResponseWriter, r *http.Request) {
	layer := mux.Vars(r)["layer"]
	var history []struct {
		ID        int       `db:"id" json:"id"`
		Title     string    `db:"title" json:"title"`
		ChangedAt time.Time `db:"changed_at" json:"changed_at"`
	}
	apihelper.OrHTTPFail(w, c.DB.Select(context.Background(), &history,
		`SELECT id, title, changed_at FROM harmony_config_history WHERE title = $1 ORDER BY changed_at DESC LIMIT 20`, layer))
	w.Header().Set("Content-Type", "application/json")
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(history))
}

func (c *cfg) getHistoryEntry(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.Atoi(idStr)
	apihelper.OrHTTPFail(w, err)

	// Fetch the history entry (snapshot of config before this change)
	var entry struct {
		ID        int       `db:"id" json:"id"`
		Title     string    `db:"title" json:"title"`
		Config    string    `db:"config" json:"config"`
		ChangedAt time.Time `db:"changed_at" json:"changed_at"`
	}
	apihelper.OrHTTPFail(w, c.DB.QueryRow(context.Background(),
		`SELECT id, title, config, changed_at FROM harmony_config_history WHERE id = $1`, id).Scan(&entry.ID, &entry.Title, &entry.Config, &entry.ChangedAt))

	// Always diff against the current live config
	var currentConfig string
	apihelper.OrHTTPFail(w, c.DB.QueryRow(context.Background(),
		`SELECT config FROM harmony_config WHERE title = $1`, entry.Title).Scan(&currentConfig))

	resp := struct {
		ID        int       `json:"id"`
		Title     string    `json:"title"`
		OldConfig string    `json:"old_config"`
		NewConfig string    `json:"new_config"`
		ChangedAt time.Time `json:"changed_at"`
	}{
		ID:        entry.ID,
		Title:     entry.Title,
		OldConfig: entry.Config,
		NewConfig: currentConfig,
		ChangedAt: entry.ChangedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(resp))
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
	configStruct, err := uiDefaultJSON()
	apihelper.OrHTTPFail(w, err)

	jsonData, err := json.Marshal(configStruct)
	apihelper.OrHTTPFail(w, err)

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonData)
	apihelper.OrHTTPFail(w, err)
}
